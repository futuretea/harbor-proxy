package cmd

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

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
	cmd.Flags().String("listen", "", "Listen address (default :8099)")
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
	log.Info().
		Str("listen", cfg.ProxyListen).
		Str("target", cfg.HarborTarget).
		Int("hosts", len(prefixMap)).
		Bool("tls_insecure", cfg.TLSInsecure).
		Bool("tls_enabled", cfg.TLSEnabled).
		Msg("starting harbor-proxy")

	// Start pprof server on separate port if enabled
	if cfg.PprofListen != "" {
		pprofMux := http.NewServeMux()
		pprofMux.HandleFunc("/debug/pprof/", pprof.Index)
		pprofMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		pprofMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		pprofMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		pprofMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		go func() {
			log.Info().
				Str("pprof_listen", cfg.PprofListen).
				Msg("starting pprof server")
			if err := http.ListenAndServe(cfg.PprofListen, pprofMux); err != nil {
				log.Error().Err(err).Msg("pprof server error")
			}
		}()
	}

	// Start HTTP/HTTPS server with a new ServeMux (without pprof handlers)
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/", p.ServeHTTP)

	log.Info().Msg("harbor-proxy is running")

	if cfg.TLSEnabled {
		log.Info().
			Str("cert", cfg.TLSCertFile).
			Str("key", cfg.TLSKeyFile).
			Msg("starting HTTPS server")
		if err := http.ListenAndServeTLS(cfg.ProxyListen, cfg.TLSCertFile, cfg.TLSKeyFile, proxyMux); err != nil {
			return fmt.Errorf("HTTPS server error: %w", err)
		}
	} else {
		log.Info().Msg("starting HTTP server")
		if err := http.ListenAndServe(cfg.ProxyListen, proxyMux); err != nil {
			return fmt.Errorf("HTTP server error: %w", err)
		}
	}

	return nil
}

// Execute runs the root command
func Execute() error {
	cmd := NewRootCmd()
	return cmd.Execute()
}
