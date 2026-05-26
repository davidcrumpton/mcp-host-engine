module.exports = {
  name: "ping",
  description: "A simple ping/pong tool for testing.",
  version: "0.0.1",
  commit: "none",
  Tags: ["utility"],
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    return "pong";
  },
};
