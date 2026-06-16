/// <reference path="../../types/mcphe.d.ts" />

const plugin = {
  name: "http_request_get",
  description: "Fetch content from a URL and return extracted plain text.",
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
    properties: {
      url: { type: "string", description: "URL to fetch." },
      // mode: text or html, default to text
      mode: {
        type: "string",
        enum: ["text", "html"],
        default: "text",
        description: "Whether to return plain text or raw HTML.",
      },
    },
    required: ["url"],
  },
  call(params: Record<string, unknown>) {
    const response = host.http.get(params.url);
    if (typeof response !== "object" || response === null) {
      return response;
    }

    const body = response.body || "";
    if (params.mode === "html") {
      return `HTTP ${response.status}\n\n${body}`;
    }
    const text = extractText(body);
    return `HTTP ${response.status}\n\n${text}`;
  },
};

function extractText(html) {
  return html
    // Remove <script> and <style> blocks entirely (content + tags)
    .replace(/<script[\s\S]*?<\/script>/gi, "")
    .replace(/<style[\s\S]*?<\/style>/gi, "")
    // Remove HTML comments
    .replace(/<!--[\s\S]*?-->/g, "")
    // Convert common block elements to newlines for readability
    .replace(/<\/?(p|div|br|li|tr|h[1-6]|blockquote|pre)[^>]*>/gi, "\n")
    // Strip all remaining tags
    .replace(/<[^>]+>/g, "")
    // Decode common HTML entities
    .replace(/&nbsp;/gi, " ")
    .replace(/&amp;/gi, "&")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/&apos;/gi, "'")
    // Collapse runs of whitespace/blank lines
    .replace(/[ \t]+/g, " ")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

module.exports = plugin;
