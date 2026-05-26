module.exports = {
  name: "http_request_delete",
  description: "Make an HTTP DELETE request to a specified URL with optional headers.",
  version: "0.0.1",
  commit: "none",
  Tags: ["http", "utility"],
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
  call(params) {
    const response = host.httpDelete(params.url, params.headers || {});
    return response;
  },
};
    