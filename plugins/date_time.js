module.exports = {
  name: "get_datetime",
  description: "Get the current date and time of the host.",
  version: "0.0.1",
  commit: "none",
  Tags: ["utility"],
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    return new Date().toISOString();
  },
};