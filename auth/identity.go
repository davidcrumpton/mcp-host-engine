package auth

import (
	"fmt"
	"strings"
)

// Identity names are restricted to a character set that cannot be mistaken for
// the revocation file's own syntax.
//
// The revocation file is line-based with the key in the first field and an
// optional expiry in the second, and ":" separates a username from a token
// label. Nothing validated either half, so `token create "alice:laptop" x` minted
// a token whose *username* contains a colon: revoking the username "alice:laptop"
// then matched both the blanket key for a user called "alice:laptop" and the
// label key for user "alice", and a username containing a space or a newline
// rewrote the file into entries nobody asked for.
//
// The set below is what the rest of the system already assumes: it is the same
// shape transport.SessionIDFromRequest accepts, plus the characters a username
// or label plausibly uses ("@", ".", "+", "-", "_").
const maxIdentityNameLen = 64

// ValidateIdentityName rejects a username or label that cannot be written to the
// revocation file unambiguously.
func ValidateIdentityName(kind, name string) error {
	if name == "" {
		return fmt.Errorf("%s is empty", kind)
	}
	if len(name) > maxIdentityNameLen {
		return fmt.Errorf("%s %q is longer than %d characters", kind, name, maxIdentityNameLen)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '@', r == '+':
		default:
			return fmt.Errorf("%s %q contains %q: use letters, digits, '-', '_', '.', '@' or '+'", kind, name, r)
		}
	}
	return nil
}

// ValidateRevocationKey checks a key of the form "username" or "username:label".
// A bare username is a blanket revoke; the labelled form targets one token.
func ValidateRevocationKey(key string) error {
	if key == "" {
		return fmt.Errorf("revocation key is empty")
	}
	username, label, hasLabel := strings.Cut(key, ":")
	if err := ValidateIdentityName("username", username); err != nil {
		return err
	}
	if hasLabel {
		if label == "" {
			return fmt.Errorf("revocation key %q has a ':' but no label", key)
		}
		if err := ValidateIdentityName("label", label); err != nil {
			return err
		}
	}
	return nil
}
