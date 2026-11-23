package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The wikipedia_search plugin validates `language` against a fixed map
// (LANG_NAMES in plugin.ts) and then requests "<lang>.wikipedia.org". The host
// enforces allowed_domains. If those two lists drift, the plugin accepts a
// language and the host then blocks the request -- which is exactly the bug
// this guards: allowed_domains listed only en.wikipedia.org while the plugin
// advertised 20 editions, so 19 of them failed with
// "access to <lang>.wikipedia.org is not allowed".
//
// Rather than duplicate the language list here (a third copy to drift), read
// the codes straight out of the plugin source.

var langNamesBlock = regexp.MustCompile(`(?s)const LANG_NAMES = \{(.*?)\n\};`)
var langKey = regexp.MustCompile(`(\w+):\s*"`)

// pluginLangCodes returns the language codes declared in LANG_NAMES.
func pluginLangCodes(t *testing.T) []string {
	t.Helper()

	path := filepath.Join("..", "plugin-devel", "plugins", "wikipedia_search", "plugin.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read plugin source at %s: %v", path, err)
	}

	m := langNamesBlock.FindSubmatch(src)
	if m == nil {
		t.Fatalf("could not find `const LANG_NAMES = {...};` in %s -- update this test if the plugin was restructured", path)
	}

	var codes []string
	for _, km := range langKey.FindAllSubmatch(m[1], -1) {
		codes = append(codes, string(km[1]))
	}
	if len(codes) == 0 {
		t.Fatalf("found LANG_NAMES in %s but parsed no language codes", path)
	}
	sort.Strings(codes)
	return codes
}

func wikipediaAllowedDomains(t *testing.T, cfg Config) []string {
	t.Helper()
	domains := cfg.AllowedDomainsFor("wikipedia_search")
	if len(domains) == 0 {
		t.Fatal("wikipedia_search has no allowed_domains; every request would be blocked")
	}
	return domains
}

// The built-in defaults must cover every language the plugin accepts.
func TestWikipediaAllowedDomainsCoverPluginLanguages(t *testing.T) {
	cfg := DefaultConfig
	domains := wikipediaAllowedDomains(t, cfg)

	have := map[string]bool{}
	for _, d := range domains {
		have[d] = true
	}

	for _, code := range pluginLangCodes(t) {
		want := code + ".wikipedia.org"
		if !have[want] {
			t.Errorf("plugin accepts language %q but allowed_domains lacks %q -- "+
				"that edition would fail with 'access to %s is not allowed'", code, want, want)
		}
	}
}

// The shipped config.yaml must cover every language the plugin accepts.
//
// Parsed directly rather than via LoadConfig, because config.yaml resolves
// secrets (file://, vault://) that do not exist in a unit-test environment.
func TestConfigYAMLWikipediaAllowedDomainsCoverPluginLanguages(t *testing.T) {
	path := filepath.Join("..", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read shipped config.yaml: %v", err)
	}

	var parsed struct {
		// Permissive: other plugins use the "env://VAR" string form for
		// allowed_domains, which would fail a strict []string decode.
		Plugins map[string]map[string]interface{} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}

	pcfg, ok := parsed.Plugins["wikipedia_search"]
	if !ok {
		t.Fatalf("%s has no wikipedia_search block", path)
	}

	// Accept either the YAML list form or a bare string, so this test keeps
	// working if the entry is moved to env:// later.
	var domains []string
	switch v := pcfg["allowed_domains"].(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				domains = append(domains, s)
			}
		}
	case []string:
		domains = v
	case string:
		t.Fatalf("%s: wikipedia_search allowed_domains is a %q reference; this test needs to resolve it to check coverage", path, v)
	}
	if len(domains) == 0 {
		t.Fatalf("%s: wikipedia_search has no allowed_domains", path)
	}

	have := map[string]bool{}
	for _, d := range domains {
		have[d] = true
	}

	for _, code := range pluginLangCodes(t) {
		want := code + ".wikipedia.org"
		if !have[want] {
			t.Errorf("config.yaml: plugin accepts %q but allowed_domains lacks %q", code, want)
		}
	}
}

// No wildcard entries: the allow-list must stay exact so it is auditable.
func TestWikipediaAllowedDomainsAreExplicitHosts(t *testing.T) {
	cfg := DefaultConfig

	for _, d := range wikipediaAllowedDomains(t, cfg) {
		if strings.Contains(d, "*") {
			t.Errorf("allowed_domains entry %q uses a wildcard; list each host explicitly", d)
		}
		if !strings.HasSuffix(d, ".wikipedia.org") {
			t.Errorf("allowed_domains entry %q is not a wikipedia.org host", d)
		}
		if strings.Contains(d, "/") {
			t.Errorf("allowed_domains entry %q should be a bare hostname, not a URL", d)
		}
	}
}
