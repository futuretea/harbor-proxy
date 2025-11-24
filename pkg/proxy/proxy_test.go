package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/futuereta/harbor-proxy/pkg/config"
)

// TestProxyNew tests the proxy constructor
func TestProxyNew(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		expectError bool
	}{
		{
			name: "valid config",
			cfg: &config.Config{
				HarborTarget: "http://harbor.example.com",
				HostPrefixMap: map[string]string{
					"hosta.local": "team-a-",
					"hostb.local": "team-b-",
				},
				TLSInsecure: true,
			},
			expectError: false,
		},
		{
			name: "invalid URL",
			cfg: &config.Config{
				HarborTarget: "://invalid-url",
			},
			expectError: true,
		},
		{
			name: "empty host prefix map",
			cfg: &config.Config{
				HarborTarget:  "https://harbor.example.com",
				HostPrefixMap: map[string]string{},
				TLSInsecure:   false,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy, err := New(tt.cfg)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if proxy == nil {
				t.Error("expected proxy but got nil")
				return
			}

			// Verify proxy was configured correctly
			if proxy.target.String() != tt.cfg.HarborTarget {
				t.Errorf("target = %q, want %q", proxy.target.String(), tt.cfg.HarborTarget)
			}
		})
	}
}

// TestProxyServeHTTP tests the HTTP request handling
func TestProxyServeHTTP(t *testing.T) {
	// Create a mock backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back the request path and host for verification
		w.Header().Set("X-Backend-Path", r.URL.Path)
		w.Header().Set("X-Backend-Host", r.Host)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	tests := []struct {
		name                string
		hostPrefixMap       map[string]string
		requestHost         string
		requestPath         string
		expectedBackendPath string
	}{
		{
			name: "path rewrite for hosta",
			hostPrefixMap: map[string]string{
				"hosta.local": "team-a-",
			},
			requestHost:         "hosta.local",
			requestPath:         "/v2/myrepo/manifests/latest",
			expectedBackendPath: "/v2/team-a-myrepo/manifests/latest",
		},
		{
			name: "path rewrite for hostb",
			hostPrefixMap: map[string]string{
				"hostb.local": "team-b-",
			},
			requestHost:         "hostb.local",
			requestPath:         "/v2/project/image/blobs/sha256:abc",
			expectedBackendPath: "/v2/team-b-project/image/blobs/sha256:abc",
		},
		{
			name:                "no prefix for unknown host",
			hostPrefixMap:       map[string]string{},
			requestHost:         "unknown.local",
			requestPath:         "/v2/myrepo/manifests/latest",
			expectedBackendPath: "/v2/myrepo/manifests/latest",
		},
		{
			name: "API ping should not be rewritten",
			hostPrefixMap: map[string]string{
				"hosta.local": "team-a-",
			},
			requestHost:         "hosta.local",
			requestPath:         "/v2/",
			expectedBackendPath: "/v2/",
		},
		{
			name: "host with port should work",
			hostPrefixMap: map[string]string{
				"hosta.local:8099": "team-a-",
			},
			requestHost:         "hosta.local:8099",
			requestPath:         "/v2/myrepo/manifests/latest",
			expectedBackendPath: "/v2/team-a-myrepo/manifests/latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create proxy config
			cfg := &config.Config{
				HarborTarget:  backend.URL,
				HostPrefixMap: tt.hostPrefixMap,
				TLSInsecure:   true,
			}

			// Create proxy
			p, err := New(cfg)
			if err != nil {
				t.Fatalf("failed to create proxy: %v", err)
			}

			// Create test request
			req := httptest.NewRequest("GET", tt.requestPath, nil)
			req.Host = tt.requestHost

			// Create response recorder
			rr := httptest.NewRecorder()

			// Execute the proxy
			p.ServeHTTP(rr, req)

			// Verify the backend received the correct path
			backendPath := rr.Header().Get("X-Backend-Path")
			if backendPath != tt.expectedBackendPath {
				t.Errorf("backend received path %q, want %q", backendPath, tt.expectedBackendPath)
			}

			// Verify response
			if rr.Code != http.StatusOK {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
			}
		})
	}
}

// TestProxyHostPrefixMapCaseInsensitive tests that host matching is case-insensitive
func TestProxyHostPrefixMapCaseInsensitive(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend-Path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		HarborTarget: backend.URL,
		HostPrefixMap: map[string]string{
			"hosta.local": "team-a-",
		},
		TLSInsecure: true,
	}

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Test with different case variations
	hosts := []string{"hosta.local", "HOSTA.LOCAL", "HostA.Local", "HoStA.lOcAl"}

	for _, host := range hosts {
		t.Run("host="+host, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v2/myrepo/manifests/latest", nil)
			req.Host = host

			rr := httptest.NewRecorder()
			p.ServeHTTP(rr, req)

			backendPath := rr.Header().Get("X-Backend-Path")
			expected := "/v2/team-a-myrepo/manifests/latest"
			if backendPath != expected {
				t.Errorf("host %q: backend path = %q, want %q", host, backendPath, expected)
			}
		})
	}
}

// TestProxyHeaderRewrite tests that headers are correctly rewritten
func TestProxyHeaderRewrite(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate Harbor's Www-Authenticate header
		w.Header().Set("Www-Authenticate", `Bearer realm="https://backend.example.com/service/token",service="harbor-registry"`)
		// Simulate Location header for uploads
		w.Header().Set("Location", "https://backend.example.com/v2/myrepo/uploads/123")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer backend.Close()

	cfg := &config.Config{
		HarborTarget: backend.URL,
		HostPrefixMap: map[string]string{
			"proxy.local": "team-",
		},
		TLSInsecure: true,
	}

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/v2/", nil)
	req.Host = "proxy.local:8099"

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	// Check Www-Authenticate header was rewritten to point to proxy
	authHeader := rr.Header().Get("Www-Authenticate")
	if !containsString(authHeader, "proxy.local") {
		t.Errorf("Www-Authenticate should contain proxy.local, got: %s", authHeader)
	}
	if containsString(authHeader, "backend.example.com") {
		t.Errorf("Www-Authenticate should not contain backend.example.com, got: %s", authHeader)
	}

	// Check Location header was rewritten
	location := rr.Header().Get("Location")
	if !containsString(location, "proxy.local") {
		t.Errorf("Location should contain proxy.local, got: %s", location)
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			len(s) > len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
