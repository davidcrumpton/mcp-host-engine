module.exports = {
  name: "write_file",
  description: "Write content to a file.",
  version: "1.0.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint:    false,
    destructiveHint: true,
    idempotentHint:  true,
    openWorldHint:   false,
  },
  inputSchema: {
    type: "object",
    properties: {
      path: { type: "string", description: "The path to the file to write." },
      content: { type: "string", description: "The content to write to the file." }
    },
    required: ["path", "content"]
  },
  call(params) {
    return host.writeFile(params.path, params.content);
  }
};
