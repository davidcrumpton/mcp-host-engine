package integrity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sha256Empty is the well-known sha256 of the empty byte string; pinning the
// hashing path to a value that cannot drift with a refactor.
const sha256Empty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func TestHashBytes(t *testing.T) {
	if got := HashBytes(nil).String(); got != "sha256:"+sha256Empty {
		t.Errorf("HashBytes(nil) = %s", got)
	}
	if got := HashBytes([]byte("abc")); got.Algorithm != SHA256 || len(got.Hex) != 64 {
		t.Errorf("HashBytes(abc) = %+v", got)
	}
}

func TestHashWithDefaultsToSHA256(t *testing.T) {
	// A zero-valued Checksum must not produce an empty digest: HashWith falls
	// back to sha256 for an algorithm name it does not know.
	if got := HashWith("", nil); got.String() != "sha256:"+sha256Empty {
		t.Errorf("HashWith(\"\", nil) = %s", got)
	}
	if got := HashWith(SHA512, nil); got.Algorithm != SHA512 || len(got.Hex) != 128 {
		t.Errorf("HashWith(sha512, nil) = %+v", got)
	}
}

func TestChecksumIsZero(t *testing.T) {
	if !(Checksum{}).IsZero() {
		t.Error("zero Checksum should report IsZero")
	}
	if (Checksum{Algorithm: SHA256, Hex: sha256Empty}).IsZero() {
		t.Error("a real checksum should not report IsZero")
	}
}

// VerifyData is the entry point the config gate uses: it must check the bytes it
// is handed, so the caller can hash and use the same slice.
func TestVerifyData(t *testing.T) {
	data := []byte("port: \"8001\"\n")
	good := HashBytes(data)

	if err := VerifyData("config.yaml", data, good); err != nil {
		t.Fatalf("VerifyData on matching bytes: %v", err)
	}

	// One byte different is a mismatch, and the message has to carry both
	// digests plus the path.
	err := VerifyData("config.yaml", []byte("port: \"8002\"\n"), good)
	if err == nil {
		t.Fatal("expected a mismatch")
	}
	for _, want := range []string{"config.yaml", "checksum mismatch", good.String(), "got sha256:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}

	// The algorithm comes from the expected checksum, so a sha512 pin works.
	sha512sum := HashWith(SHA512, data)
	if err := VerifyData("config.yaml", data, sha512sum); err != nil {
		t.Errorf("sha512 verify: %v", err)
	}
	if err := VerifyData("config.yaml", data, HashWith(SHA512, []byte("other"))); err == nil {
		t.Error("expected a sha512 mismatch")
	}
}

func TestVerifyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("transport: http\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	if err := VerifyFile(path, HashBytes(data)); err != nil {
		t.Fatalf("VerifyFile: %v", err)
	}
	if err := VerifyFile(path, HashBytes([]byte("different"))); err == nil {
		t.Error("expected a mismatch")
	}
	if err := VerifyFile(filepath.Join(dir, "nope"), HashBytes(data)); err == nil {
		t.Error("expected an error for a missing file")
	}
}
