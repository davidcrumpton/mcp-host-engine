module.exports = {
  name: "get_ip",
  description: "Get the public IP address of the host.",
  version: "1.0.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  false,
    openWorldHint:   true,
  },
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    const response = host.http.get("https://ifconfig.io/all.json");
    const payload = JSON.parse(response.body);
    return `${payload.country_code || "unknown"}: ${payload.ip || "unknown"}`;
  },
};
