/// <reference path="../../types/mcphe.d.ts" />

// L1: a goja-surfaced Go error may not be an Error, so `err.message` is
// undefined and the model receives {error: undefined}. Fall back to String().
function errText(err: unknown): string {
  const e = err as { message?: unknown } | null | undefined;
  if (e && typeof e.message === "string" && e.message) return e.message;
  return String(err);
}

const plugin = {
  name: "google_search",
  description: "Search Google using Programmable Search API.",
  version: "1.1.3",
  commit: "none",
  Tags: ["search", "utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  false,
    openWorldHint:   true,
  },
  inputSchema: {
    type: "object",
    properties: {
      query: { type: "string", description: "Search query." },
    },
    required: ["query"],
  },
  call(params: Record<string, unknown>) {
    const apiKey = host.config.google_api_key;
    const cx = host.config.google_cx_id;
    if (!apiKey || !cx) {
      throw new Error("google_api_key and google_cx_id must be configured");
    }
    const url = `https://customsearch.googleapis.com/customsearch/v1?key=${encodeURIComponent(String(apiKey))}&cx=${encodeURIComponent(String(cx))}&q=${encodeURIComponent(String(params.query))}&num=5`;

    // L5: no status check and no try/catch around JSON.parse, so a non-2xx HTML
    // error page or an empty body became an uncaught SyntaxError. The status is
    // checked first so the caller gets the reason rather than a parse failure.
    const response = host.http.request({method: "GET", url});

    if (response.status < 200 || response.status >= 300) {
      throw new Error(`Google Search returned HTTP ${response.status}`);
    }

    const body = String(response.body === undefined || response.body === null ? "" : response.body);
    if (!body.trim()) {
      throw new Error("Google Search returned an empty body");
    }

    let payload: any;
    try {
      payload = JSON.parse(body);
    } catch (parseErr) {
      throw new Error(`Google Search returned a non-JSON body: ${errText(parseErr)}`);
    }

    if (!payload || typeof payload !== "object") {
      throw new Error("Google Search returned an unexpected payload");
    }
    if (payload.error) {
      throw new Error(payload.error.message || "Google Search error");
    }

    // L5: `payload.items.length` was dereferenced without Array.isArray, so a
    // non-array `items` threw a bare TypeError.
    const items = Array.isArray(payload.items) ? payload.items : [];
    if (items.length === 0) {
      return "No results found.";
    }

    let output = `Google Search results for: ${params.query}\n\n`;
    for (let i = 0; i < items.length; i += 1) {
      const item = items[i] || {};
      output += `${i + 1}. ${item.title}\n${item.link}\n${item.snippet}\n\n`;
    }
    return output;
  },
};

module.exports = plugin;
