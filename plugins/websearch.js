"use strict";
const MAX_HTML = 1 << 20;
const MAX_RESULTS = 50;
const DEFAULT_RESULTS = 5;
function clampMaxResults(raw) {
  const n = typeof raw === "number" ? raw : parseInt(String(raw === void 0 || raw === null ? "" : raw), 10);
  if (!isFinite(n) || n < 1) return DEFAULT_RESULTS;
  return n > MAX_RESULTS ? MAX_RESULTS : Math.floor(n);
}
function errText(err) {
  const e = err;
  if (e && typeof e.message === "string" && e.message) return e.message;
  return String(err);
}
const plugin = {
  name: "websearch",
  description: "Search DuckDuckGo",
  version: "1.1.3",
  commit: "none",
  tags: ["websearch", "duckduckgo"],
  annotations: {
    readOnlyHint: false,
    destructiveHint: false,
    idempotentHint: false,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {
      CommandEvent: {
        type: "string",
        description: "The command to execute.",
        enum: ["websearch"]
      },
      query: {
        type: "string",
        description: "The search query."
      },
      max_results: {
        type: "integer",
        description: "The maximum number of results to return.",
        default: 5,
        minimum: 1,
        maximum: 50
      }
    },
    required: ["CommandEvent", "query"]
  },
  call(params) {
    try {
      const maxResults = clampMaxResults(params.max_results);
      const encoded = encodeURIComponent(params.query);
      const res = host.http.request(
        {
          url: `https://html.duckduckgo.com/html/?q=${encoded}`,
          method: "GET",
          headers: {
            "User-Agent": "mcphe/1.0 (+duckduckgo-search)"
          }
        }
      );
      if (res.status < 200 || res.status >= 300) {
        return {
          success: false,
          error: `DuckDuckGo returned HTTP ${res.status}`,
          status: res.status,
          query: params.query
        };
      }
      const html = String(res.body === void 0 || res.body === null ? "" : res.body).slice(0, MAX_HTML);
      const pattern = /<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)<\/a>[\s\S]*?(?:result__snippet[^>]*>(.*?)<\/)/g;
      const results = [];
      let match;
      while (match = pattern.exec(html)) {
        if (results.length >= maxResults) break;
        let [, href, title, snippet] = match;
        title = title.replace(/<[^>]+>/g, "").trim();
        snippet = snippet.replace(/<[^>]+>/g, "").trim();
        try {
          const parsed = host.url.parse(href, "https://duckduckgo.com");
          const uddg = parsed.searchParams.get("uddg");
          if (uddg) href = decodeURIComponent(uddg);
        } catch (err) {
          host.server.logger(4, `websearch: could not de-redirect ${href}: ${errText(err)}`);
        }
        results.push({
          title,
          url: href,
          snippet
        });
      }
      return {
        success: true,
        query: params.query,
        count: results.length,
        results
      };
    } catch (err) {
      return {
        success: false,
        error: errText(err)
      };
    }
  }
};
module.exports = plugin;
