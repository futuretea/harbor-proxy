package proxy

import (
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

	log.Info().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("host", r.Host).
		Msg("request")

	// Debug log request details
	log.Debug().
		Str("method", r.Method).
		Str("url", r.URL.String()).
		Str("host", r.Host).
		Str("remote_addr", r.RemoteAddr).
		Str("user_agent", r.Header.Get("User-Agent")).
		Str("content_type", r.Header.Get(headerContentType)).
		Int64("content_length", r.ContentLength).
		Msg("incoming request")

	p.reverseProxy.ServeHTTP(w, r)
}
