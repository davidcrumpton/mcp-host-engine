// host/exec/exec.go
package exec

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"mcphe/config"
)

// commandName returns the leading word of a shell command string, i.e. the
// executable name without arguments. This is used for allow-list matching so
// that "git" does not accidentally permit "git-evil".
func commandName(command string) string {
	// Trim leading whitespace, then take everything up to the first space/tab.
	command = strings.TrimLeft(command, " \t")
	if idx := strings.IndexAny(command, " \t"); idx >= 0 {
		return command[:idx]
	}
	return command
}

func RunCommand(ctx context.Context, cfg config.Config, pluginName string, command string) (string, error) {
	var cmd *exec.Cmd

	// Match on the command name (first whitespace-delimited token) rather than
	// a raw prefix, preventing "git-evil" from matching an allowlisted "git".
	cmdName := commandName(command)
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
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
