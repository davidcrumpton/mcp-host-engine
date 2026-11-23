"use strict";
function errText(err) {
  const e = err;
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
    readOnlyHint: true,
    destructiveHint: false,
    idempotentHint: false,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {
      query: { type: "string", description: "Search query." }
    },
    required: ["query"]
  },
  call(params) {
    const apiKey = host.config.google_api_key;
    const cx = host.config.google_cx_id;
    if (!apiKey || !cx) {
      throw new Error("google_api_key and google_cx_id must be configured");
    }
    const url = `https://customsearch.googleapis.com/customsearch/v1?key=${encodeURIComponent(String(apiKey))}&cx=${encodeURIComponent(String(cx))}&q=${encodeURIComponent(String(params.query))}&num=5`;
    const response = host.http.request({ method: "GET", url });
    if (response.status < 200 || response.status >= 300) {
      throw new Error(`Google Search returned HTTP ${response.status}`);
    }
    const body = String(response.body === void 0 || response.body === null ? "" : response.body);
    if (!body.trim()) {
      throw new Error("Google Search returned an empty body");
    }
    let payload;
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
    const items = Array.isArray(payload.items) ? payload.items : [];
    if (items.length === 0) {
      return "No results found.";
    }
    let output = `Google Search results for: ${params.query}

`;
    for (let i = 0; i < items.length; i += 1) {
      const item = items[i] || {};
      output += `${i + 1}. ${item.title}
${item.link}
${item.snippet}

`;
    }
    return output;
  }
};
module.exports = plugin;
