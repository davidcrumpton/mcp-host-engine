// host/url/url.go
//
// Exposes a Web-compatible URL and URLSearchParams API to JavaScript plugins
// running inside goja.  The surface follows:
//   https://developer.mozilla.org/en-US/docs/Web/API/URL/URL
//   https://developer.mozilla.org/en-US/docs/Web/API/URLSearchParams
//
// Usage inside a plugin (namespace API):
//
//   const u = host.url.parse("https://example.com/path?a=1&b=2#frag");
//   u.pathname          // "/path"
//   u.searchParams.get("a")   // "1"
//   u.searchParams.getAll("b") // ["2"]
//
//   const sp = host.url.searchParams("key=val&key=val2");
//   sp.get("key")       // "val"
//   sp.getAll("key")    // ["val", "val2"]
//
//   host.url.resolve("https://example.com/a/b", "../c")  // "https://example.com/a/c"
//
// None of these functions make network calls; they are pure string/parse
// operations and therefore need no allowed_domains configuration.

package url

import (
	"fmt"
	"net/url"
	"strings"
)

// ---------------------------------------------------------------------------
// URLSearchParams
// ---------------------------------------------------------------------------

// SearchParams is the Go-side representation of URLSearchParams.
// It is returned to goja as a plain map[string]interface{} whose values are
// callable closures, mirroring the JS URLSearchParams method set.
type SearchParams struct {
	values url.Values
}

// newSearchParams parses a query string (with or without a leading "?").
func newSearchParams(raw string) *SearchParams {
	raw = strings.TrimPrefix(raw, "?")
	v, _ := url.ParseQuery(raw)
	if v == nil {
		v = url.Values{}
	}
	return &SearchParams{values: v}
}

// toMap converts a SearchParams into the map that goja plugins will receive.
// Every method is a Go closure so goja sees it as a callable JS function.
func (sp *SearchParams) toMap() map[string]interface{} {
	return map[string]interface{}{
		// get(name) → string | null ("" in Go when absent)
		"get": func(name string) interface{} {
			vals := sp.values[name]
			if len(vals) == 0 {
				return nil
			}
			return vals[0]
		},
		// getAll(name) → []string (empty slice when absent)
		"getAll": func(name string) []string {
			vals := sp.values[name]
			if vals == nil {
				return []string{}
			}
			return vals
		},
		// has(name) → bool
		"has": func(name string) bool {
			_, ok := sp.values[name]
			return ok
		},
		// keys() → []string (unique, in insertion order per spec; map order here)
		"keys": func() []string {
			keys := make([]string, 0, len(sp.values))
			for k := range sp.values {
				keys = append(keys, k)
			}
			return keys
		},
		// entries() → [][]string  (each element is [key, value])
		"entries": func() [][]string {
			out := make([][]string, 0)
			for k, vs := range sp.values {
				for _, v := range vs {
					out = append(out, []string{k, v})
				}
			}
			return out
		},
		// set(name, value) — replaces all values for name
		"set": func(name, value string) {
			sp.values.Set(name, value)
		},
		// append(name, value) — adds an additional value
		"append": func(name, value string) {
			sp.values.Add(name, value)
		},
		// delete(name) — removes all values for name
		"delete": func(name string) {
			sp.values.Del(name)
		},
		// toString() → "key=val&key2=val2"  (URL-encoded, no leading "?")
		"toString": func() string {
			return sp.values.Encode()
		},
		// size → number of distinct keys (not total entries — matches Chrome behaviour)
		"size": len(sp.values),
	}
}

// ---------------------------------------------------------------------------
// URL
// ---------------------------------------------------------------------------

// ParsedURL is the Go-side representation of a parsed URL, returned to goja
// as a map of string fields plus a nested searchParams map.
func parsedURLMap(u *url.URL) map[string]interface{} {
	sp := newSearchParams(u.RawQuery)

	// origin: scheme + "://" + host  (omitted for non-http(s)/ftp schemes)
	origin := ""
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ftp":
		origin = u.Scheme + "://" + u.Host
	}

	// username / password from Userinfo
	username := ""
	password := ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}

	return map[string]interface{}{
		"href":     u.String(),
		"origin":   origin,
		"protocol": u.Scheme + ":",
		"username": username,
		"password": password,
		"host":     u.Host, // hostname:port
		"hostname": u.Hostname(),
		"port":     u.Port(),
		"pathname": func() string {
			if u.Path == "" {
				return "/"
			}
			return u.Path
		}(),
		"search": func() string {
			if u.RawQuery == "" {
				return ""
			}
			return "?" + u.RawQuery
		}(),
		"hash": func() string {
			if u.Fragment == "" {
				return ""
			}
			return "#" + u.Fragment
		}(),
		"searchParams": sp.toMap(),
		// toString() mirrors href
		"toString": func() string { return u.String() },
	}
}

// ---------------------------------------------------------------------------
// Package-level host namespace
// ---------------------------------------------------------------------------

// MakeURLNamespace returns the map exposed as host.url.* inside plugins.
//
// Functions:
//
//	host.url.parse(href [, base])
//	  Parses href (resolved against optional base) and returns a URL object.
//	  Throws (returns error) when href is invalid.
//
//	host.url.canParse(href [, base])
//	  Returns true when href (optionally relative to base) is a valid URL.
//
//	host.url.resolve(base, relative)
//	  Resolves relative against base and returns the resulting href string.
//
//	host.url.searchParams(init)
//	  Parses a query string (with or without "?") and returns a URLSearchParams object.
//
//	host.url.encode(str)
//	  Equivalent to encodeURIComponent.
//
//	host.url.decode(str)
//	  Equivalent to decodeURIComponent.
func MakeURLNamespace() map[string]interface{} {
	return map[string]interface{}{
		// parse(href [, base]) → URLObject | error
		"parse": func(args ...string) (map[string]interface{}, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("url.parse requires at least one argument")
			}
			href := args[0]
			if len(args) >= 2 && args[1] != "" {
				base, err := url.Parse(args[1])
				if err != nil {
					return nil, fmt.Errorf("url.parse: invalid base %q: %w", args[1], err)
				}
				ref, err := url.Parse(href)
				if err != nil {
					return nil, fmt.Errorf("url.parse: invalid href %q: %w", href, err)
				}
				return parsedURLMap(base.ResolveReference(ref)), nil
			}
			// Always use url.Parse — ParseRequestURI stuffs the fragment into
			// RawQuery (e.g. "a=1&b=2#section") instead of separating it into
			// the Fragment field, which breaks the search and hash properties.
			u, err := url.Parse(href)
			if err != nil {
				return nil, fmt.Errorf("url.parse: invalid URL %q: %w", href, err)
			}
			if u.Scheme == "" || u.Host == "" {
				return nil, fmt.Errorf("url.parse: %q is not an absolute URL", href)
			}
			return parsedURLMap(u), nil
		},

		// canParse(href [, base]) → bool
		"canParse": func(args ...string) bool {
			if len(args) == 0 {
				return false
			}
			href := args[0]
			if len(args) >= 2 && args[1] != "" {
				base, err := url.Parse(args[1])
				if err != nil {
					return false
				}
				ref, err := url.Parse(href)
				if err != nil {
					return false
				}
				resolved := base.ResolveReference(ref)
				return resolved.Scheme != "" && resolved.Host != ""
			}
			u, err := url.Parse(href)
			if err != nil {
				return false
			}
			return u.Scheme != "" && u.Host != ""
		},

		// resolve(base, relative) → string | error
		"resolve": func(base, relative string) (string, error) {
			b, err := url.Parse(base)
			if err != nil {
				return "", fmt.Errorf("url.resolve: invalid base %q: %w", base, err)
			}
			r, err := url.Parse(relative)
			if err != nil {
				return "", fmt.Errorf("url.resolve: invalid relative %q: %w", relative, err)
			}
			return b.ResolveReference(r).String(), nil
		},

		// searchParams(init) → URLSearchParamsObject
		"searchParams": func(init string) map[string]interface{} {
			return newSearchParams(init).toMap()
		},

		// encode(str) → percent-encoded string  (encodeURIComponent equivalent)
		"encode": func(s string) string {
			return url.QueryEscape(s)
		},

		// decode(str) → decoded string  (decodeURIComponent equivalent)
		"decode": func(s string) (string, error) {
			decoded, err := url.QueryUnescape(s)
			if err != nil {
				return "", fmt.Errorf("url.decode: %w", err)
			}
			return decoded, nil
		},
	}
}
