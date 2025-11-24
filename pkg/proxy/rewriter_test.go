package proxy

import (
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestRewriteRepositoryPath tests the repository path rewriting logic
func TestRewriteRepositoryPath(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		path     string
		expected string
	}{
		{
			name:     "no prefix - should not modify",
			prefix:   "",
			path:     "/v2/myrepo/manifests/latest",
			expected: "/v2/myrepo/manifests/latest",
		},
		{
			name:     "API ping - should not modify",
			prefix:   "prefix-",
			path:     "/v2/",
			expected: "/v2/",
		},
		{
			name:     "catalog endpoint - should not modify",
			prefix:   "prefix-",
			path:     "/v2/_catalog",
			expected: "/v2/_catalog",
		},
		{
			name:     "simple manifest path",
			prefix:   "team-a-",
			path:     "/v2/myrepo/manifests/latest",
			expected: "/v2/team-a-myrepo/manifests/latest",
		},
		{
			name:     "nested repository manifest",
			prefix:   "team-a-",
			path:     "/v2/project/subproject/image/manifests/v1.0.0",
			expected: "/v2/team-a-project/subproject/image/manifests/v1.0.0",
		},
		{
			name:     "blob path",
			prefix:   "prefix-",
			path:     "/v2/myrepo/blobs/sha256:abc123",
			expected: "/v2/prefix-myrepo/blobs/sha256:abc123",
		},
		{
			name:     "tags list",
			prefix:   "prod-",
			path:     "/v2/nginx/tags/list",
			expected: "/v2/prod-nginx/tags/list",
		},
		{
			name:     "upload endpoint",
			prefix:   "dev-",
			path:     "/v2/myimage/blobs/uploads/",
			expected: "/v2/dev-myimage/blobs/uploads/",
		},
		{
			name:     "already prefixed - should not double prefix",
			prefix:   "team-a-",
			path:     "/v2/team-a-myrepo/manifests/latest",
			expected: "/v2/team-a-myrepo/manifests/latest",
		},
		{
			name:     "referrers endpoint",
			prefix:   "test-",
			path:     "/v2/myrepo/referrers/sha256:abc",
			expected: "/v2/test-myrepo/referrers/sha256:abc",
		},
		{
			name:     "non-v2 path - should not modify",
			prefix:   "prefix-",
			path:     "/api/v1/projects",
			expected: "/api/v1/projects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rewriteRepositoryPath(tt.prefix, tt.path)
			if result != tt.expected {
				t.Errorf("rewriteRepositoryPath(%q, %q) = %q, want %q",
					tt.prefix, tt.path, result, tt.expected)
			}
		})
	}
}

// TestMaybeRewriteTokenScope tests the token scope rewriting logic
func TestMaybeRewriteTokenScope(t *testing.T) {
	tests := []struct {
		name          string
		prefix        string
		path          string
		scope         string
		expectedScope string
	}{
		{
			name:          "no prefix - should not modify",
			prefix:        "",
			path:          "/service/token",
			scope:         "repository:myrepo:pull",
			expectedScope: "repository:myrepo:pull",
		},
		{
			name:          "non-token path - should not modify",
			prefix:        "prefix-",
			path:          "/v2/myrepo/manifests/latest",
			scope:         "repository:myrepo:pull",
			expectedScope: "repository:myrepo:pull",
		},
		{
			name:          "simple repository scope",
			prefix:        "team-a-",
			path:          "/service/token",
			scope:         "repository:myrepo:pull",
			expectedScope: "repository:team-a-myrepo:pull",
		},
		{
			name:          "nested repository scope",
			prefix:        "prod-",
			path:          "/service/token",
			scope:         "repository:project/image:pull,push",
			expectedScope: "repository:prod-project/image:pull,push",
		},
		{
			name:          "already prefixed - should not double prefix",
			prefix:        "team-a-",
			path:          "/service/token",
			scope:         "repository:team-a-myrepo:pull",
			expectedScope: "repository:team-a-myrepo:pull",
		},
		{
			name:          "invalid scope format - should not modify",
			prefix:        "prefix-",
			path:          "/service/token",
			scope:         "invalid-scope",
			expectedScope: "invalid-scope",
		},
		{
			name:          "empty scope - should not modify",
			prefix:        "prefix-",
			path:          "/service/token",
			scope:         "",
			expectedScope: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test request
			req := httptest.NewRequest("GET", tt.path+"?scope="+url.QueryEscape(tt.scope), nil)

			// Apply the scope rewrite
			maybeRewriteTokenScope(tt.prefix, req)

			// Check the result
			actualScope := req.URL.Query().Get("scope")
			if actualScope != tt.expectedScope {
				t.Errorf("maybeRewriteTokenScope() scope = %q, want %q", actualScope, tt.expectedScope)
			}
		})
	}
}
