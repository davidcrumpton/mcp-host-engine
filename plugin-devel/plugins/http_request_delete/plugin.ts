/// <reference path="../../types/mcphe.d.ts" />

const plugin = {
  name: "http_request_delete",
  description: "Make an HTTP DELETE request to a specified URL with optional headers.",
  version: "1.1.0",
  commit: "none",
  Tags: ["http", "utility"],
  annotations: {
    readOnlyHint:    false,
    destructiveHint: true,
    idempotentHint:  false,
    openWorldHint:   true,
  },
  inputSchema: {
    type: "object",
    properties: {
      url: { type: "string", description: "The URL to send the DELETE request to." },
      headers: {
        type: "object",
        additionalProperties: { type: "string" },
        description: "Optional headers to include in the request.",
      },
    },
    required: ["url"],
  },
  call(params: Record<string, unknown>) {
    const response = host.httpDelete(params.url, params.headers || {});
    return response;
  },
};

module.exports = plugin;
