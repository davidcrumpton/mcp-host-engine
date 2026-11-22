// transport/transport.go
package transport

import (
	"context"
	"crypto/subtle"
	"net/http"
	"regexp"
	"strings"

	"mcphe/auth"
	"mcphe/config"
)

type contextKey string

const IdentityContextKey contextKey = "auth_identity"
const SessionIDContextKey contextKey = "session_id"

// sessionIDPattern is what a session id may contain. The value arrives from a
// query parameter (or a header) chosen by the client, and is then written into
// log lines and used to pick which transport handles the request, so it is
// restricted to characters that cannot forge a log line or a URL.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// ValidSessionID reports whether s is an acceptable client-supplied session id.
func ValidSessionID(s string) bool {
	return sessionIDPattern.MatchString(s)
}

// SessionIDFromRequest returns the session id the request carries, or "" when
// it carries none. A value that does not match ValidSessionID is refused as if
// it were absent: query values are percent-decoded, so an unvalidated id such as
// "x%0a2026-Nov-22%20[WARN]%20admin" would otherwise inject a fabricated log
// line (and, in the HTTP handler, steer routing).
func SessionIDFromRequest(r *http.Request) string {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("sessionId")
	}
	if sessionID == "" {
		sessionID = r.URL.Query().Get("sessionid")
	}
	if sessionID == "" {
		return ""
	}
	if !ValidSessionID(sessionID) {
		return ""
	}
	return sessionID
}

func ValidateToken(progname, version string, next http.Handler, secret, legacyToken string, revoked auth.Revoked, cfg config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		sessionID := SessionIDFromRequest(r)
		if sessionID == "" {
			raw := r.Header.Get("Mcp-Session-Id")
			if raw == "" {
				raw = r.URL.Query().Get("sessionId")
			}
			if raw == "" {
				raw = r.URL.Query().Get("sessionid")
			}
			if raw != "" {
				cfg.LogfWithContext(1, "unknown", "-", "Ignoring malformed session id (%d bytes)", len(raw))
			}
		}

		r = r.WithContext(context.WithValue(r.Context(), IdentityContextKey, "unknown"))
		r = r.WithContext(context.WithValue(r.Context(), SessionIDContextKey, sessionID))

		if secret != "" {
			if id, err := auth.Validate(progname, version, token, secret, revoked); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), IdentityContextKey, id.Username))
				cfg.LogfWithContext(4, id.Username, sessionID, "Token validation successful")
				next.ServeHTTP(w, r)
				return
			} else {
				cfg.LogfWithContext(1, "unknown", sessionID, "Token validation failed: %v", err)
			}
		}
		if legacyToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(legacyToken)) == 1 {
			r = r.WithContext(context.WithValue(r.Context(), IdentityContextKey, "legacy"))
			cfg.LogfWithContext(2, "legacy", sessionID, "Using legacy bearer token")
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

// CORSMiddleware adds CORS headers and answers preflight requests.
//
// It must be the *outermost* middleware. When it sat inside ValidateToken, the
// OPTIONS preflight a browser sends -- which carries no Authorization header by
// design -- was answered 401 and never reached this handler, so the
// http.MethodOptions branch below was dead code and no browser could talk to the
// server cross-origin.
//
// allowedOrigin should be the specific origin allowed to make requests (e.g.
// "http://localhost:3000"). "*" is accepted only when allowAny is set: a
// wildcard origin plus a bearer token is one misconfiguration away from a
// cross-site tool-invocation channel. An empty string disables the CORS header
// entirely.
func CORSMiddleware(next http.Handler, allowedOrigin string, allowAny bool) http.Handler {
	if allowedOrigin == "*" && !allowAny {
		// Fail closed. main.go refuses to start on this combination, so this is
		// the belt-and-braces path for a caller that builds the chain itself.
		allowedOrigin = ""
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if allowedOrigin != "" {
			// Echo the configured origin only. Reflecting an arbitrary Origin
			// header back would turn every site into an allowed origin.
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cache-Control, Mcp-Session-Id")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// A cross-origin request that names an origin other than the configured
		// one is refused for state-changing methods. CORS headers alone are
		// only a browser-side hint; this is the server-side half. Requests with
		// no Origin header (every non-browser client) are unaffected.
		if origin != "" && allowedOrigin != "" && allowedOrigin != "*" &&
			!strings.EqualFold(origin, allowedOrigin) && isStateChanging(r.Method) {
			http.Error(w, "Origin not allowed", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}
