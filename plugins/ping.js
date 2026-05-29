module.exports = {
  name: "ping",
  description: "A simple ping/pong tool for testing.",
  version: "1.0.0",
  commit: "none",
  Tags: ["utility"],
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    return "pong";
  },
};
