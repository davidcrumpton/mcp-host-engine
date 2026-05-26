module.exports = {
  name: "wikipedia_search",
  description: "Search Wikipedia for a query.",

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
            "llmctrlx-wikipedia-plugin/1.0 (https://github.com/davidcrumpton/llmctrlx)",
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
      debugInfo += `Error: ${err.message}\n`;

      return {
        success: false,
        result: err.message
      };
    }
  }
};