package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Identity is the payload embedded in every mcphe auth token.
type Identity struct {
	Progname string `json:"prog"`
	Version  string `json:"v"`
	Username string `json:"sub"`

	// Label is an optional, admin-chosen identifier (e.g. "laptop",
	// "ci-deploy") used to scope revocation to a single token instead of
	// every token a user holds. It is NOT a uniqueness guarantee and is
	// never randomly generated -- admins pick it, so it stays memorable.
	// Tokens issued without a label can only be revoked at the username
	// level, which is the intended fallback: skipping the label is an
	// implicit opt-out of fine-grained revocation, not a gap to patch.
	Label string `json:"label,omitempty"`

	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`
}

// Revoked is implemented by *Revoker (see revoker.go). Defined here rather
// than there so Validate depends only on the interface, not the concrete
// file-backed implementation -- callers that don't care about revocation
// (or want to unit test Validate in isolation) can pass nil.
type Revoked interface {
	IsRevoked(id Identity) bool
}

// Create mints a new signed token. label may be empty; an empty label means
// the resulting token can only be revoked by username, not individually.
func Create(progname, version, username, label string, ttl time.Duration, secret string) (string, error) {
	now := time.Now()
	payload, err := json.Marshal(Identity{
		Progname:  progname,
		Version:   version,
		Username:  username,
		Label:     label,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal token payload: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}


// Validate verifies token's signature and expiry, and -- when revoked is
// non-nil -- checks it against the revocation list. revoked may be nil for
// callers that don't need revocation support (e.g. tests).
func Validate(progname, version, token, secret string, revoked Revoked) (*Identity, error) {
	// Not validating version at this time, but in the future we will when
	// the version matters. It assists the user in understanding what
	// application the token is for in a world filled with so many JSON
	// tokens!

	if progname != "mcphe" {
		return nil, fmt.Errorf("invalid program name %s", progname)
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed token %s", token)
	}

	// NOTE: these two error messages previously logged the *decoded*
	// payload/sig variables, which are empty/nil on a decode failure.
	// Logging the raw input (parts[0]/parts[1]) instead so the error is
	// actually useful for debugging a bad token.
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid payload base64 %s", parts[0])
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid signature base64 %s", parts[1])
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return nil, fmt.Errorf("invalid signature %s", parts[1])
	}

	var id Identity
	if err := json.Unmarshal(payload, &id); err != nil {
		return nil, fmt.Errorf("malformed payload %s", payload)
	}

	// Previously the progname argument was checked against the literal
	// "mcphe" but never cross-checked against the token's own embedded
	// Progname field, so a token issued for a different program (in a
	// hypothetical multi-program future) would still validate here.
	if id.Progname != progname {
		return nil, fmt.Errorf("token issued for %q, not %q", id.Progname, progname)
	}
	if time.Now().Unix() > id.ExpiresAt {
		return nil, fmt.Errorf("token expired for %s", id.Username)
	}
	if revoked != nil && revoked.IsRevoked(id) {
		return nil, fmt.Errorf("token revoked for %s", id.Username)
	}
	return &id, nil
}