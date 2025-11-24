package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/futuereta/harbor-proxy/pkg/metrics"
)

// ServeHTTP handles incoming HTTP requests
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Defensive: check for nil request (should never happen in practice)
	if r == nil || r.URL == nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Track active connections
	metrics.ActiveConnections.Inc()
	defer metrics.ActiveConnections.Dec()

	// Start timing
	startTime := time.Now()

	// Wrap response writer to capture metrics
	mw := metrics.NewResponseWriter(w)

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

	// Serve the request
	p.reverseProxy.ServeHTTP(mw, r)

	// Record metrics
	duration := time.Since(startTime)
	metrics.RecordRequest(
		r.Method,
		r.URL.Path,
		r.Host, // Add host for tenant tracking
		mw.StatusCode(),
		duration,
		r.ContentLength,   // bytes received from client
		mw.BytesWritten(), // bytes sent to client
	)
}

// generateRequestID creates a short random ID for request correlation
// Returns a random hex string, or a fallback timestamp-based ID on error
func generateRequestID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		// This should almost never happen with crypto/rand
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano()))[:4])
	}
	return hex.EncodeToString(b)
}

// HealthHandler handles health check requests
// Returns 200 OK when ready to serve traffic
func (p *Proxy) HealthHandler(w http.ResponseWriter, r *http.Request) {
	// Return health check without logging (to avoid noise)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"service":   "harbor-proxy",
		"timestamp": time.Now().Unix(),
	}); err != nil {
		// Log encoding error but don't change response status
		log.Error().Err(err).Msg("failed to encode health response")
	}
}

// ReadinessHandler handles readiness check requests
// Returns 200 OK when ready to serve traffic, 503 when shutting down
func (p *Proxy) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	if atomic.LoadInt32(&p.shuttingDown) == 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "shutting_down",
			"ready":  false,
		}); err != nil {
			log.Error().Err(err).Msg("failed to encode readiness response")
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ready",
		"ready":  true,
	}); err != nil {
		log.Error().Err(err).Msg("failed to encode readiness response")
	}
}
