module.exports = {
  name: "get_datetime",
  description: "Get the current date and time of the host.",
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    return new Date().toISOString();
  },
};