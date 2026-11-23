"use strict";
const plugin = {
  name: "run_command",
  description: "Run a single command with arguments directly on the host. There is NO shell: the command is split into an argument vector and executed directly, so pipes, redirection, globbing, variable expansion, quotes and `;`/`&&` are NOT interpreted. `ps aux | grep nginx` passes the literal '|' and 'grep' as arguments to ps. Only the executable (argv[0]) is checked against the plugin's allowed_commands list; every later argument is passed through unchecked, so that list is the entire security boundary. It must not include interpreters or flag-bearing tools that can execute other programs (sh, bash, env, find, tar, curl, git).",
  version: "1.2.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint: false,
    destructiveHint: true,
    idempotentHint: false,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {
      command: {
        type: "string",
        description: "Command and arguments, e.g. `systemctl status nginx`. Executed directly, not through a shell -- shell syntax is not supported and will be passed through literally as arguments."
      }
    },
    required: ["command"]
  },
  call(params) {
    return host.exec.runCommand(params.command);
  }
};
module.exports = plugin;
