package crypto

import (
	"encoding/hex"
	"testing"
)

func TestRandomBytes(t *testing.T) {
	b, err := RandomBytes(16)
	if err != nil {
		t.Fatalf("RandomBytes error: %v", err)
	}
	if len(b) != 32 {
		t.Errorf("RandomBytes(16) returned %d characters, want 32 hex characters", len(b))
	}
	if _, err := hex.DecodeString(b); err != nil {
		t.Errorf("RandomBytes returned non-hex output %q: %v", b, err)
	}
}

// L9: raw random bytes in a Go string lose entropy when goja converts them to a
// JS string -- invalid UTF-8 becomes U+FFFD. The output must be pure ASCII hex,
// and two draws must differ.
func TestRandomBytesIsHexAndDoesNotRepeat(t *testing.T) {
	a, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes error: %v", err)
	}
	b, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes error: %v", err)
	}
	if a == b {
		t.Error("two 32-byte draws produced the same value")
	}
	for _, r := range a {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			t.Fatalf("RandomBytes output %q contains %q, which is not lowercase hex", a, r)
		}
	}
}

// H5: the length comes from a tool argument. A negative or absurd value used to
// reach make([]byte, n) and panic, which goja re-panics and nothing recovers --
// the daemon died. It must be a plain error now.
func TestRandomBytesRejectsOutOfRangeLength(t *testing.T) {
	for _, n := range []int{-1, -1024, maxRandomBytes + 1} {
		out, err := RandomBytes(n)
		if err == nil {
			t.Errorf("RandomBytes(%d): expected an error, got %d bytes", n, len(out))
		}
	}
}

func TestRandomBytesZeroIsEmpty(t *testing.T) {
	out, err := RandomBytes(0)
	if err != nil {
		t.Fatalf("RandomBytes(0): unexpected error: %v", err)
	}
	if out != "" {
		t.Errorf("RandomBytes(0): got %q, want an empty string", out)
	}
}

func TestSha256(t *testing.T) {
	s, err := Sha256("hello")
	if err != nil {
		t.Errorf("Sha256 error: %v", err)
	}
	if s != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Errorf("Sha256 error: got %s, want %s", s, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
	}
}

// test with tool

func TestCrypto(t *testing.T) {
	b, err := RandomBytes(16)
	if err != nil {
		t.Errorf("RandomBytes error: %v", err)
	}
	if len(b) != 32 {
		t.Errorf("RandomBytes error: got %d hex characters, want %d", len(b), 32)
	}

	s, err := Sha256("hello")
	if err != nil {
		t.Errorf("Sha256 error: %v", err)
	}
	if s != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Errorf("Sha256 error: got %s, want %s", s, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
	}
}
