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

// L1: a goja-surfaced Go error may not be an Error, so `err.message` is
// undefined and the model receives {error: undefined}. Fall back to String().
function errText(err: unknown): string {
  const e = err as { message?: unknown } | null | undefined;
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
  call(_params: Record<string, unknown>): unknown {
    // L5: there was no status check and no guard around JSON.parse, so a
    // non-2xx HTML page or an empty body surfaced as an uncaught SyntaxError
    // out of call() instead of a structured failure.
    try {
      const resp = host.http.request({
        method: "GET",
        url: "https://ifconfig.io/all.json",
      });

      if (resp.status < 200 || resp.status >= 300) {
        return { success: false, error: `ifconfig.io returned HTTP ${resp.status}`, status: resp.status };
      }

      const body = String(resp.body === undefined || resp.body === null ? "" : resp.body);
      if (!body.trim()) {
        return { success: false, error: "ifconfig.io returned an empty body", status: resp.status };
      }

      let payload: IPResponse;
      try {
        payload = JSON.parse(body) as IPResponse;
      } catch (parseErr) {
        return {
          success: false,
          error: `ifconfig.io returned a non-JSON body: ${errText(parseErr)}`,
          status: resp.status,
        };
      }

      if (!payload || typeof payload !== "object") {
        return { success: false, error: "ifconfig.io returned an unexpected payload", status: resp.status };
      }

      const country = payload.country_code ?? "unknown";
      const ip = payload.ip ?? "unknown";
      return `${country}: ${ip}`;
    } catch (err: unknown) {
      return { success: false, error: errText(err) };
    }
  },
};

module.exports = plugin;
