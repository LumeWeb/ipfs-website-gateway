package server

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"go.uber.org/zap"
)

const (
	// allowedSecretQueryParam is the query parameter used for /allowed endpoint authentication.
	allowedSecretQueryParam = "secret"
)

// authMiddleware creates middleware that validates the secret query parameter.
// This is used to protect the /allowed endpoint from unauthorized access.
func (s *Server) authMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		// Skip authentication if no secret is configured (for development/testing)
		if s.config.Server.AllowedSecret == "" {
			s.logger.Warn("AllowedSecret not configured, skipping authentication for /allowed endpoint")
			return next(c)
		}

		// Extract and validate the secret query parameter
		secret := c.QueryParam(allowedSecretQueryParam)
		if secret == "" || secret != s.config.Server.AllowedSecret {
			// Log authentication failure without revealing the reason
			s.logger.Warn("Authentication failed",
				zap.String("client_ip", c.RealIP()),
			)
			// Return generic 400 error without revealing authentication details
			return c.NoContent(http.StatusBadRequest)
		}

		// Authentication successful, proceed to next handler
		return next(c)
	}
}
