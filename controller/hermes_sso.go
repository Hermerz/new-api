package controller

// hermes_sso.go — one-shot login bridge for the New-API admin UI (Hermes SSO, Story TBD).
//
// Hermes signs a short-lived JWT (aud="new-api-sso", exp=30s) with HERMES_JWT_SECRET and
// redirects the admin's browser to /api/user/hermes-sso?token=<jwt>&redirect=<path>.
// We validate the token, load the user, install a session cookie via gin-contrib/sessions,
// and 302 to the requested in-app path. This lets admins bounce from Hermes console into
// /newapi-admin/ without re-entering credentials.
//
// Required env: HERMES_JWT_SECRET. When unset we reject (SSO disabled).
// Paired with HERMES_ADMIN_SSO_ONLY=true in setupLogin, which blocks every other login
// path for admin accounts — SSO becomes the only way in.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const hermesSSOAudience = "new-api-sso"

type hermesSSOClaims struct {
	UserID int    `json:"uid"`
	Role   int    `json:"role"`
	Type   string `json:"type"`
	jwt.RegisteredClaims
}

// HermesSSO: GET /api/user/hermes-sso?token=<jwt>&redirect=<path>
// 302 to redirect (default "/") after installing the session. Rejects non-admins.
func HermesSSO(c *gin.Context) {
	secret := os.Getenv("HERMES_JWT_SECRET")
	if secret == "" {
		c.String(http.StatusServiceUnavailable, "hermes sso disabled")
		return
	}
	tokenStr := c.Query("token")
	if tokenStr == "" {
		c.String(http.StatusBadRequest, "missing token")
		return
	}

	claims := &hermesSSOClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	},
		jwt.WithAudience(hermesSSOAudience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(5*time.Second),
	)
	if err != nil || !token.Valid {
		c.String(http.StatusUnauthorized, "invalid hermes sso token")
		return
	}
	if claims.Role < common.RoleAdminUser {
		c.String(http.StatusForbidden, "admin role required")
		return
	}

	var user model.User
	if err := model.DB.Where("id = ?", claims.UserID).First(&user).Error; err != nil {
		c.String(http.StatusNotFound, "user not found")
		return
	}
	if user.Status != common.UserStatusEnabled {
		c.String(http.StatusForbidden, "user disabled")
		return
	}
	if user.Role < common.RoleAdminUser {
		// defense in depth: JWT role may have drifted from DB; trust DB
		c.String(http.StatusForbidden, "admin role required")
		return
	}

	// Mark this request as the SSO entry so setupLogin (if reused) doesn't gate us.
	c.Set("hermes_sso", true)

	session := sessions.Default(c)
	session.Set("id", user.Id)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("status", user.Status)
	session.Set("group", user.Group)
	if err := session.Save(); err != nil {
		c.String(http.StatusInternalServerError, "session save failed")
		return
	}

	// The SPA reads login state from localStorage['user'] (see PageLayout.jsx loadUser),
	// NOT from the session cookie. Return a small bootstrap page that hydrates
	// localStorage with the same payload setupLogin emits and then navigates to the
	// target path. Without this the user lands on the login form even with a valid
	// session cookie.
	payload, _ := json.Marshal(map[string]any{
		"id":           user.Id,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"role":         user.Role,
		"status":       user.Status,
		"group":        user.Group,
	})
	dest := safeRedirect(c.Query("redirect"))
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>Signing in…</title></head><body><script>
try { localStorage.setItem('user', %s); } catch (e) {}
window.location.replace(%s);
</script></body></html>`,
		jsStringLiteral(string(payload)),
		jsStringLiteral(dest),
	))
}

// jsStringLiteral produces a safe JS string literal from an arbitrary Go string.
// Using JSON encoding here is safe because JSON strings are valid JS string literals.
func jsStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// safeRedirect restricts the post-SSO landing URL to same-origin app paths to avoid
// open-redirect abuse (JWT is short-lived but still, defense in depth).
func safeRedirect(raw string) string {
	if raw == "" {
		return "/"
	}
	// Reject protocol-relative or absolute URLs.
	if strings.HasPrefix(raw, "//") {
		return "/"
	}
	if !strings.HasPrefix(raw, "/") {
		return "/"
	}
	// Reject anything that could be interpreted as a scheme smuggled into the path.
	if strings.Contains(raw, "://") {
		return "/"
	}
	return raw
}
