"use strict";
const plugin = {
  name: "http_request_delete",
  description: "Make an HTTP DELETE request to a specified URL with optional headers.",
  version: "1.1.2",
  commit: "none",
  Tags: ["http", "utility"],
  annotations: {
    readOnlyHint: false,
    destructiveHint: true,
    idempotentHint: false,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {
      url: { type: "string", description: "The URL to send the DELETE request to." },
      headers: {
        type: "object",
        additionalProperties: { type: "string" },
        description: "Optional headers to include in the request."
      }
    },
    required: ["url"]
  },
  call(params) {
    const response = host.http.delete(params.url, params.headers) || {};
    if (response.status !== 200) {
      console.log(`HTTP DELETE request to ${params.url} failed with status ${response.status}`);
    } else {
      console.log(`HTTP DELETE request to ${params.url} succeeded with status ${response.status}`);
    }
    return response;
  }
};
module.exports = plugin;
