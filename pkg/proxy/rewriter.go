package proxy

import (
	"net/http"
	"net/url"
	"strings"
)

const (
	// Docker Registry V2 API paths
	v2Prefix      = "/v2/"
	v2CatalogPath = "_catalog"
	tokenPath     = "/service/token"

	// Repository scope prefix
	scopeRepository = "repository:"
)

// Repository path boundaries - precomputed slice
var repositoryBoundaries = []string{
	"/manifests/",
	"/blobs/",
	"/tags/",
	"/referrers/",
	"/uploads/",
	"/_digest",
}

// rewriteRepositoryPath inserts the given prefix to the repository segment under /v2/<repo>/...
// It leaves /v2/ (API ping) and /v2/_catalog untouched.
func rewriteRepositoryPath(prefix, path string) string {
	if prefix == "" {
		return path
	}
	if !strings.HasPrefix(path, v2Prefix) {
		return path
	}
	if path == v2Prefix {
		return path
	}
	p := strings.TrimPrefix(path, v2Prefix)
	// Special top-level endpoint
	if strings.HasPrefix(p, v2CatalogPath) {
		return path
	}
	// Find the boundary after repository name; common segments:
	bpos := -1
	for _, b := range repositoryBoundaries {
		if idx := strings.Index(p, b); idx >= 0 {
			if bpos == -1 || idx < bpos {
				bpos = idx
			}
		}
	}
	if bpos == -1 {
		// Unknown shape; be conservative
		return path
	}
	repo := p[:bpos] // may include slashes like project/name
	if strings.HasPrefix(repo, prefix) {
		return path // already prefixed
	}
	newRepo := prefix + repo
	return v2Prefix + newRepo + p[bpos:]
}

// maybeRewriteTokenScope prefixes repository in the scope query for token service
// Returns true if scope was rewritten, false otherwise
// Optimized to minimize allocations
func maybeRewriteTokenScope(prefix string, req *http.Request) bool {
	if prefix == "" {
		return false
	}
	if !strings.HasPrefix(req.URL.Path, tokenPath) {
		return false
	}

	// Get scope from query string directly to avoid url.Values allocation
	rawQuery := req.URL.RawQuery
	if rawQuery == "" {
		return false
	}

	// Find scope parameter manually to avoid Query() allocation
	scopeStart := strings.Index(rawQuery, "scope=")
	if scopeStart == -1 {
		return false
	}
	scopeStart += 6 // len("scope=")

	// Find the end of scope value (either & or end of string)
	scopeEnd := strings.Index(rawQuery[scopeStart:], "&")
	var scopeEncoded string
	if scopeEnd == -1 {
		scopeEncoded = rawQuery[scopeStart:]
	} else {
		scopeEncoded = rawQuery[scopeStart : scopeStart+scopeEnd]
	}

	// URL decode the scope value
	scope, err := url.QueryUnescape(scopeEncoded)
	if err != nil {
		return false
	}

	// Check if it's a repository scope
	if !strings.HasPrefix(scope, scopeRepository) {
		return false
	}

	// Find repository name between first and second colon
	// Format: repository:project/name:pull,push
	firstColon := len(scopeRepository) // Position after "repository:"
	secondColon := strings.Index(scope[firstColon:], ":")
	if secondColon == -1 {
		return false
	}
	secondColon += firstColon

	repo := scope[firstColon:secondColon]
	if strings.HasPrefix(repo, prefix) {
		return false // already prefixed
	}

	// Build new scope with prefix using strings.Builder to minimize allocations
	var builder strings.Builder
	builder.Grow(len(scope) + len(prefix)) // Pre-allocate exact size
	builder.WriteString(scopeRepository)
	builder.WriteString(prefix)
	builder.WriteString(repo)
	builder.WriteString(scope[secondColon:]) // rest of the scope
	newScope := builder.String()

	// Replace scope in query string
	newScopeEncoded := url.QueryEscape(newScope)
	if scopeEnd == -1 {
		req.URL.RawQuery = rawQuery[:scopeStart-6] + "scope=" + newScopeEncoded
	} else {
		req.URL.RawQuery = rawQuery[:scopeStart-6] + "scope=" + newScopeEncoded + rawQuery[scopeStart+scopeEnd:]
	}
	return true
}
