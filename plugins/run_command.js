module.exports = {
  name: "run_command",
  description: "Run a shell command on the host.",
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
