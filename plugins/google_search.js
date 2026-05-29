module.exports = {
  name: "google_search",
  description: "Search Google using Programmable Search API.",
  version: "1.0.0",
  commit: "none",
  Tags: ["search", "utility"],
  inputSchema: {
    type: "object",
    properties: {
      query: { type: "string", description: "Search query." },
    },
    required: ["query"],
  },
  call(params) {
    const apiKey = host.config.google_api_key;
    const cx = host.config.google_cx_id;
    if (!apiKey || !cx) {
      throw new Error("google_api_key and google_cx_id must be configured");
    }
    const url = `https://customsearch.googleapis.com/customsearch/v1?key=${encodeURIComponent(apiKey)}&cx=${encodeURIComponent(cx)}&q=${encodeURIComponent(params.query)}&num=5`;
    const response = host.httpGet(url);
    const payload = JSON.parse(response.body);
    if (payload.error) {
      throw new Error(payload.error.message || "Google Search error");
    }
    if (!payload.items || payload.items.length === 0) {
      return "No results found.";
    }
    let output = `Google Search results for: ${params.query}\n\n`;
    for (let i = 0; i < payload.items.length; i += 1) {
      const item = payload.items[i];
      output += `${i + 1}. ${item.title}\n${item.link}\n${item.snippet}\n\n`;
    }
    return output;
  },
};
