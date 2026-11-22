// host/exec/exec.go
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"mcphe/config"
)

const (
	// maxOutputBytes caps what one command may return. A child's output is read
	// into memory, so without a cap a single allow-listed `yes` grows the heap
	// until the process dies.
	maxOutputBytes = 1 << 20 // 1 MiB

	// maxArgs bounds the argv a plugin can hand us. It is a guard against a
	// pathological argument list, not a meaningful limit.
	maxArgs = 4096
)

// envPassthrough lists the variables a child keeps without being named in the
// plugin's allowed_env_vars: the ones an ordinary program needs to start and to
// find its own files. Nothing secret belongs in this list -- the point of it is
// that a plugin allowed to run `git` does not thereby get VAULT_TOKEN,
// GITLAB_API_KEY and the rest of the daemon's environment.
var envPassthrough = map[string]bool{
	"PATH":    true,
	"HOME":    true,
	"USER":    true,
	"LOGNAME": true,
	"LANG":    true,
	"TMPDIR":  true,
	"TZ":      true,
	"TERM":    true,
	"PWD":     true,
	"SHELL":   true,
	// Windows needs these for any process to start at all.
	"SystemRoot": true,
	"COMSPEC":    true,
	"PATHEXT":    true,
}

// RunCommand runs a command for a plugin.
//
// The allow-list names executables, so only argv[0] is compared against it, and
// argv[0] is the only thing that is executed: there is no shell in between. The
// command string is split into an argument vector by splitCommand, which does
// quoting and nothing else, so `echo hi; rm -rf /` runs /bin/echo with the
// literal arguments "hi;" and "rm" and "-rf" and "/". Shell metacharacters are
// inert text, which is the whole point: an allow-list that can be escaped with
// `;`, `$(...)` or `&&` is not an allow-list.
func RunCommand(ctx context.Context, cfg config.Config, pluginName string, command string) (string, error) {
	argv, err := splitCommand(command)
	if err != nil {
		return "", fmt.Errorf("invalid command %q: %w", command, err)
	}
	if len(argv) == 0 {
		return "", fmt.Errorf("command %q is not allowed", command)
	}
	if len(argv) > maxArgs {
		return "", fmt.Errorf("command %q has %d arguments, more than the %d allowed", command, len(argv), maxArgs)
	}

	// Match on the executable name only. Comparing argv[0] (rather than a
	// prefix of the line) is what stops an allow-listed "git" from permitting
	// "git-evil", and it is now also the only thing that runs.
	cmdName := argv[0]
	allowed := false
	for _, allowedCmd := range cfg.AllowedRunCommandsFor(pluginName) {
		if cmdName == allowedCmd {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("command %q is not allowed", command)
	}

	// A cancellable child of ctx: cancelling it on output overflow kills the
	// process, so an unbounded producer cannot keep writing into a pipe nobody
	// is reading.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(runCtx, cmdName, argv[1:]...)
	cmd.Env = childEnv(cfg, pluginName)
	out := &cappedBuffer{limit: maxOutputBytes, onOverflow: cancel}
	cmd.Stdout = out
	cmd.Stderr = out

	err = cmd.Run()
	if out.overflowed() {
		return "", fmt.Errorf("command %q produced more than %d bytes of output; it was stopped", cmdName, maxOutputBytes)
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

// childEnv returns the environment a plugin's command runs with: the daemon's
// own variables, filtered to the ones this plugin is allowed to read plus the
// harmless ones every process needs. Failing closed here matters because the
// child inherits whatever we pass: `runCommand("env")` (or any command that
// dumps its environment) must not be a way around allowed_env_vars.
func childEnv(cfg config.Config, pluginName string) []string {
	allowed := make(map[string]bool)
	for _, name := range cfg.AllowedENVsFor(pluginName) {
		allowed[name] = true
	}
	env := make([]string, 0, len(allowed)+len(envPassthrough))
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if allowed[name] || envPassthrough[name] || strings.HasPrefix(name, "LC_") {
			env = append(env, entry)
		}
	}
	return env
}

// splitCommand splits a command string into an argument vector the way a POSIX
// shell splits words -- whitespace separates, single quotes are literal, double
// quotes group, a backslash escapes the next character -- and performs nothing
// else. There is no expansion, substitution, globbing, piping or redirection:
// "$(id)", "`id`", "*" and "a;b" all arrive as ordinary text.
//
// Splitting here rather than handing the string to `sh -c` is what makes the
// allow-list mean anything. Quoting is honoured so that `git commit -m "two
// words"` still passes one argument, but a quoting mistake is reported instead
// of being silently reinterpreted by a shell.
func splitCommand(command string) ([]string, error) {
	var (
		args    []string
		current strings.Builder
		inWord  bool
		quote   rune // 0 when not inside quotes, else '\'' or '"'
		escaped bool
	)
	flush := func() {
		if inWord {
			args = append(args, current.String())
			current.Reset()
			inWord = false
		}
	}

	for _, r := range command {
		if r == 0 {
			return nil, errors.New("command contains a NUL byte")
		}
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		switch quote {
		case '\'':
			// Inside single quotes everything is literal, including backslashes.
			if r == '\'' {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		case '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				current.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\\':
			escaped = true
			inWord = true
		case '\'':
			quote = '\''
			inWord = true
		case '"':
			quote = '"'
			inWord = true
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			current.WriteRune(r)
			inWord = true
		}
	}
	if escaped {
		return nil, errors.New("command ends with a trailing backslash")
	}
	if quote != 0 {
		return nil, errors.New("command has an unterminated quote")
	}
	flush()
	return args, nil
}

// cappedBuffer collects a command's output up to a byte limit and reports
// (once) when the limit is passed. Stdout and stderr are copied by two separate
// goroutines, so it is internally synchronised.
type cappedBuffer struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	limit      int
	truncated  bool
	onOverflow func()
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.truncated {
		// Keep accepting (and discarding) so the copier does not spin on errors
		// while the process is being killed.
		return len(p), nil
	}
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.buf.Write(p[:room])
		b.truncated = true
		if b.onOverflow != nil {
			b.onOverflow()
		}
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

func (b *cappedBuffer) overflowed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
