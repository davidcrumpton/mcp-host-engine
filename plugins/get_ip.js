module.exports = {
  name: "get_ip",
  description: "Get the public IP address of the host.",
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    const response = host.httpGet("https://ifconfig.io/all.json");
    const payload = JSON.parse(response.body);
    return `${payload.country_code || "unknown"}: ${payload.ip || "unknown"}`;
  },
};
