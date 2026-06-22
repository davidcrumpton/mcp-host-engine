// transport/transport.go
package transport

import (
	"context"
	"net/http"
	"strings"

	"mcphe/auth"
	"mcphe/config"
)

type contextKey string

const IdentityContextKey contextKey = "auth_identity"
const SessionIDContextKey contextKey = "session_id"

func ValidateToken(progname, version string, next http.Handler, secret, legacyToken string, revoked auth.Revoked, cfg config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		sessionID := r.Header.Get("Mcp-Session-Id")
		if sessionID == "" {
			sessionID = r.URL.Query().Get("sessionId")
			if sessionID == "" {
				sessionID = r.URL.Query().Get("sessionid")
			}
		}

		r = r.WithContext(context.WithValue(r.Context(), IdentityContextKey, "unknown"))
		r = r.WithContext(context.WithValue(r.Context(), SessionIDContextKey, sessionID))

		if secret != "" {
			if id, err := auth.Validate(progname, version, token, secret, revoked); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), IdentityContextKey, id.Username))
				cfg.LogfWithContext(2, id.Username, sessionID, "Token validation successful for user")
				next.ServeHTTP(w, r)
				return
			} else {
				cfg.LogfWithContext(1, "unknown", sessionID, "Token validation failed: %v", err)
			}
		}
		if legacyToken != "" && token == legacyToken {
			r = r.WithContext(context.WithValue(r.Context(), IdentityContextKey, "legacy"))
			cfg.LogfWithContext(2, "legacy", sessionID, "Using legacy bearer token")
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
