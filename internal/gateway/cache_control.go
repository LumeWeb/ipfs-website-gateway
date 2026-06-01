package gateway

import (
	"net/http"
)

const mutableCacheControl = "public, max-age=0, must-revalidate"

// CacheControlMiddleware rewrites Cache-Control headers on gateway responses.
// All websites served through this gateway are mutable from the owner's
// perspective — they can publish new content at any time. Boxo sets
// TTL-based max-age on /ipns/ paths and immutable (336-day) max-age on
// /ipfs/ paths, but since the gateway routes both types as website
// content (not raw IPFS objects), every response should use
// max-age=0, must-revalidate so browsers revalidate via ETag on
// every visit.
type CacheControlMiddleware struct{}

func NewCacheControlMiddleware() *CacheControlMiddleware {
	return &CacheControlMiddleware{}
}

func (m *CacheControlMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&cacheControlRewriter{ResponseWriter: w}, r)
	})
}

type cacheControlRewriter struct {
	http.ResponseWriter
	headerWritten bool
}

func (c *cacheControlRewriter) WriteHeader(code int) {
	if c.headerWritten {
		return
	}
	c.headerWritten = true
	c.ResponseWriter.Header().Set("Cache-Control", mutableCacheControl)
	c.ResponseWriter.WriteHeader(code)
}

func (c *cacheControlRewriter) Write(p []byte) (int, error) {
	if !c.headerWritten {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(p)
}

func (c *cacheControlRewriter) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}
