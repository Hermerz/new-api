package middleware

// hermes_auth.go — AR7: validate X-Hermes-Token (Hermes internal JWT) on relay routes.
// Hermes issues JWTs with aud="new-api" to identify the originating Hermes user.
// This middleware runs AFTER TokenAuth() and enriches the context with the Hermes user ID.
//
// Required env var: HERMES_JWT_SECRET  (same secret used by Hermes api/)
// If the env var is not set the middleware is a no-op (allows gradual rollout).

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const hermesAudience = "new-api"

// hermesContextKey is the gin context key for the validated Hermes user ID.
const hermesContextKey = "hermes_user_id"

// hermesClaims mirrors the Hermes JWT payload (subset used here).
type hermesClaims struct {
	UserID int    `json:"uid"`
	Role   int    `json:"role"`
	Type   string `json:"type"`
	jwt.RegisteredClaims
}

// HermesTokenAuth validates X-Hermes-Token and injects hermes_user_id into context.
// Non-configured (no HERMES_JWT_SECRET): no-op.
// Token present but invalid: 401.
// Token absent: no-op (caller may be a direct API consumer without Hermes).
func HermesTokenAuth() gin.HandlerFunc {
	secret := os.Getenv("HERMES_JWT_SECRET")
	if secret == "" {
		// Not configured — middleware disabled.
		return func(c *gin.Context) { c.Next() }
	}
	key := []byte(secret)

	return func(c *gin.Context) {
		tokenStr := c.GetHeader("X-Hermes-Token")
		if tokenStr == "" {
			// No Hermes token — allow regular TokenAuth to handle this request.
			c.Next()
			return
		}

		claims := &hermesClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return key, nil
		},
			jwt.WithAudience(hermesAudience),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(5*time.Second),
		)
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_hermes_token"})
			c.Abort()
			return
		}

		// Reject special-purpose tokens (ws, totp_pending).
		if claims.Type != "" && claims.Type != "access" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "hermes_token_type_not_allowed"})
			c.Abort()
			return
		}

		c.Set(hermesContextKey, claims.UserID)
		c.Next()
	}
}

// GetHermesUserID returns the Hermes user ID from context, or 0 if not present.
func GetHermesUserID(c *gin.Context) int {
	if v, exists := c.Get(hermesContextKey); exists {
		if id, ok := v.(int); ok {
			return id
		}
	}
	return 0
}

// extractHermesBearerToken is a helper (unused externally) kept for reference.
func extractHermesBearerToken(s string) string {
	if strings.HasPrefix(s, "Bearer ") {
		return strings.TrimPrefix(s, "Bearer ")
	}
	return s
}
