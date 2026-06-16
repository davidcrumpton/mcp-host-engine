/// <reference path="../../types/mcphe.d.ts" />

const plugin = {
  name: "write_file",
  description: "Write content to a file.",
  version: "1.1.0",
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
  call(params: Record<string, unknown>) {
    return host.fs.writeFile(params.path as string, params.content as string);
  }
};

module.exports = plugin;
