module.exports = {
  name: "wikipedia_search",
  description: "Search Wikipedia for a query.",
  version: "0.0.1",
  commit: "none",
  Tags: ["search", "utility"],
  inputSchema: {
    type: "object",
    properties: {
      query: {
        type: "string",
        description: "Search query."
      }
    },
    required: ["query"]
  },

  call(params) {
    const encoded = encodeURIComponent(params.query);
    const url =
      `https://en.wikipedia.org/api/rest_v1/page/summary/${encoded}`;

    try {
      const response = host.httpGet(url, {
        headers: {
          "User-Agent":
            "mcphe-wikipedia-plugin/1.0 (https://github.com/davidcrumpton/mcp-host-engine/plugins/wikipedia_search.js; David Crumpton <david.crumpton>; mcphe <mcphe>)",
          "Accept": "application/json"
        }
      });

      const status = response.status ?? response.statusCode;

      const body =
        typeof response.body === "string"
          ? response.body
          : response.text?.() ?? "";

      const payload = JSON.parse(body);

      return {
        success: true,
        result: `${payload.title}: ${payload.extract}`
      };
    } catch (err) {
      return {
        success: false,
        result: err.message
      };
    }
  }
};