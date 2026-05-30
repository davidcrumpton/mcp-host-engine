module.exports = {
  name: "get_datetime",
  description: "Get the current date and time of the host.",
  version: "1.0.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  false,
    openWorldHint:   false,
  },
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    return new Date().toISOString();
  },
};