package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/viper"
)

// Config represents the static configuration for the Harbor Proxy
type Config struct {
	// Target Harbor server URL
	HarborTarget string `mapstructure:"harbor_target"`

	// Proxy listen address
	ProxyListen string `mapstructure:"proxy_listen"`

	// Host to repository prefix mapping
	HostPrefixMap map[string]string `mapstructure:"host_prefix_map"`

	// TLS configuration for backend
	TLSInsecure bool `mapstructure:"tls_insecure"`

	// TLS configuration for proxy server
	TLSEnabled  bool   `mapstructure:"tls_enabled"`
	TLSCertFile string `mapstructure:"tls_cert_file"`
	TLSKeyFile  string `mapstructure:"tls_key_file"`

	// pprof listen address
	PprofListen string `mapstructure:"pprof_listen"`

	// Log level (trace, debug, info, warn, error, fatal, panic)
	LogLevel string `mapstructure:"log_level"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate Harbor target
	if c.HarborTarget == "" {
		return fmt.Errorf("harbor_target is required")
	}
	if !strings.HasPrefix(c.HarborTarget, "http://") && !strings.HasPrefix(c.HarborTarget, "https://") {
		return fmt.Errorf("harbor_target must start with http:// or https://, got %s", c.HarborTarget)
	}
	if _, err := url.Parse(c.HarborTarget); err != nil {
		return fmt.Errorf("invalid harbor_target URL: %w", err)
	}

	// Validate log level
	validLevels := map[string]bool{
		"trace": true,
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
		"panic": true,
	}
	if c.LogLevel != "" && !validLevels[strings.ToLower(c.LogLevel)] {
		return fmt.Errorf("log_level must be one of: trace, debug, info, warn, error, fatal, panic, got %s", c.LogLevel)
	}

	// Validate TLS configuration
	if c.TLSEnabled {
		if c.TLSCertFile == "" {
			return fmt.Errorf("tls_cert_file is required when tls_enabled is true")
		}
		if c.TLSKeyFile == "" {
			return fmt.Errorf("tls_key_file is required when tls_enabled is true")
		}
	}

	return nil
}

// LoadConfig loads configuration from file and environment variables using Viper
// Priority: command-line flags > environment variables > config file > defaults
func LoadConfig(configPath string) (*Config, error) {
	// Use the global viper instance to access bound command-line flags
	v := viper.GetViper()

	// Set configuration file if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Configure environment variable support
	// Environment variables use HARBOR_PROXY_ prefix and replace - with _
	v.SetEnvPrefix("HARBOR_PROXY")
	v.AllowEmptyEnv(true)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	// Unmarshal configuration into struct
	config := &Config{}

	// Manually set host_prefix_map to avoid unmarshal type conflict
	// It can be either a string (from CLI/env) or a map (from YAML)
	if mapStr := v.GetString("host_prefix_map"); mapStr != "" {
		// String format: "hosta=a-,hostb=b-" (from --map flag or env var)
		config.HostPrefixMap = parseHostPrefixMapString(mapStr)
	} else if mapValue := v.GetStringMapString("host_prefix_map"); len(mapValue) > 0 {
		// Map format from YAML config file
		config.HostPrefixMap = mapValue
	}

	// Unmarshal remaining fields
	config.HarborTarget = v.GetString("harbor_target")
	config.ProxyListen = v.GetString("proxy_listen")
	config.TLSInsecure = v.GetBool("tls_insecure")
	config.TLSEnabled = v.GetBool("tls_enabled")
	config.TLSCertFile = v.GetString("tls_cert_file")
	config.TLSKeyFile = v.GetString("tls_key_file")
	config.PprofListen = v.GetString("pprof_listen")
	config.LogLevel = v.GetString("log_level")

	// Apply defaults for empty values
	applyDefaults(config)

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// applyDefaults applies default values to empty configuration fields
func applyDefaults(config *Config) {
	if config.ProxyListen == "" {
		config.ProxyListen = ":8080"
	}
	if config.LogLevel == "" {
		config.LogLevel = "info"
	}
	// TLSInsecure defaults to true for development
	if !config.TLSInsecure {
		config.TLSInsecure = true
	}
}

// GetHostPrefixMap returns the host prefix map with normalized keys (lowercase)
// Keys preserve port information for exact matching
func (c *Config) GetHostPrefixMap() map[string]string {
	m := make(map[string]string)
	for host, prefix := range c.HostPrefixMap {
		// Normalize to lowercase for case-insensitive matching
		// but preserve port for exact matching
		m[strings.ToLower(host)] = prefix
	}
	return m
}

// parseHostPrefixMapString parses comma-separated host=prefix pairs
// Format: "host1=prefix1,host2=prefix2"
func parseHostPrefixMapString(s string) map[string]string {
	m := make(map[string]string)
	if s == "" {
		return m
	}
	pairs := strings.Split(s, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			host := strings.TrimSpace(parts[0])
			prefix := strings.TrimSpace(parts[1])
			if host != "" {
				m[host] = prefix
			}
		}
	}
	return m
}
