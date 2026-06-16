/// <reference path="../../types/mcphe.d.ts" />

// I have not tested this but it looks like it should work.

const plugin = {
  name:        "elastic7",
  description: "Fetch data from Elastic7",
  version: "1.1.0",
  commit:      "none",
  Tags:        ["search", "elastic7"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  true,
    openWorldHint:   true,
  },
  inputSchema: {
    type: "object",
    properties: {
      index: { type: "string", description: "Index to search in." },
      query: { type: "object", description: "Query to search for." },
    },
    required: ["index", "query"],
  },

  // call() must NOT be async. All host.* calls are synchronous — no await needed.
  call(params: Record<string, unknown>) {
    const cfg     = host.server.config();
    const baseUrl = host.process.env("ELASTIC_BASE_URL") || cfg.base_url;
    const apiKey  = host.process.env("ELASTIC_API_KEY") || cfg.api_key;

    if (!baseUrl || !apiKey) {
      throw new Error("ELASTIC_BASE_URL and ELASTIC_API_KEY must be set in plugin config");
    }

    host.server.logger(3, "elastic7: fetching from index=" + params.index);

    const url = baseUrl + "/" + encodeURIComponent(params.index) + "_search";

    // Headers: flat object — do NOT nest under a "headers" key
    const headers = {
      "Authorization": "Bearer " + apiKey,
      "Accept":        "application/json",
    };

    try {
      const resp = host.http.rawPost(url, headers, JSON.stringify(params.query));

      host.server.logger(3, "elastic7: response status=" + resp.status);

      if (resp.status === 404) {
        return { success: false, error: "Record not found" };
      }
      if (resp.status !== 200) {
        return { success: false, error: "HTTP error " + resp.status };
      }

      return { success: true, data: JSON.parse(resp.body) };

    } catch (err) {
      host.server.logger(1, "elastic7 error: " + err.message);
      return { success: false, error: err.message };
    }
  },
};

module.exports = plugin;
