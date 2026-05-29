module.exports = {
  name: "wikipedia_search_test",
  description: "A simple Wikipedia search tool for testing MCP Apps.",
  version: "1.0.0",
  Tags: ["search", "test", "utility"],

  // Add _meta.ui to make it an MCP App
  _meta: {
    ui: {
      resourceUri: "https://en.wikipedia.org/wiki/Special:Search", // Wikipedia search page
      type: "web" // Specifies this is a web-based UI
    }
  },

  inputSchema: {
    type: "object",
    properties: {
      query: {
        type: "string",
        description: "The search query for Wikipedia."
      }
    },
    required: ["query"]
  },

  // Tool logic
  call(params) {
    const encoded = encodeURIComponent(params.query);
    const url = `https://en.wikipedia.org/api/rest_v1/page/summary/${encoded}`;

    try {
      const response = host.httpGet(url, {
        headers: {
          "User-Agent": "mcp-test-wikipedia/1.0 (Testing MCP App)",
          "Accept": "application/json"
        }
      });

      const status = response.status ?? response.statusCode;
      const body = typeof response.body === "string" ? response.body : response.text?.() ?? "";
      const payload = JSON.parse(body);

      if (payload.title && payload.extract) {
        return {
          success: true,
          result: {
            title: payload.title,
            summary: payload.extract,
            url: `https://en.wikipedia.org/wiki/${encoded}`
          }
        };
      } else {
        return {
          success: false,
          result: "No results found for the query."
        };
      }
    } catch (err) {
      return {
        success: false,
        result: `Error: ${err.message}`
      };
    }
  }
};
