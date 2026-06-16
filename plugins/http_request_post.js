"use strict";
const plugin = {
  name: "http_request_post",
  description: "Make an HTTP POST request from the host.",
  version: "1.1.2",
  commit: "none",
  Tags: ["http", "utility"],
  annotations: {
    readOnlyHint: false,
    destructiveHint: false,
    idempotentHint: false,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {
      url: { type: "string", description: "URL to send the POST request to." },
      headers: {
        type: "object",
        additionalProperties: { type: "string" },
        description: "Optional headers to include in the request."
      },
      body: { type: "string", description: "Request body to send." }
    },
    required: ["url", "body"]
  },
  call(params) {
    const response = host.http.post(params.url, params.headers || void 0, params.body);
    return `Status: ${response.status}, Body: ${response.body}`;
  }
};
module.exports = plugin;
