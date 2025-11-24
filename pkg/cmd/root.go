package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/futuereta/harbor-proxy/pkg/config"
	"github.com/futuereta/harbor-proxy/pkg/proxy"
)

// bindFlags binds command-line flags to viper configuration keys
func bindFlags(cmd *cobra.Command) {
	flagBindings := map[string]string{
		"harbor_target":   "target",
		"proxy_listen":    "listen",
		"host_prefix_map": "map",
		"tls_insecure":    "tls-insecure",
		"tls_enabled":     "tls-enabled",
		"tls_cert_file":   "tls-cert-file",
		"tls_key_file":    "tls-key-file",
		"pprof_listen":    "pprof-listen",
		"log_level":       "log-level",
	}

	for key, flag := range flagBindings {
		viper.BindPFlag(key, cmd.Flags().Lookup(flag))
	}
}

// NewRootCmd creates a new cobra command for the Harbor Proxy
func NewRootCmd() *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:   "harbor-proxy",
		Short: "Harbor registry proxy with host-based repository prefixing",
		Long: `Harbor Proxy is a reverse proxy for Harbor that enables multi-domain support
with automatic repository path rewriting based on the incoming host.

This allows you to route different domains to different repository prefixes
in your Harbor registry, enabling multi-tenancy and domain-based isolation.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			bindFlags(cmd)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProxy(cfgFile)
		},
	}

	// Add configuration file flag
	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path (supports YAML)")

	// Proxy configuration flags
	cmd.Flags().String("target", "", "Harbor target URL (e.g., https://harbor.example.com)")
	cmd.Flags().String("listen", "", "Listen address (default :8080)")
	cmd.Flags().String("map", "", "Host to repo prefix map (e.g., 'hosta.example.com=a-,hostb.example.com=b-')")
	cmd.Flags().Bool("tls-insecure", true, "Skip TLS certificate verification for backend")
	cmd.Flags().Bool("tls-enabled", false, "Enable TLS/HTTPS for proxy server")
	cmd.Flags().String("tls-cert-file", "", "TLS certificate file path")
	cmd.Flags().String("tls-key-file", "", "TLS private key file path")
	cmd.Flags().String("log-level", "info", "Log level (trace, debug, info, warn, error, fatal, panic)")
	cmd.Flags().String("pprof-listen", "", "pprof HTTP listen address (e.g., :6060, disabled if empty)")

	return cmd
}

// runProxy runs the Harbor proxy with the given configuration
func runProxy(cfgFile string) error {
	// Load configuration from file, environment variables, and command-line flags
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logging with JSON format
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	// Parse log level from string
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel // fallback to info
	}
	zerolog.SetGlobalLevel(level)

	// Create proxy
	p, err := proxy.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create proxy: %w", err)
	}

	// Log startup information
	prefixMap := cfg.GetHostPrefixMap()

	// Create a formatted string showing the host mappings
	var mappings []string
	for host, prefix := range prefixMap {
		mappings = append(mappings, fmt.Sprintf("%s → %s", host, prefix))
	}

	log.Info().
		Str("listen", cfg.ProxyListen).
		Str("target", cfg.HarborTarget).
		Bool("tls", cfg.TLSEnabled).
		Str("log_level", cfg.LogLevel).
		Msg("Starting harbor-proxy")

	if len(mappings) > 0 {
		log.Info().
			Int("count", len(mappings)).
			Strs("mappings", mappings).
			Msg("Host mappings configured")
	}

	// Start pprof server on separate port if enabled
	var pprofServer *http.Server
	if cfg.PprofListen != "" {
		pprofMux := http.NewServeMux()
		pprofMux.HandleFunc("/debug/pprof/", pprof.Index)
		pprofMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		pprofMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		pprofMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		pprofMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		pprofServer = &http.Server{
			Addr:    cfg.PprofListen,
			Handler: pprofMux,
		}

		go func() {
			log.Info().
				Str("addr", cfg.PprofListen).
				Msg("pprof server enabled")
			if err := pprofServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("pprof server error")
			}
		}()
	}

	// Start HTTP/HTTPS server with a new ServeMux (without pprof handlers)
	proxyMux := http.NewServeMux()
	// Prometheus metrics endpoint (no auth required)
	proxyMux.Handle("/metrics", promhttp.Handler())
	// Health check endpoints (no auth required)
	proxyMux.HandleFunc("/healthz", p.HealthHandler)
	proxyMux.HandleFunc("/readyz", p.ReadinessHandler)
	// Main proxy handler
	proxyMux.HandleFunc("/", p.ServeHTTP)

	// Create server with explicit configuration for graceful shutdown
	mainServer := &http.Server{
		Addr:    cfg.ProxyListen,
		Handler: proxyMux,
		// No read/write timeout - registry blob transfers can take hours
		// Graceful shutdown will handle in-flight requests properly
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if cfg.TLSEnabled {
			log.Info().
				Str("cert", cfg.TLSCertFile).
				Str("key", cfg.TLSKeyFile).
				Msg("Serving HTTPS")
			serverErr <- mainServer.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			log.Info().Msg("Serving HTTP")
			serverErr <- mainServer.ListenAndServe()
		}
	}()

	// Wait for interrupt signal or server error
	select {
	case sig := <-sigChan:
		log.Info().
			Str("signal", sig.String()).
			Msg("Received shutdown signal, starting graceful shutdown...")

		// Mark as shutting down - readiness checks will fail
		p.SetShuttingDown()
		log.Info().Msg("Marked as not ready, waiting for load balancer to remove backend...")

		// Give load balancers time to detect we're not ready (via readiness probe)
		// This prevents new connections from being established
		time.Sleep(10 * time.Second)

		// Create shutdown context with timeout
		// For registry proxy, we need generous timeout for large blob transfers
		shutdownTimeout := 5 * time.Minute
		log.Info().
			Dur("timeout", shutdownTimeout).
			Msg("Waiting for active connections to complete")

		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		// Shutdown main server
		if err := mainServer.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("Error during main server shutdown")
		} else {
			log.Info().Msg("Main server stopped gracefully")
		}

		// Shutdown pprof server if running
		if pprofServer != nil {
			if err := pprofServer.Shutdown(ctx); err != nil {
				log.Error().Err(err).Msg("Error during pprof server shutdown")
			}
		}

		return nil

	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}

// Execute runs the root command
func Execute() error {
	cmd := NewRootCmd()
	return cmd.Execute()
}
