module.exports = {
  name: "battery_status",
  description: "Get battery status from the host.",
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    return host.runCommand("pmset -g batt");
  },
};
