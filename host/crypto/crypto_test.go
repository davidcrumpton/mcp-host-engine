package crypto

import (
	"testing"
)

func TestRandomBytes(t *testing.T) {
	b, err := RandomBytes(16)
	if err != nil {
		t.Errorf("RandomBytes error: %v", err)
	}
	if len(b) != 16 {
		t.Errorf("RandomBytes error: got %d bytes, want %d", len(b), 16)
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
	if len(b) != 16 {
		t.Errorf("RandomBytes error: got %d bytes, want %d", len(b), 16)
	}

	s, err := Sha256("hello")
	if err != nil {
		t.Errorf("Sha256 error: %v", err)
	}
	if s != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Errorf("Sha256 error: got %s, want %s", s, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
	}
}
