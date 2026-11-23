"use strict";
function errText(err) {
  const e = err;
  if (e && typeof e.message === "string" && e.message) return e.message;
  return String(err);
}
const plugin = {
  name: "get_ip",
  description: "Get the public IP address of the host.",
  version: "1.1.3",
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
    try {
      const resp = host.http.request({
        method: "GET",
        url: "https://ifconfig.io/all.json"
      });
      if (resp.status < 200 || resp.status >= 300) {
        return { success: false, error: `ifconfig.io returned HTTP ${resp.status}`, status: resp.status };
      }
      const body = String(resp.body === void 0 || resp.body === null ? "" : resp.body);
      if (!body.trim()) {
        return { success: false, error: "ifconfig.io returned an empty body", status: resp.status };
      }
      let payload;
      try {
        payload = JSON.parse(body);
      } catch (parseErr) {
        return {
          success: false,
          error: `ifconfig.io returned a non-JSON body: ${errText(parseErr)}`,
          status: resp.status
        };
      }
      if (!payload || typeof payload !== "object") {
        return { success: false, error: "ifconfig.io returned an unexpected payload", status: resp.status };
      }
      const country = (_a = payload.country_code) != null ? _a : "unknown";
      const ip = (_b = payload.ip) != null ? _b : "unknown";
      return `${country}: ${ip}`;
    } catch (err) {
      return { success: false, error: errText(err) };
    }
  }
};
module.exports = plugin;
