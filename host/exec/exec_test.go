package exec

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mcphe/config"
)

func cfgWithCommands(pluginName string, cmds []string) config.Config {
	iCmds := make([]interface{}, len(cmds))
	for i, c := range cmds {
		iCmds[i] = c
	}
	return config.Config{
		Plugins: map[string]map[string]interface{}{
			pluginName: {"allowed_commands": iCmds},
		},
	}
}

func TestRunCommand_Success(t *testing.T) {
	cfg := cfgWithCommands("myplugin", []string{"echo"})
	out, err := RunCommand(context.Background(), cfg, "myplugin", "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected 'hello' in output, got %q", out)
	}
}

func TestRunCommand_NotAllowed(t *testing.T) {
	cfg := cfgWithCommands("myplugin", []string{"echo"})
	_, err := RunCommand(context.Background(), cfg, "myplugin", "rm -rf /")
	if err == nil {
		t.Fatal("expected command-not-allowed error")
	}
}

func TestRunCommand_NoAllowedCommands(t *testing.T) {
	_, err := RunCommand(context.Background(), config.Config{}, "myplugin", "echo hi")
	if err == nil {
		t.Fatal("expected error when no commands configured")
	}
}

// ---------------------------------------------------------------------------
// H1: the command string is an argv, not a shell program
// ---------------------------------------------------------------------------

func TestRunCommand_ShellMetacharactersAreLiteral(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX echo and paths")
	}
	dir := t.TempDir()
	canary := filepath.Join(dir, "pwned")

	cfg := cfgWithCommands("p", []string{"echo"})
	for _, tc := range []struct{ command, want string }{
		{"echo hi; touch " + canary, "hi; touch " + canary + "\n"},
		{"echo hi && touch " + canary, "hi && touch " + canary + "\n"},
		{"echo hi | tee " + canary, "hi | tee " + canary + "\n"},
		{"echo $(touch " + canary + ")", "$(touch " + canary + ")\n"},
		{"echo `touch " + canary + "`", "`touch " + canary + "`\n"},
	} {
		out, err := RunCommand(context.Background(), cfg, "p", tc.command)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", tc.command, err)
		}
		// The whole line is one echo argument: nothing else ran.
		if out != tc.want {
			t.Errorf("%q: got %q, want %q", tc.command, out, tc.want)
		}
		if _, statErr := os.Stat(canary); statErr == nil {
			t.Fatalf("%q: the injected command ran (canary exists)", tc.command)
		}
	}
}

func TestRunCommand_CommandSubstitutionIsNotExpanded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX echo")
	}
	cfg := cfgWithCommands("p", []string{"echo"})
	out, err := RunCommand(context.Background(), cfg, "p", "echo $(id) `id` $HOME")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "$(id) `id` $HOME\n"
	if out != want {
		t.Errorf("output: got %q, want %q (nothing may be expanded)", out, want)
	}
}

// An allow-list entry covers the executable, not a line that starts with it.
func TestRunCommand_AllowListIsTheExecutableOnly(t *testing.T) {
	cfg := cfgWithCommands("p", []string{"git"})
	for _, command := range []string{
		"git; id",
		"git && id",
		"git-evil",
		"/usr/bin/git status",
	} {
		if _, err := RunCommand(context.Background(), cfg, "p", command); err == nil {
			t.Errorf("%q: expected refusal", command)
		}
	}
}

func TestRunCommand_QuotingGroupsArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX echo")
	}
	cfg := cfgWithCommands("p", []string{"echo"})
	out, err := RunCommand(context.Background(), cfg, "p", `echo "two words" 'three words' four\ five`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "two words three words four five\n"
	if out != want {
		t.Errorf("output: got %q, want %q", out, want)
	}
}

func TestRunCommand_MalformedQuotingIsAnError(t *testing.T) {
	cfg := cfgWithCommands("p", []string{"echo"})
	for _, command := range []string{`echo "unterminated`, `echo 'unterminated`, `echo trailing\`} {
		if _, err := RunCommand(context.Background(), cfg, "p", command); err == nil {
			t.Errorf("%q: expected an error rather than a guess", command)
		}
	}
}

// H1, second order: the child must not inherit the daemon's environment, or a
// single allow-listed command defeats allowed_env_vars.
func TestRunCommand_ChildEnvIsScrubbed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX env")
	}
	t.Setenv("MCPHE_TEST_ALLOWED", "visible")
	t.Setenv("MCPHE_TEST_SECRET", "must-not-leak")

	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"p": {
				"allowed_commands": []interface{}{"env"},
				"allowed_env_vars": []interface{}{"MCPHE_TEST_ALLOWED"},
			},
		},
	}
	out, err := RunCommand(context.Background(), cfg, "p", "env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "MCPHE_TEST_ALLOWED=visible") {
		t.Errorf("an allowed variable should reach the child; got:\n%s", out)
	}
	if strings.Contains(out, "MCPHE_TEST_SECRET") {
		t.Errorf("a variable that is not allowed must not reach the child; got:\n%s", out)
	}
}

func TestRunCommand_OutputIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /dev/zero")
	}
	cfg := cfgWithCommands("p", []string{"cat"})
	out, err := RunCommand(context.Background(), cfg, "p", "cat /dev/zero")
	if err == nil {
		t.Fatalf("expected an error for unbounded output, got %d bytes", len(out))
	}
	if !strings.Contains(err.Error(), "more than") {
		t.Errorf("error should say the output was capped, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// splitCommand
// ---------------------------------------------------------------------------

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		command string
		want    []string
	}{
		{"", nil},
		{"   ", nil},
		{"ls", []string{"ls"}},
		{"ls -l /tmp", []string{"ls", "-l", "/tmp"}},
		{"  ls\t-l\n/tmp  ", []string{"ls", "-l", "/tmp"}},
		{`echo "a b"`, []string{"echo", "a b"}},
		{`echo 'a b'`, []string{"echo", "a b"}},
		{`echo a\ b`, []string{"echo", "a b"}},
		{`echo ""`, []string{"echo", ""}},
		{`echo "a\"b"`, []string{"echo", `a"b`}},
		{`echo 'a\b'`, []string{"echo", `a\b`}},
		// Nothing is expanded, substituted or globbed.
		{`echo $(id) $HOME *.txt`, []string{"echo", "$(id)", "$HOME", "*.txt"}},
		{`echo hi; id`, []string{"echo", "hi;", "id"}},
	}
	for _, tc := range cases {
		got, err := splitCommand(tc.command)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.command, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%q: got %q, want %q", tc.command, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q: got %q, want %q", tc.command, got, tc.want)
				break
			}
		}
	}
}

func TestSplitCommandRejects(t *testing.T) {
	for _, command := range []string{
		`echo "unterminated`,
		`echo 'unterminated`,
		`echo trailing\`,
		"echo a\x00b",
	} {
		if _, err := splitCommand(command); err == nil {
			t.Errorf("%q: expected an error", command)
		}
	}
}
