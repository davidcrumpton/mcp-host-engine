// configguard.go
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"mcphe/config"
	"mcphe/integrity"
)

// configGuard is the command-line half of the config-integrity gate: the digest
// the operator says the config file should have, and whether they insist on
// having checked one at all.
//
// The expected digest never comes from the file being checked. A config that
// states its own checksum proves nothing -- whoever edits the config edits the
// expected value too -- so the value is supplied from outside: the command line,
// or an env:// / file:// reference it resolves.
type configGuard struct {
	expected string // -config-checksum: "algo:hex", "env://VAR", "file://path", or ""
	required bool   // -require-config-checksum
}

// register adds the gate's flags to fs. The serve path, `token` and `digest` all
// take them, so every command that reads a config honors the same policy.
func (g *configGuard) register(fs *flag.FlagSet) {
	fs.StringVar(&g.expected, "config-checksum", "",
		`expected checksum of the config file: "sha256:<hex>", or "env://VAR" / "file://path" to read it from elsewhere`)
	fs.BoolVar(&g.required, "require-config-checksum", false,
		"refuse to run unless -config-checksum verifies the config file (same as require_config_checksum in the config)")
}

// load reads the config at path, verifies it, and parses the bytes it verified.
//
// Ordering matters. When an expected checksum is available the file is hashed
// before it is parsed, so a rejected file is never interpreted -- no vault
// client, no env:// expansion, no plugin values. When none is available the only
// thing read out of the unverified file is the require_config_checksum flag,
// which can only ever make us stricter.
//
// allowMissing means "fall back to the built-in defaults when there is no file",
// which is how a bare `mcphe` and `mcphe digest` behave.
func (g configGuard) load(path string, allowMissing bool) (config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if allowMissing && os.IsNotExist(err) {
			// Running on built-in defaults. There is no file whose integrity
			// could be checked, which is fine only if nobody asked for a check.
			if g.expected != "" || g.required {
				return config.Config{}, fmt.Errorf("no config file at %s: the config checksum gate needs a file to verify", path)
			}
			return config.DefaultConfig, nil
		}
		if os.IsNotExist(err) {
			return config.Config{}, fmt.Errorf("config file not found: %s", path)
		}
		return config.Config{}, fmt.Errorf("error reading config: %w", err)
	}
	return g.loadBytes(path, data)
}

// checkRunAsRoot refuses to start as root unless the config says run_as_root:
// true, and does so before anything else acts on the config.
//
// It reads the file itself because that is the whole point: the config must not
// be parsed, resolved or used to log in to Vault before this decision is made.
// The bytes are re-read by load(), which verifies them; a file that changed in
// between is caught there by the checksum, and without a checksum the second
// read is the one that is acted on, so no unverified value survives.
func (g configGuard) checkRunAsRoot(path string, allowMissing bool) error {
	if !isRoot() {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if allowMissing && os.IsNotExist(err) {
			// No file: built-in defaults, and run_as_root defaults to false.
			return fmt.Errorf("This program is not designed to run as root.\nIf you really want to run as root, add 'run_as_root: true' to your config file.")
		}
		// A config we cannot read is reported by load(); there is nothing to
		// learn about run_as_root here.
		return nil
	}
	allowed, err := config.PeekRunAsRoot(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if !allowed {
		return fmt.Errorf("This program is not designed to run as root.\nIf you really want to run as root, add 'run_as_root: true' to your config file.")
	}
	return nil
}

// loadBytes is load for bytes the caller has already read. It exists so a
// pre-parse check (the root guard, which must see run_as_root before the config
// is acted on) can work from the same bytes the gate verifies and parses -- one
// read, one set of bytes, no window in which the file changes underneath.
func (g configGuard) loadBytes(path string, data []byte) (config.Config, error) {
	var err error
	var want integrity.Checksum
	if g.expected != "" {
		want, err = parseExpectedChecksum(g.expected)
		if err != nil {
			return config.Config{}, err
		}
		if err := integrity.VerifyData(path, data, want); err != nil {
			return config.Config{}, fmt.Errorf("config integrity check failed: %w", err)
		}
	} else {
		required := g.required
		if !required {
			required, err = config.PeekRequireConfigChecksum(data)
			if err != nil {
				return config.Config{}, fmt.Errorf("%s: %w", path, err)
			}
		}
		if required {
			return config.Config{}, fmt.Errorf(
				"require_config_checksum is set, but no expected checksum was supplied: pass -config-checksum <algo:hex> "+
					"(find the value with `mcphe config verify -config %s`)", path)
		}
	}

	cfg, err := config.LoadConfigBytes(path, data)
	if err != nil {
		return config.Config{}, err
	}
	if !want.IsZero() {
		cfg.Logf(1, "Config checksum verified: %s (%s)", want, path)
	}
	return cfg, nil
}

// parseExpectedChecksum turns a -config-checksum value into a Checksum. The
// value may be a literal digest or an env:// / file:// reference, so the
// expected value can live in an EnvironmentFile or a root-owned file instead of
// in the service unit's argv.
func parseExpectedChecksum(raw string) (integrity.Checksum, error) {
	resolved, err := config.ResolveValue(raw)
	if err != nil {
		return integrity.Checksum{}, fmt.Errorf("resolving -config-checksum: %w", err)
	}
	sum, err := integrity.ParseChecksum(strings.TrimSpace(resolved))
	if err != nil {
		return integrity.Checksum{}, fmt.Errorf("-config-checksum: %w", err)
	}
	return sum, nil
}

// runConfigCommand implements:
//
//	mcphe config verify [-config path] -checksum <algo:hex|env://VAR|file://path>
//
// It answers one question without starting the server, loading plugins, or
// parsing the config: does this file match the digest you expect? Exit status is
// 0 on a match and 1 otherwise, so it works as a systemd ExecStartPre, a
// deployment gate, or a CI step. With no -checksum it prints the file's current
// digest and the exact command to pin it.
func runConfigCommand(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	configPath := fs.String("config", "./config.yaml", "path to config.yaml")
	checksum := fs.String("checksum", "",
		`expected checksum: "sha256:<hex>", or "env://VAR" / "file://path" to read it from elsewhere`)
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintln(out, "  mcphe config verify [-config path] -checksum <sha256:hex|env://VAR|file://path>")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Compares the bytes of the config file with an expected checksum and exits 0 on a")
		fmt.Fprintln(out, "match, 1 otherwise. The file is not parsed, so this checks integrity only.")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Omit -checksum to print the file's current digest and the command that pins it.")
		fs.PrintDefaults()
	}

	if len(args) < 1 || args[0] != "verify" {
		fs.Usage()
		os.Exit(2)
	}
	fs.Parse(args[1:])
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "config verify takes no positional arguments (got %q)\n", fs.Arg(0))
		os.Exit(2)
	}

	data, err := os.ReadFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config %q: %v\n", *configPath, err)
		os.Exit(1)
	}

	if *checksum == "" {
		got := integrity.HashBytes(data)
		fmt.Fprintf(os.Stderr, "%s is %s\n", *configPath, got)
		fmt.Fprintf(os.Stderr, "pin it with: mcphe config verify -checksum %s -config %s\n", got, *configPath)
		os.Exit(2)
	}

	want, err := parseExpectedChecksum(*checksum)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}
	if err := integrity.VerifyData(*configPath, data, want); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ok: %s matches %s\n", *configPath, want)
}
