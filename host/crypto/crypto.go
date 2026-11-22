package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// maxRandomBytes caps one randomBytes() call. The length comes straight from a
// tool argument, so an unvalidated one is not just a wrong answer: make([]byte,
// n) panics for a negative or absurd n, goja re-panics a non-Exception panic
// instead of turning it into a JS throw, and the goroutine handling a tools/call
// is not net/http's -- so the whole daemon would die. Validate at the boundary.
const maxRandomBytes = 1 << 20 // 1 MiB

// RandomBytes returns n cryptographically random bytes as a lowercase hex
// string (so 2n characters).
//
// It used to return the raw bytes as a Go string. goja converts a Go string to
// a JS string through UTF-8, and invalid UTF-8 becomes U+FFFD: roughly 2% of the
// bytes in any 32-byte draw became the same replacement character, so the result
// was neither uniform nor 32 bytes of entropy, and using it for a nonce or a
// token produced something that looked random and was not. Hex has no such
// failure mode and is the form callers wanted anyway.
func RandomBytes(n int) (string, error) {
	if n < 0 || n > maxRandomBytes {
		return "", fmt.Errorf("randomBytes: length must be between 0 and %d, got %d", maxRandomBytes, n)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Sha256(data string) (string, error) {
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil)), nil
}
