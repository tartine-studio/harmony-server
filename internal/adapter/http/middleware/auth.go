package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

func IsAuthenticated(tokenProvider domain.TokenProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "missing or invalid authorization header", "UNAUTHORIZED")
				return
			}

			tokenString := strings.TrimPrefix(header, "Bearer ")
			claims, err := tokenProvider.ValidateToken(tokenString)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid or expired token", "UNAUTHORIZED")
				return
			}

			if claims.Type != domain.AccessToken {
				writeError(w, http.StatusUnauthorized, "invalid token type", "UNAUTHORIZED")
				return
			}

			ctx := NewUserContext(r.Context(), claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
		"code":  code,
	})
}
