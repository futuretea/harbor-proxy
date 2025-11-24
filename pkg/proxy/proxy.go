package proxy

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/futuereta/harbor-proxy/pkg/config"
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

	// Configure TLS and transport with proper settings for proxy workload
	reverseProxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.TLSInsecure,
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
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

		// Debug: log host mapping
		log.Debug().
			Str("incoming_host", incomingHost).
			Str("prefix", prefix).
			Int("map_size", len(proxy.hostPrefixMap)).
			Msg("host prefix lookup")

		// Rewrite repository path for registry endpoints
		originalPath := req.URL.Path
		req.URL.Path = rewriteRepositoryPath(prefix, req.URL.Path)

		if originalPath != req.URL.Path {
			log.Debug().
				Str("original_path", originalPath).
				Str("rewritten_path", req.URL.Path).
				Str("prefix", prefix).
				Msg("path rewritten")
		}

		// Debug log outgoing request to backend
		authorization := req.Header.Get(headerAuthorization)
		if len(authorization) > authorizationLogLength {
			// Mask token for security, show first 20 chars
			authorization = authorization[:authorizationLogLength] + "..."
		}
		log.Debug().
			Str("backend_url", target.String()).
			Str("backend_path", req.URL.Path).
			Str("backend_host", target.Host).
			Str("client_host", incomingHost).
			Str("prefix", prefix).
			Str("authorization", authorization).
			Str("query", req.URL.RawQuery).
			Msg("proxying to backend")

		// CRITICAL: Set Host header to backend target host
		// This ensures Harbor/Ingress always sees the same host regardless of client
		req.Host = target.Host
		// Also set Host in Header for HTTP/1.1 compliance
		req.Header.Set(headerHost, target.Host)

		// For token service, adjust scope query
		maybeRewriteTokenScope(prefix, req)
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

		// Debug log backend response
		log.Debug().
			Int("status_code", resp.StatusCode).
			Str("status", resp.Status).
			Str("content_type", resp.Header.Get(headerContentType)).
			Int64("content_length", resp.ContentLength).
			Str("www_authenticate", resp.Header.Get(headerWwwAuth)).
			Str("location", resp.Header.Get(headerLocation)).
			Msg("backend response")

		// Www-Authenticate realm should point to our proxy host
		// Only rewrite Bearer token authentication, preserve Basic auth as-is
		if wa := resp.Header.Get(headerWwwAuth); wa != "" && strings.HasPrefix(wa, authPrefixBearer) {
			originalWA := wa
			resp.Header.Del(headerWwwAuth)
			resp.Header.Set(headerWwwAuth, fmt.Sprintf(tokenServiceRealm, proto, clientHost))
			log.Debug().
				Str("original", originalWA).
				Str("rewritten", resp.Header.Get(headerWwwAuth)).
				Msg("www-authenticate header rewritten")
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
					log.Debug().
						Str("original", location).
						Str("rewritten", newLocation).
						Str("target_scheme", target.Scheme).
						Str("client_proto", proto).
						Str("final_scheme", scheme).
						Msg("location header rewritten")
				}
				// If it's a relative URL, leave it as is
			} else {
				log.Warn().
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
