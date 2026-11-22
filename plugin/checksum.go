package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mcphe/config"
	"mcphe/integrity"
)

// Checksum parsing, hashing and the coreutils manifest format live in
// mcphe/integrity, because the config-file gate in main.go needs the same
// definition of "a checksum" and cannot import this package. The names are
// re-exported here so the plugin-integrity code (and its tests) can keep using
// the plugin-local vocabulary.
const (
	ChecksumSHA256 = integrity.SHA256
	ChecksumSHA512 = integrity.SHA512
)

// Checksum is an algorithm name plus a lowercase hex digest, written
// "sha256:<hex>" in config.yaml.
type Checksum = integrity.Checksum

// Manifest is a parsed coreutils-style checksum file, used for
// plugin_checksum_manifest.
type Manifest = integrity.Manifest

// ParseChecksum normalises a raw config value into a Checksum. See
// integrity.ParseChecksum.
func ParseChecksum(raw interface{}) (Checksum, error) { return integrity.ParseChecksum(raw) }

// HashBytes returns the sha256 of data.
func HashBytes(data []byte) Checksum { return integrity.HashBytes(data) }

// VerifyFile reads path and returns a descriptive error when its content does
// not match want.
func VerifyFile(path string, want Checksum) error { return integrity.VerifyFile(path, want) }

// LoadManifest reads and parses a checksum manifest.
func LoadManifest(path string) (*Manifest, error) { return integrity.LoadManifest(path) }

// hashWith digests data with algo, defaulting to sha256.
func hashWith(algo string, data []byte) Checksum { return integrity.HashWith(algo, data) }

// Status values reported by VerifyConfiguredChecksums and printed by
// `mcphe digest -verify`.
const (
	ChecksumStatusOK        = "ok"
	ChecksumStatusMismatch  = "mismatch"
	ChecksumStatusMissing   = "missing"
	ChecksumStatusUnchecked = "unchecked"
)

// ---------------------------------------------------------------------------
// Startup policy
// ---------------------------------------------------------------------------

// checksumPolicy is the plugin-integrity policy derived from config at startup:
// an optional expected checksum per configured entry (with "plugin:" aliases
// inheriting their source's), and an optional whole-directory manifest.
type checksumPolicy struct {
	cfg      config.Config
	dir      string
	declared map[string]Checksum // config entry name -> expected entry-file checksum
	manifest *Manifest
	required bool
}

// newChecksumPolicy validates the integrity-related config and returns the
// policy LoadPlugins enforces. A nil policy is never returned with a nil error.
func newChecksumPolicy(cfg config.Config) (*checksumPolicy, error) {
	dir := cfg.PluginDir
	if dir == "" {
		dir = "plugins"
	}
	p := &checksumPolicy{
		cfg:      cfg,
		dir:      dir,
		declared: make(map[string]Checksum),
		required: cfg.RequirePluginChecksums,
	}

	// Parse every declared checksum up front, including entries that are
	// currently disabled: a malformed checksum is a config bug worth reporting
	// at startup, not one to discover the day the plugin is re-enabled.
	for instanceName := range cfg.Plugins {
		raw, ok := cfg.PluginChecksum(instanceName)
		if !ok {
			continue
		}
		sum, err := ParseChecksum(raw)
		if err != nil {
			return nil, fmt.Errorf("plugin %q: %w", instanceName, err)
		}
		p.declared[instanceName] = sum
	}

	// Two entries backed by the same module file must agree: a checksum
	// describes the file, so a disagreement is a config error rather than a
	// last-one-wins override that would silently weaken one instance.
	for instanceName, sum := range p.declared {
		source := cfg.PluginSource(instanceName)
		if source == instanceName {
			continue
		}
		if srcSum, ok := p.declared[source]; ok && !srcSum.Equal(sum) {
			return nil, fmt.Errorf("plugin %q declares %s but its module %q declares %s: entries sharing a module must declare the same checksum",
				instanceName, sum, source, srcSum)
		}
	}

	if cfg.PluginChecksumManifest != "" {
		m, err := LoadManifest(cfg.PluginChecksumManifest)
		if err != nil {
			return nil, err
		}
		p.manifest = m
	}
	return p, nil
}

// expected returns the checksum configured for instanceName, if any.
func (p *checksumPolicy) expected(instanceName string) (Checksum, bool) {
	if p == nil {
		return Checksum{}, false
	}
	c, ok := p.declared[instanceName]
	return c, ok
}

// requireDeclared fails when checksums are mandatory and instanceName declares
// none. It is called only for plugins that are actually about to be loaded, so a
// disabled entry never trips it.
func (p *checksumPolicy) requireDeclared(instanceName string) error {
	if p == nil || !p.required {
		return nil
	}
	if _, ok := p.expected(instanceName); !ok {
		return fmt.Errorf("plugin %q: no checksum configured, and require_plugin_checksums is true (run `mcphe digest -format yaml`)", instanceName)
	}
	return nil
}

// verifyEntry checks the bytes of a plugin's entry file -- the exact bytes about
// to be handed to the JavaScript runtime -- against the configured checksum and
// the manifest. Checking the same slice that is executed closes the window a
// separate pre-flight read would open between "verified" and "ran".
func (p *checksumPolicy) verifyEntry(instanceName, path string, content []byte) error {
	if p == nil {
		return nil
	}
	if want, ok := p.expected(instanceName); ok {
		if got := hashWith(want.Algorithm, content); !got.Equal(want) {
			return fmt.Errorf("checksum mismatch: config expects %s, file is %s", want, got)
		}
	}
	if p.manifest != nil {
		return p.manifest.Verify(p.dir, path, content)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Verification report (`mcphe digest -verify`)
// ---------------------------------------------------------------------------

// ChecksumReport is the outcome of checking one file against its expected
// checksum.
type ChecksumReport struct {
	Plugin   string // config entry name, or "-" for a manifest-only row
	File     string // path checked
	Status   string // ok | mismatch | missing | unchecked
	Expected string
	Actual   string
}

// Failed reports whether the row represents a failure (as opposed to a pass or
// an entry that simply declares no checksum).
func (r ChecksumReport) Failed() bool {
	return r.Status == ChecksumStatusMismatch || r.Status == ChecksumStatusMissing
}

// VerifyConfiguredChecksums checks every enabled plugin's entry file against the
// checksum declared in config, plus every file covered by the manifest when one
// is configured. It does not execute plugin code, so it is safe to run as a CI
// or pre-deploy gate; enforcement inside LoadPlugins remains authoritative.
func VerifyConfiguredChecksums(cfg config.Config) ([]ChecksumReport, error) {
	policy, err := newChecksumPolicy(cfg)
	if err != nil {
		return nil, err
	}
	dir := policy.dir

	files, err := pluginFilesIn(dir)
	if err != nil {
		return nil, fmt.Errorf("scanning plugin directory %q: %w", dir, err)
	}
	// Index exported names so a config entry whose file name differs from its
	// plugin name can still be located.
	byName := make(map[string]string, len(files))
	for _, f := range files {
		if n := DetectPluginName(f); n != "" {
			if _, dup := byName[n]; !dup {
				byName[n] = f
			}
		}
	}

	instances := make([]string, 0, len(cfg.Plugins))
	for name := range cfg.Plugins {
		instances = append(instances, name)
	}
	sort.Strings(instances)

	reports := make([]ChecksumReport, 0, len(instances))
	for _, instance := range instances {
		if !cfg.IsToolEnabled(instance) {
			continue
		}
		source := cfg.PluginSource(instance)
		path := filepath.Join(dir, source+".js")
		if _, statErr := os.Stat(path); statErr != nil {
			path = byName[source]
		}
		rep := ChecksumReport{Plugin: instance, File: path}

		want, declared := policy.expected(instance)
		switch {
		case !declared && policy.required:
			rep.Status = ChecksumStatusMissing
			reports = append(reports, rep)
			continue
		case !declared:
			rep.Status = ChecksumStatusUnchecked
			reports = append(reports, rep)
			continue
		}
		rep.Expected = want.String()

		if path == "" {
			rep.Status = ChecksumStatusMissing
			reports = append(reports, rep)
			continue
		}
		data, readErr := readPluginFile(path)
		if readErr != nil {
			rep.Status = ChecksumStatusMissing
			reports = append(reports, rep)
			continue
		}
		got := hashWith(want.Algorithm, data)
		rep.Actual = got.String()
		if got.Equal(want) {
			rep.Status = ChecksumStatusOK
		} else {
			rep.Status = ChecksumStatusMismatch
		}
		reports = append(reports, rep)
	}

	if policy.manifest != nil {
		keys := make([]string, 0, len(policy.manifest.Entries))
		for k := range policy.manifest.Entries {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, rel := range keys {
			want := policy.manifest.Entries[rel]
			rep := ChecksumReport{Plugin: "-", File: rel, Expected: want.String()}
			path := rel
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, filepath.FromSlash(rel))
			}
			data, readErr := readPluginFile(path)
			if readErr != nil {
				rep.Status = ChecksumStatusMissing
				reports = append(reports, rep)
				continue
			}
			got := hashWith(want.Algorithm, data)
			rep.Actual = got.String()
			if got.Equal(want) {
				rep.Status = ChecksumStatusOK
			} else {
				rep.Status = ChecksumStatusMismatch
			}
			reports = append(reports, rep)
		}
	}

	return reports, nil
}

// ---------------------------------------------------------------------------
// Digest generation (`mcphe digest`)
// ---------------------------------------------------------------------------

// DigestEntry is one row of `mcphe digest` output.
type DigestEntry struct {
	Path     string `json:"path"`           // relative to the scanned directory, slash-separated
	Name     string `json:"name,omitempty"` // exported plugin name, when detectable
	Checksum string `json:"checksum"`       // "algo:hex"
}

// DigestDir walks dir recursively and digests every *.js file. Recursive on
// purpose: the loader only globs the top level, but require() can reach
// subdirectories such as plugins/lib/, and a manifest that skipped those would
// give false confidence.
func DigestDir(dir, algo string) ([]DigestEntry, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("plugin directory %q: %w", dir, err)
	}
	entries := make([]DigestEntry, 0, 32)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".js") {
			return nil
		}
		data, readErr := readPluginFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}
		entries = append(entries, DigestEntry{
			Path:     filepath.ToSlash(rel),
			Name:     DetectPluginName(path),
			Checksum: hashWith(algo, data).String(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning %q: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// DigestPaths digests the named files. Paths are reported as given, relative to
// dir when they live under it.
func DigestPaths(dir, algo string, paths []string) ([]DigestEntry, error) {
	entries := make([]DigestEntry, 0, len(paths))
	for _, path := range paths {
		data, err := readPluginFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		rel := path
		if dir != "" {
			if r, relErr := filepath.Rel(dir, path); relErr == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
		entries = append(entries, DigestEntry{
			Path:     filepath.ToSlash(rel),
			Name:     DetectPluginName(path),
			Checksum: hashWith(algo, data).String(),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// FormatDigestText renders "algo:hex  path" lines -- human-readable, and
// directly usable as a plugin's `checksum:` value.
func FormatDigestText(entries []DigestEntry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s  %s\n", e.Checksum, e.Path)
	}
	return b.String()
}

// FormatDigestManifest renders coreutils-style "<hex>  <path>" lines, the input
// format of `sha256sum -c` and of plugin_checksum_manifest.
func FormatDigestManifest(entries []DigestEntry) string {
	var b strings.Builder
	for _, e := range entries {
		hexOnly := e.Checksum
		if i := strings.Index(hexOnly, ":"); i >= 0 {
			hexOnly = hexOnly[i+1:]
		}
		fmt.Fprintf(&b, "%s  %s\n", hexOnly, e.Path)
	}
	return b.String()
}

// FormatDigestJSON renders the entries as a JSON object keyed by path.
func FormatDigestJSON(entries []DigestEntry) (string, error) {
	obj := make(map[string]DigestEntry, len(entries))
	for _, e := range entries {
		obj[e.Path] = e
	}
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// FormatDigestYAML renders a paste-ready `plugins:` block. Keys are exported
// plugin names -- the names config.yaml uses -- not file names, and each entry
// carries the file it came from as a comment so a mismatch is obvious.
func FormatDigestYAML(entries []DigestEntry) string {
	var b strings.Builder
	b.WriteString("plugins:\n")
	used := make(map[string]int, len(entries))
	for _, e := range entries {
		key := e.Name
		if key == "" {
			// Fall back to the file stem; the comment says so, so nobody has to
			// guess why the key does not look like a plugin name.
			key = strings.TrimSuffix(filepath.Base(e.Path), ".js")
		}
		used[key]++
		if n := used[key]; n > 1 {
			key = fmt.Sprintf("%s_%d", key, n)
		}
		fmt.Fprintf(&b, "  %s:\n", key)
		fmt.Fprintf(&b, "    # %s\n", e.Path)
		fmt.Fprintf(&b, "    checksum: \"%s\"\n", e.Checksum)
	}
	return b.String()
}
