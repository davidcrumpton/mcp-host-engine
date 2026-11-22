// Package integrity implements content digests: parsing "algo:hex" checksum
// values, hashing bytes, and verifying files against a checksum or a manifest.
//
// It holds no policy and knows nothing about plugins or config files. Both the
// plugin loader and the config gate need the same definition of "a checksum" --
// what strings are accepted, which algorithms exist, how a mismatch is worded --
// and they live in packages that cannot import one another, so the primitives
// live here.
package integrity

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Checksum algorithms understood in a "checksum:" value and in checksum
// manifests.
const (
	SHA256 = "sha256"
	SHA512 = "sha512"
)

// Checksum is a content digest: an algorithm name plus a lowercase hex digest.
// In YAML and in manifests it is written "sha256:<hex>".
type Checksum struct {
	Algorithm string
	Hex       string
}

// String renders the canonical "algo:hex" form used in config.yaml.
func (c Checksum) String() string {
	return c.Algorithm + ":" + c.Hex
}

// Equal reports whether two checksums name the same algorithm and digest.
func (c Checksum) Equal(other Checksum) bool {
	return c.Algorithm == other.Algorithm && c.Hex == other.Hex
}

// IsZero reports whether c holds no digest at all.
func (c Checksum) IsZero() bool {
	return c.Algorithm == "" && c.Hex == ""
}

// ParseChecksum normalises a raw config value into a Checksum. Accepted forms
// are "sha256:<hex>", "sha512:<hex>", and bare hex whose length selects the
// algorithm (64 chars = sha256, 128 = sha512). Case is ignored.
//
// Anything else -- including a misspelled algorithm -- is an error rather than a
// silent "no checksum configured". A checksum that is quietly ignored is worse
// than no checksum at all, because it looks like protection that isn't there.
func ParseChecksum(raw interface{}) (Checksum, error) {
	s, ok := raw.(string)
	if !ok {
		return Checksum{}, fmt.Errorf("checksum must be a string (e.g. \"sha256:<hex>\"), got %T", raw)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return Checksum{}, fmt.Errorf("checksum is empty")
	}

	algo, digest := "", s
	if i := strings.Index(s, ":"); i >= 0 {
		algo = strings.ToLower(strings.TrimSpace(s[:i]))
		digest = strings.TrimSpace(s[i+1:])
	}
	digest = strings.ToLower(digest)

	switch algo {
	case "":
		switch len(digest) {
		case 64:
			algo = SHA256
		case 128:
			algo = SHA512
		default:
			return Checksum{}, fmt.Errorf("checksum %q: bare digest must be 64 hex chars (sha256) or 128 (sha512), got %d", s, len(digest))
		}
	case SHA256:
		if len(digest) != 64 {
			return Checksum{}, fmt.Errorf("checksum %q: sha256 digest must be 64 hex chars, got %d", s, len(digest))
		}
	case SHA512:
		if len(digest) != 128 {
			return Checksum{}, fmt.Errorf("checksum %q: sha512 digest must be 128 hex chars, got %d", s, len(digest))
		}
	default:
		return Checksum{}, fmt.Errorf("checksum %q: unsupported algorithm %q (want %s or %s)", s, algo, SHA256, SHA512)
	}

	if _, err := hex.DecodeString(digest); err != nil {
		return Checksum{}, fmt.Errorf("checksum %q: not valid hex: %w", s, err)
	}
	return Checksum{Algorithm: algo, Hex: digest}, nil
}

// HashWith digests data with algo, defaulting to sha256 for an unknown name.
// Unknown algorithms cannot reach here from config (ParseChecksum rejects them)
// but defaulting keeps a zero-valued Checksum harmless.
func HashWith(algo string, data []byte) Checksum {
	if algo == SHA512 {
		sum := sha512.Sum512(data)
		return Checksum{Algorithm: SHA512, Hex: hex.EncodeToString(sum[:])}
	}
	sum := sha256.Sum256(data)
	return Checksum{Algorithm: SHA256, Hex: hex.EncodeToString(sum[:])}
}

// HashBytes returns the sha256 of data.
func HashBytes(data []byte) Checksum {
	return HashWith(SHA256, data)
}

// Matches reports whether data digests to c.
func (c Checksum) Matches(data []byte) bool {
	return HashWith(c.Algorithm, data).Equal(c)
}

// VerifyData checks data -- read from path -- against want. The caller reads the
// file once and passes those same bytes on for use, so the digest covers exactly
// what will be acted on, with no window between "verified" and "used".
func VerifyData(path string, data []byte, want Checksum) error {
	if got := HashWith(want.Algorithm, data); !got.Equal(want) {
		return fmt.Errorf("%s: checksum mismatch: expected %s, got %s", path, want, got)
	}
	return nil
}

// VerifyFile reads path and returns a descriptive error when its content does
// not match want.
func VerifyFile(path string, want Checksum) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return VerifyData(path, data, want)
}

// ---------------------------------------------------------------------------
// Manifest
// ---------------------------------------------------------------------------

// Manifest maps file paths to expected checksums. It is parsed from a
// coreutils-compatible file ("<hex>  <path>" per line), which means a manifest
// produced by `mcphe digest -format manifest` is also checkable with
// `sha256sum -c`. Blank lines and lines starting with '#' are ignored.
//
// A manifest is what extends coverage beyond a single entry file: the sandboxed
// require() can pull in other .js files from plugin_dir (plugins/lib/ and
// friends), and those are read at call time, long after the entry file was
// verified. With a manifest configured every file the loader reads is checked
// against it.
type Manifest struct {
	Path    string
	Entries map[string]Checksum
}

// LoadManifest reads and parses a checksum manifest.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading checksum manifest: %w", err)
	}
	m := &Manifest{Path: path, Entries: make(map[string]Checksum)}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// coreutils puts the digest first, then whitespace, then the name --
		// possibly prefixed with '*' for binary mode. Splitting on the first
		// run of whitespace keeps paths containing spaces intact.
		sep := strings.IndexAny(line, " \t")
		if sep < 0 {
			return nil, fmt.Errorf("%s:%d: expected \"<digest>  <path>\", got %q", path, i+1, line)
		}
		sum, err := ParseChecksum(line[:sep])
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
		name := strings.TrimPrefix(strings.TrimLeft(line[sep:], " \t"), "*")
		if name == "" {
			return nil, fmt.Errorf("%s:%d: missing file name", path, i+1)
		}
		m.Entries[ManifestKey(name)] = sum
	}
	if len(m.Entries) == 0 {
		return nil, fmt.Errorf("checksum manifest %s lists no files", path)
	}
	return m, nil
}

// ManifestKey normalises a manifest entry name so lookups match regardless of
// "./" prefixes or Windows separators. Absolute paths are kept absolute.
func ManifestKey(name string) string {
	name = filepath.ToSlash(name)
	if filepath.IsAbs(name) {
		return filepath.ToSlash(filepath.Clean(name))
	}
	return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(name, "./")))
}

// RelKey expresses absPath relative to dir the same way manifest entries are
// stored, so a manifest generated in the plugin directory matches files read
// from it.
func RelKey(dir, absPath string) string {
	abs := filepath.Clean(absPath)
	if rel, err := filepath.Rel(dir, abs); err == nil {
		return ManifestKey(rel)
	}
	return ManifestKey(abs)
}

// Verify checks data read from absPath against the manifest. A file that is not
// listed is an error, not a pass: with a manifest configured, silence must not
// be mistaken for coverage.
func (m *Manifest) Verify(dir, absPath string, data []byte) error {
	if m == nil {
		return nil
	}
	rel := RelKey(dir, absPath)
	want, ok := m.Entries[rel]
	if !ok {
		// Fall back to an absolute-path entry for manifests generated with
		// absolute names.
		if want, ok = m.Entries[ManifestKey(absPath)]; !ok {
			return fmt.Errorf("%s is not listed in checksum manifest %s", rel, m.Path)
		}
	}
	if got := HashWith(want.Algorithm, data); !got.Equal(want) {
		return fmt.Errorf("checksum mismatch: manifest %s expects %s, file is %s", m.Path, want, got)
	}
	return nil
}

// Len reports how many files the manifest covers.
func (m *Manifest) Len() int {
	if m == nil {
		return 0
	}
	return len(m.Entries)
}
