// transport/transport.go
package transport

import (
	"context"
	"net/http"
	"strings"

	"mcphe/auth"
)

type contextKey string

const IdentityContextKey contextKey = "auth_identity"

func ValidateToken(progname, version string, next http.Handler, secret, legacyToken string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")

		r = r.WithContext(context.WithValue(r.Context(), IdentityContextKey, "unknown"))
		if secret != "" {
			if id, err := auth.Validate(progname, version, token, secret, nil); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), IdentityContextKey, id.Username))
				next.ServeHTTP(w, r)
				return
			}
		}
		if legacyToken != "" && token == legacyToken {
			r = r.WithContext(context.WithValue(r.Context(), IdentityContextKey, "legacy"))
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

// CORSMiddleware adds CORS headers. allowedOrigin should be set to the specific
// origin that is allowed to make requests (e.g. "http://localhost:3000"), or "*"
// only when the caller explicitly opts into wide-open CORS. Passing an empty
// string disables the Access-Control-Allow-Origin header entirely.
func CORSMiddleware(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cache-Control, Mcp-Session-Id")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
