module.exports = {
  name: "run_command",
  description: "Run a shell command on the host.",
  version: "1.0.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint:    false,
    destructiveHint: true,
    idempotentHint:  false,
    openWorldHint:   true,
  },
  inputSchema: {
    type: "object",
    properties: {
      command: { type: "string", description: "Shell command to run." },
    },
    required: ["command"],
  },
  call(params) {
    return host.runCommand(params.command);
  },
};
