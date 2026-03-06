package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/rs/zerolog"

	"github.com/futuretea/harbor-proxy/pkg/config"
)

func init() {
	// Disable logging during benchmarks
	zerolog.SetGlobalLevel(zerolog.Disabled)
}

// BenchmarkRewriteRepositoryPath benchmarks the repository path rewriting
func BenchmarkRewriteRepositoryPath(b *testing.B) {
	tests := []struct {
		name   string
		prefix string
		path   string
	}{
		{
			name:   "simple_manifest",
			prefix: "team-a-",
			path:   "/v2/myrepo/manifests/latest",
		},
		{
			name:   "nested_repository",
			prefix: "prod-",
			path:   "/v2/project/image/blobs/sha256:abc123",
		},
		{
			name:   "no_rewrite_catalog",
			prefix: "team-",
			path:   "/v2/_catalog",
		},
		{
			name:   "no_rewrite_ping",
			prefix: "team-",
			path:   "/v2/",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = rewriteRepositoryPath(tt.prefix, tt.path)
			}
		})
	}
}

// BenchmarkMaybeRewriteTokenScope benchmarks token scope rewriting
func BenchmarkMaybeRewriteTokenScope(b *testing.B) {
	tests := []struct {
		name   string
		prefix string
		scope  string
	}{
		{
			name:   "simple_scope",
			prefix: "team-a-",
			scope:  "repository:myrepo:pull",
		},
		{
			name:   "nested_scope",
			prefix: "prod-",
			scope:  "repository:project/image:push,pull",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				req, _ := http.NewRequest("GET", "/service/token?scope="+url.QueryEscape(tt.scope), nil)
				maybeRewriteTokenScope(tt.prefix, req)
			}
		})
	}
}

// BenchmarkStripPort benchmarks the stripPort utility function
func BenchmarkStripPort(b *testing.B) {
	tests := []struct {
		name     string
		hostport string
	}{
		{
			name:     "with_port",
			hostport: "example.com:8080",
		},
		{
			name:     "without_port",
			hostport: "example.com",
		},
		{
			name:     "ipv6_with_port",
			hostport: "[2001:db8::1]:8080",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = stripPort(tt.hostport)
			}
		})
	}
}

// BenchmarkProxyServeHTTP benchmarks the full proxy request handling
func BenchmarkProxyServeHTTP(b *testing.B) {
	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Create proxy
	cfg := &config.Config{
		HarborTarget:  backend.URL,
		HostPrefixMap: map[string]string{"test.local": "test-"},
	}
	proxy, err := New(cfg)
	if err != nil {
		b.Fatalf("failed to create proxy: %v", err)
	}

	tests := []struct {
		name string
		path string
		host string
	}{
		{
			name: "manifest_request",
			path: "/v2/myrepo/manifests/latest",
			host: "test.local",
		},
		{
			name: "blob_request",
			path: "/v2/myrepo/blobs/sha256:abc123",
			host: "test.local",
		},
		{
			name: "ping_request",
			path: "/v2/",
			host: "test.local",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest("GET", tt.path, nil)
				req.Host = tt.host
				w := httptest.NewRecorder()
				proxy.ServeHTTP(w, req)
			}
		})
	}
}
