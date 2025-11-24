package proxy

import (
	"testing"
)

// TestStripPort tests the stripPort function
func TestStripPort(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "host with port",
			host:     "example.com:8080",
			expected: "example.com",
		},
		{
			name:     "host without port",
			host:     "example.com",
			expected: "example.com",
		},
		{
			name:     "localhost with port",
			host:     "localhost:3000",
			expected: "localhost",
		},
		{
			name:     "IPv4 with port",
			host:     "192.168.1.1:8080",
			expected: "192.168.1.1",
		},
		{
			name:     "empty string",
			host:     "",
			expected: "",
		},
		{
			name:     "IPv6 with port",
			host:     "[::1]:8080",
			expected: "::1",
		},
		{
			name:     "IPv6 without port",
			host:     "[::1]",
			expected: "[::1]", // SplitHostPort returns error, so returns original
		},
		{
			name:     "IPv6 full address with port",
			host:     "[2001:db8::1]:8080",
			expected: "2001:db8::1",
		},
		{
			name:     "host with multiple colons (invalid but handled)",
			host:     "invalid:host:8080",
			expected: "invalid:host:8080", // SplitHostPort fails, returns original
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripPort(tt.host)
			if result != tt.expected {
				t.Errorf("stripPort(%q) = %q, want %q", tt.host, result, tt.expected)
			}
		})
	}
}
