package auth

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/DharmilG/go-commerce/internal/httpx"
)

// Middleware requires a valid Bearer token and puts the identity in the
// request context.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "A valid Bearer token is required", nil)
			return
		}

		claims, err := s.tokens.Parse(parts[1])
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "A valid Bearer token is required", nil)
			return
		}

		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "A valid Bearer token is required", nil)
			return
		}

		user := httpx.User{
			ID:    userID,
			Email: claims.Email,
			Name:  claims.Name,
			Role:  claims.Role,
		}
		next.ServeHTTP(w, r.WithContext(httpx.WithUser(r.Context(), user)))
	})
}

// RequireAdmin must run after Middleware.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := httpx.UserFromContext(r.Context())
		if !ok {
			httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
			return
		}
		if user.Role != "admin" {
			httpx.Error(w, r, http.StatusForbidden, "FORBIDDEN", "Administrator access is required", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
