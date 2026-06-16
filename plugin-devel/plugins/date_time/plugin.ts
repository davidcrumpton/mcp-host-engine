/// <reference path="../../types/mcphe.d.ts" />

const plugin = {
  name: "get_datetime",
  description: "Get the current date and time of the host.",
  version: "1.1.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  false,
    openWorldHint:   false,
  },
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params: Record<string, unknown>) {
    return new Date().toISOString();
  },
};

module.exports = plugin;
