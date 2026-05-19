package gateway

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"

	gwheaders "github.com/tj/go-headers"
	"go.uber.org/zap"
)

const HeadersFilename = "_headers"

var HardcodedSecurityHeaders = map[string]string{
	"X-Content-Type-Options":             "nosniff",
	"Referrer-Policy":                    "strict-origin-when-cross-origin",
	"X-Download-Options":                 "noopen",
	"X-Permitted-Cross-Domain-Policies": "none",
}

var AllowedUserHeaders = map[string]bool{
	"content-security-policy":              true,
	"cross-origin-resource-policy":        true,
	"cross-origin-opener-policy":          true,
	"cross-origin-embedder-policy":        true,
	"permissions-policy":                  true,
	"x-frame-options":                    true,
	"x-xss-protection":                   true,
	"referrer-policy":                    true,
	"access-control-allow-origin":         true,
	"access-control-allow-methods":      true,
	"access-control-allow-headers":      true,
	"access-control-max-age":            true,
}

var BlockedHeaders = map[string]bool{
	"strict-transport-security": true,
	"clear-site-data":          true,
	"cache-control":            true,
}

type pathRule struct {
	pattern string
	headers http.Header
}

type HeadersMiddleware struct {
	rules  []pathRule
	logger *zap.Logger
}

func NewHeadersMiddleware(logger *zap.Logger) *HeadersMiddleware {
	return &HeadersMiddleware{
		rules:  make([]pathRule, 0),
		logger: logger,
	}
}

func (h *HeadersMiddleware) Parse(r io.Reader) error {
	rules, err := gwheaders.Parse(r)
	if err != nil {
		return fmt.Errorf("failed to parse %s file: %w", HeadersFilename, err)
	}

	var parsed []pathRule

	for pathPattern, headerSet := range rules {
		filtered := make(http.Header)

		for key, values := range headerSet {
			norm := strings.ToLower(key)

			if BlockedHeaders[norm] {
				h.logger.Warn("blocked header in _headers file (operator-only)",
					zap.String("header", key),
					zap.String("path", pathPattern))
				continue
			}

			if !h.isHeaderAllowed(norm) {
				h.logger.Warn("unrecognized header in _headers file (skipping)",
					zap.String("header", key),
					zap.String("path", pathPattern))
				continue
			}

			if norm == "x-xss-protection" {
				for _, v := range values {
					if !strings.Contains(strings.ToLower(v), "mode=block") {
						h.logger.Warn("x-xss-protection must include 'mode=block' (skipping)",
							zap.String("value", v))
						continue
					}
					filtered.Add(key, v)
				}
				continue
			}

			for _, v := range values {
				filtered.Add(key, v)
			}
		}

		if len(filtered) > 0 {
			parsed = append(parsed, pathRule{pattern: pathPattern, headers: filtered})
		}
	}

	sort.Slice(parsed, func(i, j int) bool {
		return len(parsed[i].pattern) < len(parsed[j].pattern)
	})

	h.rules = parsed

	return nil
}

func (h *HeadersMiddleware) isHeaderAllowed(norm string) bool {
	if AllowedUserHeaders[norm] {
		return true
	}
	return strings.HasPrefix(norm, "x-")
}

func (h *HeadersMiddleware) ApplyHeaders(w http.ResponseWriter, requestPath string) {
	for key, value := range HardcodedSecurityHeaders {
		w.Header().Set(key, value)
	}

	for _, rule := range h.rules {
		if matchPath(rule.pattern, requestPath) {
			for key, values := range rule.headers {
				if _, hardcoded := HardcodedSecurityHeaders[key]; hardcoded {
					h.logger.Debug("user header skipped (would override hardcoded)",
						zap.String("header", key))
					continue
				}
				w.Header().Del(key)
				for _, v := range values {
					w.Header().Add(key, v)
				}
			}
		}
	}
}

func (h *HeadersMiddleware) HasRules() bool {
	return len(h.rules) > 0
}

func ApplyDefaultSecurityHeaders(w http.ResponseWriter) {
	for key, value := range HardcodedSecurityHeaders {
		w.Header().Set(key, value)
	}
}

func matchPath(pattern, requestPath string) bool {
	pattern = path.Clean("/" + pattern)
	requestPath = path.Clean("/" + requestPath)

	if pattern == requestPath {
		return true
	}

	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		if prefix == "" || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}

	if strings.Contains(pattern, "*") {
		matched, _ := path.Match(pattern, requestPath)
		return matched
	}

	return false
}

func ValidateHeadersFile(content string) []string {
	var warnings []string

	rules, err := gwheaders.Parse(strings.NewReader(content))
	if err != nil {
		return []string{fmt.Sprintf("parse error: %v", err)}
	}

	for pathPattern, headerSet := range rules {
		for key := range headerSet {
			norm := strings.ToLower(key)
			if BlockedHeaders[norm] {
				warnings = append(warnings,
					fmt.Sprintf("warning: %s is operator-only and will be ignored at path %s", key, pathPattern))
			}
			if !AllowedUserHeaders[norm] && !strings.HasPrefix(norm, "x-") {
				warnings = append(warnings,
					fmt.Sprintf("warning: %s is not in safelist and will be ignored at path %s", key, pathPattern))
			}
		}
	}

	return warnings
}
