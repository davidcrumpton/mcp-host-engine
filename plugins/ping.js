module.exports = {
  name: "ping",
  description: "A simple ping/pong tool for testing.",
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    return "pong";
  },
};
