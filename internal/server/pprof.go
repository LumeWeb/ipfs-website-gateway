package server

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/http/pprof"
	"strconv"
	"strings"
	"sync"

	"github.com/labstack/echo/v5"
	"go.uber.org/zap"
)

// gatewaySecretHeader is the header used to authenticate internal/debug
// endpoints. It carries the same shared gateway secret (Server.AllowedSecret)
// that the gateway uses when talking to the portal, so a single credential
// gates all protected endpoints.
const gatewaySecretHeader = "X-Gateway-Secret"

// defaultPprofPath is the only path prefix net/http/pprof can serve from. Its
// route checks, trailing-slash redirect, and rendered HTML links all hardcode
// this exact prefix.
const defaultPprofPath = "/debug/pprof"

// pprofAuthMiddleware protects the pprof debug endpoints. It requires the
// configured gateway secret to be presented in the X-Gateway-Secret header.
// Pprof exposes sensitive in-memory state, so unlike the /allowed endpoint it
// fails closed: when no secret is configured, access is refused rather than
// opened up.
func (s *Server) pprofAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.config.Server.AllowedSecret == "" {
			s.logger.Warn("AllowedSecret not configured, rejecting pprof access")
			return c.NoContent(http.StatusUnauthorized)
		}

		provided := c.Request().Header.Get(gatewaySecretHeader)
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(s.config.Server.AllowedSecret)) != 1 {
			s.logger.Warn("pprof authentication failed",
				zap.String("client_ip", c.RealIP()),
			)
			return c.NoContent(http.StatusUnauthorized)
		}

		return next(c)
	}
}

// profileFlight serializes CPU-profile and execution-trace requests. Go's
// runtime only allows one active CPU profile/trace globally, so concurrent
// requests would collide; serializing and rejecting overlaps prevents an
// authorized caller from saturating all cores with parallel blocked requests.
var profileFlight sync.Mutex

// pprofHandlers maps pprof sub-paths to their net/http/pprof handlers.
var pprofHandlers = map[string]http.HandlerFunc{
	"/cmdline":      pprof.Cmdline,
	"/profile":      singleFlightHandler(clampSecondsHandler(pprof.Profile)),
	"/symbol":       pprof.Symbol,
	"/trace":        singleFlightHandler(clampSecondsHandler(pprof.Trace)),
	"/allocs":       pprof.Handler("allocs").ServeHTTP,
	"/block":        pprof.Handler("block").ServeHTTP,
	"/goroutine":    pprof.Handler("goroutine").ServeHTTP,
	"/heap":         pprof.Handler("heap").ServeHTTP,
	"/mutex":        pprof.Handler("mutex").ServeHTTP,
	"/threadcreate": pprof.Handler("threadcreate").ServeHTTP,
}

// registerPprofRoutes registers the Go pprof debug endpoints on the echo
// router, gated by the same shared gateway secret used for other protected
// endpoints. base is the configured path prefix.
//
// net/http/pprof hardcodes the "/debug/pprof" prefix in its route checks,
// trailing-slash redirect, and rendered HTML links, so a non-default base
// cannot work correctly. Any other path is rejected at startup.
func (s *Server) registerPprofRoutes(e *echo.Echo, base string) error {
	base = strings.TrimSuffix(base, "/")

	if base != defaultPprofPath {
		return fmt.Errorf("pprof path must be %q, got %q (net/http/pprof hardcodes this prefix)", defaultPprofPath, base)
	}

	// The index is served at the trailing-slash route (net/http/pprof.Index only
	// serves paths ending in "/"), and the bare base redirects to it so
	// /debug/pprof and /debug/pprof/ both resolve. Both are auth-gated so a
	// request without a valid secret fails closed (401) rather than revealing
	// that pprof is enabled.
	index := s.pprofAuthMiddleware(echo.WrapHandler(http.HandlerFunc(pprof.Index)))
	e.GET(base+"/", index)
	e.GET(base, s.pprofAuthMiddleware(func(c *echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, base+"/")
	}))

	for sub, handler := range pprofHandlers {
		path := base + sub
		e.GET(path, s.pprofAuthMiddleware(echo.WrapHandler(handler)))
	}

	s.logger.Info("pprof endpoints enabled", zap.String("path", base))
	return nil
}

// maxProfileSeconds caps the attacker-controllable ?seconds= query parameter on
// the cpu-profile (/profile) and execution-trace (/trace) endpoints. Without a
// cap, any request that passes auth could hold CPU across all cores for the
// requested duration; clamping to the stdlib default prevents unbounded block.
const maxProfileSeconds = 30

// clampSecondsHandler wraps a pprof handler so that the ?seconds= query
// parameter is clamped to maxProfileSeconds before delegating, preventing a
// caller from forcing an unbounded CPU profile or trace.
func clampSecondsHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vals := r.URL.Query()
		if raw := vals.Get("seconds"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil {
				if n > maxProfileSeconds {
					vals.Set("seconds", strconv.Itoa(maxProfileSeconds))
					r.URL.RawQuery = vals.Encode()
				}
			}
		}
		next(w, r)
	}
}

// singleFlightHandler wraps a CPU-blocking pprof handler (profile/trace) so
// only one request runs at a time. If another in-flight request already holds
// the lock, the request is rejected with 409 Conflict rather than queued, since
// Go serializes CPU profiling globally and a backlog would only tie up cores.
func singleFlightHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !profileFlight.TryLock() {
			http.Error(w, "another CPU profile/trace request is already in progress", http.StatusConflict)
			return
		}
		defer profileFlight.Unlock()
		next(w, r)
	}
}
