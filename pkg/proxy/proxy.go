package proxy

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/futuretea/harbor-proxy/pkg/config"
	"github.com/futuretea/harbor-proxy/pkg/metrics"
)

const (
	// Header names
	headerXForwardedProto = "X-Forwarded-Proto"
	headerXForwardedHost  = "X-Forwarded-Host"
	headerHost            = "Host"
	headerAuthorization   = "Authorization"
	headerWwwAuth         = "Www-Authenticate"
	headerLocation        = "Location"
	headerContentType     = "Content-Type"

	// Protocol schemes
	schemeHTTP  = "http"
	schemeHTTPS = "https"

	// Authentication
	authPrefixBearer = "Bearer "

	// Token service
	tokenServiceRealm = `Bearer realm="%s://%s/service/token",service="harbor-registry"`

	// Security: max length to show in logs
	authorizationLogLength = 20
)

// Proxy wraps the reverse proxy with Harbor-specific configuration
type Proxy struct {
	reverseProxy  *httputil.ReverseProxy
	target        *url.URL
	hostPrefixMap map[string]string
	shuttingDown  int32 // atomic flag for graceful shutdown
}

// New creates a new Harbor proxy instance
func New(cfg *config.Config) (*Proxy, error) {
	target, err := url.Parse(cfg.HarborTarget)
	if err != nil {
		return nil, fmt.Errorf("failed to parse harbor target: %w", err)
	}

	proxy := &Proxy{
		target:        target,
		hostPrefixMap: cfg.GetHostPrefixMap(),
	}

	// Create reverse proxy
	reverseProxy := httputil.NewSingleHostReverseProxy(target)

	// Configure TLS and transport optimized for container registry proxy
	// Registry workload characteristics:
	// - Large file transfers (image layers can be hundreds of MB to several GB)
	// - High concurrency (multiple clients pulling/pushing simultaneously)
	// - Long-lived connections (downloading large images takes time)
	// - No total request timeout - blob transfers can take minutes/hours for multi-GB layers
	reverseProxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			// Timeout for establishing TCP connection only
			Timeout:   60 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.TLSInsecure,
		},
		// HTTP/2 enables multiplexing multiple layer downloads over single connection
		ForceAttemptHTTP2: true,
		// Connection pool sizing for high concurrency
		MaxIdleConns:        200, // Total idle connections across all hosts
		MaxIdleConnsPerHost: 100, // Per-backend idle connections (registry clients pull layers in parallel)
		IdleConnTimeout:     90 * time.Second,
		// TLS handshake can be slow under load
		TLSHandshakeTimeout: 15 * time.Second,
		// Wait time for response headers - should be quick even for large blobs
		ResponseHeaderTimeout: 30 * time.Second,
		// Expect-Continue for large uploads (image push)
		ExpectContinueTimeout: 2 * time.Second,
		// Disable compression - container images are already compressed
		DisableCompression: true,
		// Buffer sizes for large file transfers
		WriteBufferSize: 64 * 1024, // 64KB write buffer
		ReadBufferSize:  64 * 1024, // 64KB read buffer
	}

	// Set up request director
	originalDirector := reverseProxy.Director
	reverseProxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Save original client information before rewriting
		incomingProto := req.Header.Get(headerXForwardedProto)
		if incomingProto == "" {
			incomingProto = schemeHTTP
		}
		incomingHost := req.Host // Keep the full host with port

		// Store client information in headers for ModifyResponse
		// These headers will be used to rewrite responses back to client
		if req.Header.Get(headerXForwardedProto) == "" {
			req.Header.Set(headerXForwardedProto, incomingProto)
		}
		if req.Header.Get(headerXForwardedHost) == "" {
			req.Header.Set(headerXForwardedHost, incomingHost)
		}

		// Determine prefix based on incoming host (exact match with port)
		// Use ToLower once to avoid multiple calls
		hostKey := strings.ToLower(incomingHost)
		prefix := proxy.hostPrefixMap[hostKey]
		reqID := req.Header.Get("X-Request-ID")

		// Rewrite repository path for registry endpoints
		originalPath := req.URL.Path
		req.URL.Path = rewriteRepositoryPath(prefix, req.URL.Path)

		if originalPath != req.URL.Path {
			metrics.PathRewritesTotal.Inc()
			log.Debug().
				Str("req_id", reqID).
				Str("original_path", originalPath).
				Str("rewritten_path", req.URL.Path).
				Str("prefix", prefix).
				Msg("  ⤷ path rewritten")
		}

		// Debug log outgoing request to backend
		if log.Debug().Enabled() {
			authorization := req.Header.Get(headerAuthorization)
			authType := ""
			if authorization != "" {
				if strings.HasPrefix(authorization, "Basic ") {
					authType = "Basic"
				} else if strings.HasPrefix(authorization, "Bearer ") {
					authType = "Bearer"
				} else {
					authType = "Unknown"
				}
			}

			logEvent := log.Debug().
				Str("req_id", reqID).
				Str("backend", target.Host).
				Str("path", req.URL.Path)

			if authType != "" {
				logEvent.Str("auth", authType)
			}
			if req.URL.RawQuery != "" {
				logEvent.Str("query", req.URL.RawQuery)
			}
			if prefix != "" {
				logEvent.Str("prefix", prefix)
			}

			logEvent.Msg("  ⤷ backend")
		}

		// CRITICAL: Set Host header to backend target host
		// This ensures Harbor/Ingress always sees the same host regardless of client
		req.Host = target.Host
		// Also set Host in Header for HTTP/1.1 compliance
		req.Header.Set(headerHost, target.Host)

		// For token service, adjust scope query
		if maybeRewriteTokenScope(prefix, req) {
			metrics.ScopeRewritesTotal.Inc()
			if log.Debug().Enabled() {
				log.Debug().
					Str("req_id", reqID).
					Str("prefix", prefix).
					Msg("  ⤷ scope rewritten")
			}
		}
	}

	// Set up response modifier
	reverseProxy.ModifyResponse = func(resp *http.Response) error {
		proto := resp.Request.Header.Get(headerXForwardedProto)
		if proto == "" {
			proto = schemeHTTP
		}
		clientHost := resp.Request.Header.Get(headerXForwardedHost)
		if clientHost == "" {
			clientHost = resp.Request.Host
		}
		reqID := resp.Request.Header.Get("X-Request-ID")

		// Log response status (info for errors, debug for success)
		if resp.StatusCode >= 400 {
			log.Info().
				Str("req_id", reqID).
				Int("status", resp.StatusCode).
				Str("path", resp.Request.URL.Path).
				Msg("← error response")
		} else if log.Debug().Enabled() {
			log.Debug().
				Str("req_id", reqID).
				Int("status", resp.StatusCode).
				Str("content_type", resp.Header.Get(headerContentType)).
				Msg("← response")
		}

		// Www-Authenticate realm should point to our proxy host
		// Only rewrite Bearer token authentication, preserve Basic auth as-is
		if wa := resp.Header.Get(headerWwwAuth); wa != "" && strings.HasPrefix(wa, authPrefixBearer) {
			resp.Header.Del(headerWwwAuth)
			resp.Header.Set(headerWwwAuth, fmt.Sprintf(tokenServiceRealm, proto, clientHost))
			if log.Debug().Enabled() {
				log.Debug().
					Str("req_id", reqID).
					Str("realm", fmt.Sprintf("%s://%s/service/token", proto, clientHost)).
					Msg("  ⤷ www-authenticate rewritten")
			}
		}

		// Rewrite Location header (uploads/manifests redirect)
		if location := resp.Header.Get(headerLocation); location != "" {
			// Parse the location URL
			if locationURL, err := url.Parse(location); err == nil {
				// Check if it's an absolute URL pointing to the backend
				if locationURL.IsAbs() {
					// Replace backend host with client host
					// Use target's scheme (not client's proto) to match backend protocol
					// UNLESS client explicitly connected via HTTP and we're not in TLS mode
					scheme := target.Scheme
					if proto == schemeHTTP && scheme == schemeHTTPS {
						// Client used HTTP, don't upgrade to HTTPS in redirect
						// This prevents "http: server gave HTTP response to HTTPS client" error
						scheme = schemeHTTP
					}
					locationURL.Scheme = scheme
					locationURL.Host = clientHost
					newLocation := locationURL.String()
					resp.Header.Set(headerLocation, newLocation)
					if log.Debug().Enabled() {
						log.Debug().
							Str("req_id", reqID).
							Str("location", newLocation).
							Msg("  ⤷ location rewritten")
					}
				}
				// If it's a relative URL, leave it as is
			} else {
				log.Warn().
					Str("req_id", reqID).
					Err(err).
					Str("location", location).
					Msg("failed to parse location header")
			}
		}

		return nil
	}

	proxy.reverseProxy = reverseProxy
	return proxy, nil
}

// SetShuttingDown marks the proxy as shutting down
// This will cause readiness checks to fail
func (p *Proxy) SetShuttingDown() {
	atomic.StoreInt32(&p.shuttingDown, 1)
}
