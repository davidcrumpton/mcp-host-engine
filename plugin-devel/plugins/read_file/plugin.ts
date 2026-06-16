/// <reference path="../../types/mcphe.d.ts" />

const plugin = {
  name: "read_file",
  description: "Read a file from the host filesystem.",
  version: "1.1.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  true,
    openWorldHint:   false,
  },
  inputSchema: {
    type: "object",
    properties: {
      path: { type: "string", description: "Path to read." },
    },
    required: ["path"],
  },
  call(params: Record<string, unknown>) {
    return host.fs.readFile(params.path);
  },
};

module.exports = plugin;
