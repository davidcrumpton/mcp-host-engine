"use strict";
const plugin = {
  name: "get_ip",
  description: "Get the public IP address of the host.",
  version: "1.1.2",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint: true,
    destructiveHint: false,
    idempotentHint: false,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {},
    required: []
  },
  call(_params) {
    var _a, _b;
    var resp = host.http.request({
      method: "GET",
      url: "https://ifconfig.io/all.json"
    });
    const payload = JSON.parse(resp.body);
    return `${(_a = payload.country_code) != null ? _a : "unknown"}: ${(_b = payload.ip) != null ? _b : "unknown"}`;
  }
};
module.exports = plugin;
