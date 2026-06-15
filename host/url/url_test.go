package url

import (
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustParse(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	ns := MakeURLNamespace()
	parseFn := ns["parse"].(func(...string) (map[string]interface{}, error))
	u, err := parseFn(args...)
	if err != nil {
		t.Fatalf("url.parse(%v): unexpected error: %v", args, err)
	}
	return u
}

func str(t *testing.T, m map[string]interface{}, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("key %q missing from URL map", key)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("key %q is not a string: %T", key, v)
	}
	return s
}

func spGet(t *testing.T, sp map[string]interface{}, name string) interface{} {
	t.Helper()
	getFn, ok := sp["get"].(func(string) interface{})
	if !ok {
		t.Fatalf("searchParams.get is not a function")
	}
	return getFn(name)
}

func spGetAll(t *testing.T, sp map[string]interface{}, name string) []string {
	t.Helper()
	getFn, ok := sp["getAll"].(func(string) []string)
	if !ok {
		t.Fatalf("searchParams.getAll is not a function")
	}
	return getFn(name)
}

// ---------------------------------------------------------------------------
// MakeURLNamespace structure
// ---------------------------------------------------------------------------

func TestMakeURLNamespace_Keys(t *testing.T) {
	ns := MakeURLNamespace()
	for _, k := range []string{"parse", "canParse", "resolve", "searchParams", "encode", "decode"} {
		if _, ok := ns[k]; !ok {
			t.Errorf("namespace missing key %q", k)
		}
	}
}

// ---------------------------------------------------------------------------
// url.parse — happy paths
// ---------------------------------------------------------------------------

func TestParse_FullURL(t *testing.T) {
	u := mustParse(t, "https://user:pass@example.com:8080/path/to?a=1&b=2#section")

	if got := str(t, u, "protocol"); got != "https:" {
		t.Errorf("protocol: got %q, want %q", got, "https:")
	}
	if got := str(t, u, "username"); got != "user" {
		t.Errorf("username: got %q", got)
	}
	if got := str(t, u, "password"); got != "pass" {
		t.Errorf("password: got %q", got)
	}
	if got := str(t, u, "hostname"); got != "example.com" {
		t.Errorf("hostname: got %q", got)
	}
	if got := str(t, u, "port"); got != "8080" {
		t.Errorf("port: got %q", got)
	}
	if got := str(t, u, "host"); got != "example.com:8080" {
		t.Errorf("host: got %q", got)
	}
	if got := str(t, u, "pathname"); got != "/path/to" {
		t.Errorf("pathname: got %q", got)
	}
	if got := str(t, u, "search"); got != "?a=1&b=2" {
		t.Errorf("search: got %q", got)
	}
	if got := str(t, u, "hash"); got != "#section" {
		t.Errorf("hash: got %q", got)
	}
	if got := str(t, u, "origin"); got != "https://example.com:8080" {
		t.Errorf("origin: got %q", got)
	}
}

func TestParse_SimpleHTTP(t *testing.T) {
	u := mustParse(t, "http://example.com/")
	if got := str(t, u, "protocol"); got != "http:" {
		t.Errorf("protocol: got %q", got)
	}
	if got := str(t, u, "pathname"); got != "/" {
		t.Errorf("pathname: got %q", got)
	}
	if got := str(t, u, "search"); got != "" {
		t.Errorf("search: got %q", got)
	}
	if got := str(t, u, "hash"); got != "" {
		t.Errorf("hash: got %q", got)
	}
}

func TestParse_NoPath_PathnameIsSlash(t *testing.T) {
	u := mustParse(t, "https://example.com")
	if got := str(t, u, "pathname"); got != "/" {
		t.Errorf("pathname: got %q, want /", got)
	}
}

func TestParse_WithBase(t *testing.T) {
	u := mustParse(t, "../other", "https://example.com/a/b/c")
	if got := str(t, u, "href"); got != "https://example.com/a/other" {
		t.Errorf("href: got %q", got)
	}
}

func TestParse_RelativeWithBase_AbsolutePath(t *testing.T) {
	u := mustParse(t, "/new/path", "https://example.com/old/path")
	if got := str(t, u, "pathname"); got != "/new/path" {
		t.Errorf("pathname: got %q", got)
	}
}

func TestParse_ToString(t *testing.T) {
	raw := "https://example.com/path?q=1"
	u := mustParse(t, raw)
	toStringFn := u["toString"].(func() string)
	if got := toStringFn(); got != raw {
		t.Errorf("toString: got %q, want %q", got, raw)
	}
}

// ---------------------------------------------------------------------------
// url.parse — error paths
// ---------------------------------------------------------------------------

func TestParse_InvalidURL(t *testing.T) {
	ns := MakeURLNamespace()
	parseFn := ns["parse"].(func(...string) (map[string]interface{}, error))
	_, err := parseFn("not a url at all ://???")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestParse_NoArgs(t *testing.T) {
	ns := MakeURLNamespace()
	parseFn := ns["parse"].(func(...string) (map[string]interface{}, error))
	_, err := parseFn()
	if err == nil {
		t.Error("expected error when called with no arguments")
	}
}

func TestParse_InvalidBase(t *testing.T) {
	ns := MakeURLNamespace()
	parseFn := ns["parse"].(func(...string) (map[string]interface{}, error))
	_, err := parseFn("/path", "://bad-base")
	if err == nil {
		t.Error("expected error for invalid base URL")
	}
}

// ---------------------------------------------------------------------------
// url.canParse
// ---------------------------------------------------------------------------

func TestCanParse_ValidURL(t *testing.T) {
	ns := MakeURLNamespace()
	fn := ns["canParse"].(func(...string) bool)
	if !fn("https://example.com/") {
		t.Error("expected canParse to return true for valid URL")
	}
}

func TestCanParse_InvalidURL(t *testing.T) {
	ns := MakeURLNamespace()
	fn := ns["canParse"].(func(...string) bool)
	if fn("not a url ://???") {
		t.Error("expected canParse to return false for invalid URL")
	}
}

func TestCanParse_RelativeWithBase(t *testing.T) {
	ns := MakeURLNamespace()
	fn := ns["canParse"].(func(...string) bool)
	if !fn("../other", "https://example.com/a/b/") {
		t.Error("expected canParse to return true for resolvable relative URL")
	}
}

func TestCanParse_NoArgs(t *testing.T) {
	ns := MakeURLNamespace()
	fn := ns["canParse"].(func(...string) bool)
	if fn() {
		t.Error("expected canParse to return false with no args")
	}
}

// ---------------------------------------------------------------------------
// url.resolve
// ---------------------------------------------------------------------------

func TestResolve_RelativePath(t *testing.T) {
	ns := MakeURLNamespace()
	fn := ns["resolve"].(func(string, string) (string, error))
	got, err := fn("https://example.com/a/b/", "../c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/a/c" {
		t.Errorf("got %q", got)
	}
}

func TestResolve_AbsoluteRef(t *testing.T) {
	ns := MakeURLNamespace()
	fn := ns["resolve"].(func(string, string) (string, error))
	got, err := fn("https://example.com/a/b", "https://other.com/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://other.com/x" {
		t.Errorf("got %q", got)
	}
}

func TestResolve_InvalidBase(t *testing.T) {
	ns := MakeURLNamespace()
	fn := ns["resolve"].(func(string, string) (string, error))
	_, err := fn("://bad", "/path")
	if err == nil {
		t.Error("expected error for invalid base")
	}
}

// ---------------------------------------------------------------------------
// url.searchParams
// ---------------------------------------------------------------------------

func TestSearchParams_Get(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("key=val&other=2")
	if got := spGet(t, sp, "key"); got != "val" {
		t.Errorf("get: got %v", got)
	}
}

func TestSearchParams_GetMissing(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("key=val")
	if got := spGet(t, sp, "missing"); got != nil {
		t.Errorf("expected nil for missing key, got %v", got)
	}
}

func TestSearchParams_GetAll(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("k=1&k=2&k=3")
	vals := spGetAll(t, sp, "k")
	if len(vals) != 3 {
		t.Errorf("getAll: got %v", vals)
	}
}

func TestSearchParams_Has(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("present=1")
	hasFn := sp["has"].(func(string) bool)
	if !hasFn("present") {
		t.Error("has: expected true for present key")
	}
	if hasFn("absent") {
		t.Error("has: expected false for absent key")
	}
}

func TestSearchParams_SetAndGet(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("")
	setFn := sp["set"].(func(string, string))
	setFn("foo", "bar")
	if got := spGet(t, sp, "foo"); got != "bar" {
		t.Errorf("after set: got %v", got)
	}
}

func TestSearchParams_Append(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("k=1")
	appendFn := sp["append"].(func(string, string))
	appendFn("k", "2")
	vals := spGetAll(t, sp, "k")
	if len(vals) != 2 {
		t.Errorf("append: expected 2 values, got %v", vals)
	}
}

func TestSearchParams_Delete(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("k=1&other=2")
	deleteFn := sp["delete"].(func(string))
	deleteFn("k")
	if got := spGet(t, sp, "k"); got != nil {
		t.Errorf("after delete: expected nil, got %v", got)
	}
}

func TestSearchParams_ToString(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("a=1&b=2")
	toStringFn := sp["toString"].(func() string)
	got := toStringFn()
	// net/url encodes in sorted key order
	if got == "" {
		t.Error("toString returned empty string")
	}
}

func TestSearchParams_LeadingQuestionMark(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("?a=1")
	if got := spGet(t, sp, "a"); got != "1" {
		t.Errorf("expected '1', got %v", got)
	}
}

func TestSearchParams_Keys(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("x=1&y=2")
	keysFn := sp["keys"].(func() []string)
	keys := keysFn()
	if len(keys) != 2 {
		t.Errorf("keys: expected 2, got %v", keys)
	}
}

func TestSearchParams_Entries(t *testing.T) {
	ns := MakeURLNamespace()
	spFn := ns["searchParams"].(func(string) map[string]interface{})
	sp := spFn("a=1&a=2&b=3")
	entriesFn := sp["entries"].(func() [][]string)
	entries := entriesFn()
	if len(entries) != 3 {
		t.Errorf("entries: expected 3, got %v", entries)
	}
}

func TestSearchParams_NestedInParsedURL(t *testing.T) {
	u := mustParse(t, "https://example.com/?foo=bar&foo=baz")
	sp, ok := u["searchParams"].(map[string]interface{})
	if !ok {
		t.Fatal("searchParams is not a map")
	}
	vals := spGetAll(t, sp, "foo")
	if len(vals) != 2 {
		t.Errorf("expected 2 values for 'foo', got %v", vals)
	}
}

// ---------------------------------------------------------------------------
// url.encode / url.decode
// ---------------------------------------------------------------------------

func TestEncode(t *testing.T) {
	ns := MakeURLNamespace()
	fn := ns["encode"].(func(string) string)
	got := fn("hello world & more")
	if got == "hello world & more" {
		t.Error("encode did not encode the string")
	}
	if got == "" {
		t.Error("encode returned empty string")
	}
}

func TestDecode_RoundTrip(t *testing.T) {
	ns := MakeURLNamespace()
	encodeFn := ns["encode"].(func(string) string)
	decodeFn := ns["decode"].(func(string) (string, error))
	original := "hello world & more=stuff"
	encoded := encodeFn(original)
	decoded, err := decodeFn(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded != original {
		t.Errorf("round-trip: got %q, want %q", decoded, original)
	}
}

func TestDecode_InvalidSequence(t *testing.T) {
	ns := MakeURLNamespace()
	fn := ns["decode"].(func(string) (string, error))
	_, err := fn("%ZZ")
	if err == nil {
		t.Error("expected error for invalid percent-encoded sequence")
	}
}
