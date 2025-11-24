package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/rs/zerolog/log"
)

// ServeHTTP handles incoming HTTP requests
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Preserve client context for ModifyResponse
	// Auto-detect TLS connection or use X-Forwarded-Proto header
	if xfProto := r.Header.Get(headerXForwardedProto); xfProto == "" {
		// Detect if request came over TLS
		proto := schemeHTTP
		if r.TLS != nil {
			proto = schemeHTTPS
		}
		r.Header.Set(headerXForwardedProto, proto)
	}
	r.Header.Set(headerXForwardedHost, r.Host)

	// Generate request ID for correlation
	reqID := generateRequestID()
	r.Header.Set("X-Request-ID", reqID)

	// Log incoming request with essential info
	logEvent := log.Info().
		Str("req_id", reqID).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("host", r.Host)

	// Add query string if present
	if r.URL.RawQuery != "" {
		logEvent.Str("query", r.URL.RawQuery)
	}

	// Add remote address in debug mode
	if log.Debug().Enabled() {
		logEvent.Str("remote", r.RemoteAddr)
	}

	logEvent.Msg("→ request")

	p.reverseProxy.ServeHTTP(w, r)
}

// generateRequestID creates a short random ID for request correlation
func generateRequestID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
