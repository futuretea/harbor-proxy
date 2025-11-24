package config

import (
	"testing"
)

func TestParseHostPrefixMapString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:  "valid single pair",
			input: "hosta.local=team-a-",
			expected: map[string]string{
				"hosta.local": "team-a-",
			},
		},
		{
			name:  "valid multiple pairs",
			input: "hosta.local=team-a-,hostb.local=team-b-",
			expected: map[string]string{
				"hosta.local": "team-a-",
				"hostb.local": "team-b-",
			},
		},
		{
			name:  "with spaces",
			input: "hosta.local = team-a- , hostb.local = team-b-",
			expected: map[string]string{
				"hosta.local": "team-a-",
				"hostb.local": "team-b-",
			},
		},
		{
			name:     "empty string",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:  "empty prefix value",
			input: "hosta.local=,hostb.local=team-b-",
			expected: map[string]string{
				"hosta.local": "",
				"hostb.local": "team-b-",
			},
		},
		{
			name:  "trailing comma",
			input: "hosta.local=team-a-,",
			expected: map[string]string{
				"hosta.local": "team-a-",
			},
		},
		{
			name:     "invalid format (no equals)",
			input:    "hosta.local",
			expected: map[string]string{},
		},
		{
			name:  "mixed valid and invalid",
			input: "hosta.local=team-a-,invalid,hostb.local=team-b-",
			expected: map[string]string{
				"hosta.local": "team-a-",
				"hostb.local": "team-b-",
			},
		},
		{
			name:  "host with port",
			input: "hosta.local:8099=team-a-,hostb.local:9000=team-b-",
			expected: map[string]string{
				"hosta.local:8099": "team-a-",
				"hostb.local:9000": "team-b-",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseHostPrefixMapString(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("got %d entries, want %d", len(result), len(tt.expected))
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("for key %q: got %q, want %q", k, result[k], v)
				}
			}
		})
	}
}

func TestGetHostPrefixMap(t *testing.T) {
	tests := []struct {
		name     string
		inputMap map[string]string
		expected map[string]string
	}{
		{
			name: "host without port",
			inputMap: map[string]string{
				"hosta.local": "team-a-",
				"hostb.local": "team-b-",
			},
			expected: map[string]string{
				"hosta.local": "team-a-",
				"hostb.local": "team-b-",
			},
		},
		{
			name: "host with port preserved for exact matching",
			inputMap: map[string]string{
				"hosta.local:8099": "team-a-",
				"hostb.local:9000": "team-b-",
			},
			expected: map[string]string{
				"hosta.local:8099": "team-a-",
				"hostb.local:9000": "team-b-",
			},
		},
		{
			name: "mixed hosts with and without port",
			inputMap: map[string]string{
				"hosta.local:8099": "team-a-",
				"hostb.local":      "team-b-",
			},
			expected: map[string]string{
				"hosta.local:8099": "team-a-",
				"hostb.local":      "team-b-",
			},
		},
		{
			name: "case insensitive normalization with port",
			inputMap: map[string]string{
				"HostA.Local:8099": "team-a-",
				"HOSTB.LOCAL":      "team-b-",
			},
			expected: map[string]string{
				"hosta.local:8099": "team-a-",
				"hostb.local":      "team-b-",
			},
		},
		{
			name: "IPv6 with port preserved",
			inputMap: map[string]string{
				"[::1]:8099":         "ipv6-a-",
				"[2001:db8::1]:9000": "ipv6-b-",
			},
			expected: map[string]string{
				"[::1]:8099":         "ipv6-a-",
				"[2001:db8::1]:9000": "ipv6-b-",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				HostPrefixMap: tt.inputMap,
			}
			result := cfg.GetHostPrefixMap()

			if len(result) != len(tt.expected) {
				t.Errorf("got %d entries, want %d", len(result), len(tt.expected))
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("for key %q: got %q, want %q", k, result[k], v)
				}
			}
		})
	}
}
