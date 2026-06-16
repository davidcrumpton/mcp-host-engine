/// <reference path="../../types/mcphe.d.ts" />

/**
 * get_ip — returns the public IP address of the host.
 *
 * goja constraints:
 *   - call() is synchronous — no async/await
 *   - host.http.get headers are a flat { key: string } object
 *   - no `this` inside call() — not needed here, but noted
 */

interface IPResponse {
  ip?: string;
  country_code?: string;
}

const plugin = {
  name: "get_ip",
  description: "Get the public IP address of the host.",
  version: "1.1.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  false,
    openWorldHint:   true,
  },
  inputSchema: {
    type: "object",
    properties: {},
    required: [] as string[],
  },
  call(_params: Record<string, unknown>): string {
    const response = host.http.get("https://ifconfig.io/all.json");
    const payload  = JSON.parse(response.body) as IPResponse;
    return `${payload.country_code ?? "unknown"}: ${payload.ip ?? "unknown"}`;
  },
};

module.exports = plugin;
