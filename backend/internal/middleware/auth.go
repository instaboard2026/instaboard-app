package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"instaboard/internal/config"
	"instaboard/pkg/utils"
)

type contextKey string

const UserIDKey contextKey = "user_id"

// RequireJWT returns a middleware that validates the Bearer JWT and sets user ID in context.
// On missing or invalid token, responds with 401 and does not call next.
func RequireJWT(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				utils.WriteError(w, http.StatusUnauthorized, "authorization required", nil)
				return
			}
			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				utils.WriteError(w, http.StatusUnauthorized, "invalid authorization header", nil)
				return
			}
			tokenString := strings.TrimPrefix(auth, prefix)
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(cfg.JWTSecret), nil
			})
			if err != nil || !token.Valid {
				utils.WriteError(w, http.StatusUnauthorized, "invalid or expired token", nil)
				return
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				utils.WriteError(w, http.StatusUnauthorized, "invalid token claims", nil)
				return
			}
			sub, ok := claims["sub"]
			if !ok {
				utils.WriteError(w, http.StatusUnauthorized, "missing sub claim", nil)
				return
			}
			var userID int64
			switch v := sub.(type) {
			case string:
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					utils.WriteError(w, http.StatusUnauthorized, "invalid sub claim", nil)
					return
				}
				userID = n
			case float64:
				if v < 0 || v != float64(int64(v)) {
					utils.WriteError(w, http.StatusUnauthorized, "invalid sub claim", nil)
					return
				}
				userID = int64(v)
			default:
				utils.WriteError(w, http.StatusUnauthorized, "invalid sub claim", nil)
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalJWT returns a middleware that parses the Bearer JWT if present and sets user ID in context.
// On missing or invalid token, continues without user ID (no 401).
func OptionalJWT(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}
			tokenString := strings.TrimPrefix(auth, "Bearer ")
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(cfg.JWTSecret), nil
			})
			if err != nil || !token.Valid {
				next.ServeHTTP(w, r)
				return
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			sub, ok := claims["sub"]
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			var userID int64
			switch v := sub.(type) {
			case string:
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					next.ServeHTTP(w, r)
					return
				}
				userID = n
			case float64:
				if v >= 0 && v == float64(int64(v)) {
					userID = int64(v)
				}
			}
			if userID != 0 {
				ctx := context.WithValue(r.Context(), UserIDKey, userID)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserIDFromContext returns the user ID set by RequireJWT or OptionalJWT, or 0 if not set.
func UserIDFromContext(ctx context.Context) int64 {
	v := ctx.Value(UserIDKey)
	if v == nil {
		return 0
	}
	id, _ := v.(int64)
	return id
}
