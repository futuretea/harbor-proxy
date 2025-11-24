package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "harbor_proxy"
)

var (
	// RequestsTotal counts total HTTP requests
	// Labels: method, path_type, status, host
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of HTTP requests by client host",
		},
		[]string{"method", "path_type", "status", "host"},
	)

	// RequestDuration tracks request latency
	// Labels: method, path_type, host
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency in seconds by client host",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		},
		[]string{"method", "path_type", "host"},
	)

	// BytesTransferred tracks data transfer
	// Labels: direction (sent/received), host
	BytesTransferred = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "bytes_transferred_total",
			Help:      "Total bytes transferred by client host",
		},
		[]string{"direction", "host"}, // "sent" or "received", "host"
	)

	// BackendRequestsTotal counts backend requests
	BackendRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "backend_requests_total",
			Help:      "Total number of backend requests",
		},
		[]string{"backend", "status"},
	)

	// BackendRequestDuration tracks backend latency
	BackendRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "backend_request_duration_seconds",
			Help:      "Backend request latency in seconds",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		},
		[]string{"backend"},
	)

	// PathRewritesTotal counts path rewrites
	PathRewritesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "path_rewrites_total",
			Help:      "Total number of path rewrites",
		},
	)

	// ScopeRewritesTotal counts scope rewrites
	ScopeRewritesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scope_rewrites_total",
			Help:      "Total number of token scope rewrites",
		},
	)

	// ActiveConnections tracks current active connections
	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_connections",
			Help:      "Current number of active connections",
		},
	)

	// RegistryOperations tracks registry-specific operations
	RegistryOperations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "registry_operations_total",
			Help:      "Total number of registry operations",
		},
		[]string{"operation"}, // "pull", "push", "manifest", "blob", "token"
	)
)

// PathType categorizes request paths for metrics
// Safe against out-of-bounds panics
func PathType(path string) string {
	if path == "/v2/" {
		return "ping"
	}
	if path == "/healthz" {
		return "health"
	}
	if path == "/readyz" {
		return "readiness"
	}

	pathLen := len(path)

	// Registry API paths - check length first to prevent panic
	switch {
	case pathLen >= 16 && path[:16] == "/v2/_catalog":
		return "catalog"
	case pathLen >= 14 && path[pathLen-14:] == "/service/token":
		return "token"
	case pathLen > 10 && (path[pathLen-10:] == "/manifests" || contains(path, "/manifests/")):
		return "manifest"
	case pathLen > 6 && (path[pathLen-6:] == "/blobs" || contains(path, "/blobs/")):
		return "blob"
	case pathLen > 5 && contains(path, "/tags"):
		return "tags"
	case pathLen > 8 && contains(path, "/uploads"):
		return "upload"
	default:
		return "other"
	}
}

// RegistryOperation determines the operation type
func RegistryOperation(method, pathType string) string {
	switch pathType {
	case "token":
		return "token"
	case "manifest":
		if method == "GET" || method == "HEAD" {
			return "pull"
		}
		return "push"
	case "blob":
		if method == "GET" || method == "HEAD" {
			return "pull"
		}
		return "push"
	case "upload":
		return "push"
	default:
		return "other"
	}
}

// RecordRequest records a complete request
// Optimized to skip health checks to reduce overhead
func RecordRequest(method, path, host string, status int, duration time.Duration, bytesReceived, bytesSent int64) {
	pathType := PathType(path)

	// Skip metrics for health checks to reduce overhead
	if pathType == "health" || pathType == "readiness" {
		return
	}

	statusStr := strconv.Itoa(status)

	RequestsTotal.WithLabelValues(method, pathType, statusStr, host).Inc()
	RequestDuration.WithLabelValues(method, pathType, host).Observe(duration.Seconds())

	if bytesReceived > 0 {
		BytesTransferred.WithLabelValues("received", host).Add(float64(bytesReceived))
	}
	if bytesSent > 0 {
		BytesTransferred.WithLabelValues("sent", host).Add(float64(bytesSent))
	}

	// Track registry operations (skip ping as well)
	if pathType != "ping" {
		operation := RegistryOperation(method, pathType)
		RegistryOperations.WithLabelValues(operation).Inc()
	}
}

// contains checks if string contains substring (simple helper)
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
