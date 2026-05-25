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
    // const url =
    //   `https://en.wikipedia.org/api/rest_v1/page/summary/${encoded}`;
    const url =
        `http://localhost:9090/api/rest_v1/page/summary/${encoded}`;

    let debugInfo = `Making request for: ${url}\n`;

    try {
      const response = host.httpGet(url, {
        headers: {
          "User-Agent":
            "llmctrlx-wikipedia-plugin/1.0 (https://github.com/davidcrumpton/llmctrlx)",
          "Accept": "application/json"
        }
      });

      const status = response.status ?? response.statusCode;
      debugInfo += `Status: ${status}\n`;
      debugInfo += `Headers: ${JSON.stringify(response.headers)}\n`;

      const body =
        typeof response.body === "string"
          ? response.body
          : response.text?.() ?? "";

      debugInfo += `Body preview: ${body.substring(0, 200)}\n`;

      const payload = JSON.parse(body);

      return {
        success: true,
        debugInfo,
        result: `${payload.title}: ${payload.extract}`
      };
    } catch (err) {
      debugInfo += `Error: ${err.message}\n`;

      return {
        success: false,
        debugInfo,
        result: err.message
      };
    }
  }
};