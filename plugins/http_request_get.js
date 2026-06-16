"use strict";
const plugin = {
  name: "http_request_get",
  description: "Fetch content from a URL and return extracted plain text.",
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
    properties: {
      url: { type: "string", description: "URL to fetch." },
      // mode: text or html, default to text
      mode: {
        type: "string",
        enum: ["text", "html"],
        default: "text",
        description: "Whether to return plain text or raw HTML."
      }
    },
    required: ["url"]
  },
  call(params) {
    const response = host.http.get(params.url);
    if (typeof response !== "object" || response === null) {
      return response;
    }
    const body = response.body || "";
    if (params.mode === "html") {
      return `HTTP ${response.status}

${body}`;
    }
    const text = extractText(body);
    return `HTTP ${response.status}

${text}`;
  }
};
function extractText(html) {
  return html.replace(/<script[\s\S]*?<\/script>/gi, "").replace(/<style[\s\S]*?<\/style>/gi, "").replace(/<!--[\s\S]*?-->/g, "").replace(/<\/?(p|div|br|li|tr|h[1-6]|blockquote|pre)[^>]*>/gi, "\n").replace(/<[^>]+>/g, "").replace(/&nbsp;/gi, " ").replace(/&amp;/gi, "&").replace(/&lt;/gi, "<").replace(/&gt;/gi, ">").replace(/&quot;/gi, '"').replace(/&#39;/gi, "'").replace(/&apos;/gi, "'").replace(/[ \t]+/g, " ").replace(/\n{3,}/g, "\n\n").trim();
}
module.exports = plugin;
