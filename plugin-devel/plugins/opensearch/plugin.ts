/// <reference path="../../types/mcphe.d.ts" />

// This use Opensearch Dashboard's proxy for queries
// GET https://<domain>:5601/api/console/proxy?path=%2Fapm%2F_search&method=POST

const plugin = {
  name:        "opensearch",
  description: "Fetch data from OpenSearch Dashboard/OpenSearch using query string syntax.",
  version: "1.1.0",
  commit:      "none",
  Tags:        ["search", "opensearch"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  true,
    openWorldHint:   true,
  },
  inputSchema: {
    type: "object",
    properties: {
      indexName: { type: "string", description: "Elasticsearch index to search." },
      query:     { type: "string", description: "Lucene/KQL query string." },
      size:      { type: "number", description: "Number of results to return." },
      from:      { type: "number", description: "Number of results to skip." },
    },
    required: ["query", "indexName"],
  },

  // call() must NOT be async. All host.* calls are synchronous — no await needed.
  call(params: Record<string, unknown>) {
    const cfg     = host.config;

    // take from ENV first then cfg
    const baseUrl = host.process.env("OPENSEARCH_BASE_URL") || cfg.opensearch_base_url;
    const username = host.process.env("OPENSEARCH_USERNAME") || cfg.opensearch_username;
    const password = host.process.env("OPENSEARCH_PASSWORD") || cfg.opensearch_password;

    if (!baseUrl) {
      throw new Error("opensearch_base_url must be set in plugin config");
    }

    // Build Basic auth header using the standard JS built-in btoa()
    const headers = {
      "Content-Type": "application/json",
      "kbn-xsrf":     "true",
      "osd-xsrf":     "true",
    };
    if (username && password) {
      headers["Authorization"] = "Basic " + host.utils.btoa(username + ":" + password);
    }

    const size = params.size || 10;
    const from = params.from || 0;
    
    const url = baseUrl.replace(/\/$/, "")
      + "/api/console/proxy?path=" + encodeURIComponent("/" + params.indexName + "/_search?size=" + size + "&from=" + from) + "&method=POST";

    try {
      const resp = host.http.post(url, headers);

      if (resp.status !== 200) {
        return { success: false, error: "HTTP " + resp.status, body: resp.body };
      }

      return { success: true, data: JSON.parse(resp.body) };
    } catch (err) {
      return { success: false, error: err.message };
    }
  },
};

module.exports = plugin;
