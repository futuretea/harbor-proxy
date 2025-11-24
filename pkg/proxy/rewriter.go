package proxy

import (
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
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
func maybeRewriteTokenScope(prefix string, req *http.Request) {
	if prefix == "" {
		return
	}
	if !strings.HasPrefix(req.URL.Path, tokenPath) {
		return
	}
	q := req.URL.Query()
	scope := q.Get("scope")
	// scope format examples: repository:project/name:pull
	if !strings.HasPrefix(scope, scopeRepository) {
		return
	}

	parts := strings.Split(scope, ":")
	// expect ["repository", "project/name", "pull"] (len>=3)
	if len(parts) < 3 {
		return
	}

	repo := parts[1]
	if strings.HasPrefix(repo, prefix) {
		return // already prefixed
	}

	originalScope := scope
	parts[1] = prefix + repo
	q.Set("scope", strings.Join(parts, ":"))
	req.URL.RawQuery = q.Encode()
	log.Debug().
		Str("original_scope", originalScope).
		Str("rewritten_scope", q.Get("scope")).
		Str("prefix", prefix).
		Msg("token scope rewritten")
}
