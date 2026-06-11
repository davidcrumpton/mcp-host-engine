package path

import "testing"

func TestBasename(t *testing.T) {
	bn := Basename("/path/to/foo.txt")
	if bn != "foo.txt" {
		t.Errorf("basename error: got %s, want %s", bn, "foo.txt")
	}
}

func TestJoin(t *testing.T) {
	j := Join("/path", "to", "foo.txt")
	if j != "/path/to/foo.txt" {
		t.Errorf("join error: got %s, want %s", j, "/path/to/foo.txt")
	}
}

func TestResolve(t *testing.T) {
	r := Resolve("/path", "to", "foo.txt")
	if r != "/path/to/foo.txt" {
		t.Errorf("resolve error: got %s, want %s", r, "/path/to/foo.txt")
	}
}

func TestNormalize(t *testing.T) {
	n := Normalize("/path/to/foo.txt")
	if n != "/path/to/foo.txt" {
		t.Errorf("normalize error: got %s, want %s", n, "/path/to/foo.txt")
	}
}

func TestDirname(t *testing.T) {
	d := Dirname("/path/to/foo.txt")
	if d != "/path/to" {
		t.Errorf("dirname error: got %s, want %s", d, "/path/to")
	}
}

func TestExtname(t *testing.T) {
	e := Extname("/path/to/foo.txt")
	if e != ".txt" {
		t.Errorf("extname error: got %s, want %s", e, ".txt")
	}
}
