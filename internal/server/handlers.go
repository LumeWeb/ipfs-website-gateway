package server

import (
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

// HandleGatewayRequest processes DNSLink vhost requests following the complete pipeline:
// 1. Extract domain from Host header
// 2. Validate DNSLink
// 3. Check status cache
// 4. Query internal API if cache miss
// 5. Check result (404 if not found, 410 if blocked)
// 6. Fetch content from IPFS
// 7. Serve with proper headers
func (s *Server) HandleGatewayRequest(c echo.Context) error {
	ctx := c.Request().Context()

	// Step 1: Extract domain from Host header
	host := c.Request().Host
	domain := extractDomain(host)
	if domain == "" {
		return s.errorResponse(c, http.StatusBadRequest, "invalid host header")
	}

	s.logger.Debug("processing gateway request", zap.String("domain", domain))

	// Step 3: Check status cache (before DNS validation for performance)
	cacheResult := s.statusCache.Get(domain)
	if cacheResult.Hit && !cacheResult.Expired {
		// Cache hit - use cached response
		s.logger.Debug("cache hit", zap.String("domain", domain))
		if cacheResult.Entry.Response == nil {
			return s.errorResponse(c, http.StatusNotFound, "domain not found")
		}
		return s.serveWebsite(c, cacheResult.Entry.Response)
	}

	// Step 2: Validate DNSLink
	_, err := s.dns.ValidateDNSLink(ctx, domain)
	if err != nil {
		s.logger.Debug("DNSLink validation failed", zap.String("domain", domain), zap.Error(err))
		return s.errorResponse(c, http.StatusNotFound, "domain not found")
	}

	// Step 4: Query internal API if cache miss
	website, err := s.api.GetWebsite(ctx, domain)
	if err != nil {
		s.logger.Debug("API query failed", zap.String("domain", domain), zap.Error(err))

		// Check error type and cache accordingly
		if strings.Contains(err.Error(), "website not found") {
			s.statusCache.SetInvalid(domain)
			return s.errorResponse(c, http.StatusNotFound, "domain not found")
		}
		if strings.Contains(err.Error(), "website is broken or gone") {
			s.statusCache.SetInvalid(domain)
			return s.errorResponse(c, http.StatusGone, "domain unavailable")
		}

		// Internal error
		return s.errorResponse(c, http.StatusInternalServerError, "internal server error")
	}

	// Step 5: Check result status
	if website.Status != types.StatusActive {
		s.logger.Debug("website not active", zap.String("domain", domain), zap.String("status", website.Status))
		s.statusCache.SetInvalid(domain)
		return s.errorResponse(c, http.StatusGone, "domain unavailable")
	}

	// Cache the successful response
	s.statusCache.Set(domain, website)

	// Step 6 & 7: Fetch content and serve
	return s.serveWebsite(c, website)
}

// serveWebsite fetches content from IPFS and serves it with proper headers
func (s *Server) serveWebsite(c echo.Context, website *types.GatewayWebsiteResponse) error {
	ctx := c.Request().Context()

	// Parse CID from target hash
	cid, err := cid.Decode(website.TargetHash)
	if err != nil {
		s.logger.Error("failed to parse CID", zap.String("hash", website.TargetHash), zap.Error(err))
		return s.errorResponse(c, http.StatusInternalServerError, "internal server error")
	}

	// Extract path from request URL
	requestPath := strings.TrimPrefix(c.Request().URL.Path, "/")
	pathComponents := strings.Split(requestPath, "/")
	if len(pathComponents) > 0 && pathComponents[0] == "" {
		pathComponents = pathComponents[1:]
	}

	// Fetch content from IPFS
	reader, filename, err := s.fetcher.FetchUnixFile(ctx, cid, pathComponents)
	if err != nil {
		s.logger.Error("failed to fetch content from IPFS", 
			zap.String("domain", website.Domain),
			zap.String("cid", cid.String()),
			zap.Error(err))
		return s.errorResponse(c, http.StatusBadGateway, "failed to fetch content")
	}
	defer func() { _ = reader.Close() }()

	// Determine content type
	contentType := getContentType(filename)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Get content length
	var contentLength int64
	if seeker, ok := reader.(io.Seeker); ok {
		currentPos, _ := seeker.Seek(0, io.SeekCurrent)
		endPos, _ := seeker.Seek(0, io.SeekEnd)
		_, _ = seeker.Seek(currentPos, io.SeekStart)
		contentLength = endPos - currentPos
	}

	// Set response headers
	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Content-Length", strconv.Itoa(int(contentLength)))
	c.Response().Header().Set("Cache-Control", "public, max-age=3600")
	c.Response().Header().Set("ETag", cid.String())

	// Set Content-Disposition if we have a filename
	if filename != "" {
		c.Response().Header().Set("Content-Disposition", "inline; filename=\""+filename+"\"")
	}

	// Stream the content
	c.Response().WriteHeader(http.StatusOK)
	_, err = io.Copy(c.Response(), reader)
	if err != nil {
		s.logger.Error("error streaming content", zap.Error(err))
		return err
	}

	return nil
}

// errorResponse returns a standardized error response
func (s *Server) errorResponse(c echo.Context, status int, message string) error {
	statusStr := strconv.Itoa(status)
	return c.HTML(status, "<!DOCTYPE html><html><head><title>"+statusStr+"</title></head><body><h1>"+statusStr+"</h1><p>"+message+"</p></body></html>")
}

// extractDomain extracts the domain from a host header, removing port if present
func extractDomain(host string) string {
	if host == "" {
		return ""
	}
	// Split on colon to remove port
	if idx := strings.Index(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}

// getContentType returns the appropriate Content-Type header based on file extension
func getContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".webp":
		return "image/webp"
	case ".woff", ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	default:
		return ""
	}
}
