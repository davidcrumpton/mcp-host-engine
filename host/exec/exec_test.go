package exec

import (
	"context"
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