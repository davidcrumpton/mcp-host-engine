"use strict";
function kibanaGlobMatch(pattern, value) {
  let re = "^";
  for (let i = 0; i < pattern.length; i++) {
    const ch = pattern.charAt(i);
    if (ch === "*") {
      re += ".*";
    } else if ("\\^$.|?+()[]{}".indexOf(ch) !== -1) {
      re += "\\" + ch;
    } else {
      re += ch;
    }
  }
  return new RegExp(re + "$").test(value);
}
function kibanaIndexGuard(index, cfg) {
  const name = typeof index === "string" ? index : "";
  if (!name) {
    return "index is required";
  }
  const configured = cfg && Array.isArray(cfg.allowed_index_patterns) ? cfg.allowed_index_patterns.map(function(p) {
    return String(p);
  }) : [];
  if (configured.length) {
    for (let i = 0; i < configured.length; i++) {
      if (kibanaGlobMatch(configured[i], name)) {
        return null;
      }
    }
    return `index ${JSON.stringify(name)} is not permitted by allowed_index_patterns (${configured.join(", ")}).`;
  }
  if (name.indexOf("*") !== -1 && cfg.allow_wildcard_index !== true) {
    return `index ${JSON.stringify(name)} contains '*', which expands to every index the credential can see, security indices included. Set allowed_index_patterns (or allow_wildcard_index: true) in the plugin config to permit it.`;
  }
  return null;
}
const plugin = {
  name: "kibana",
  description: "Fetch data from Kibana/Elasticsearch using query string syntax.",
  version: "1.1.3",
  commit: "none",
  Tags: ["search", "kibana"],
  annotations: {
    readOnlyHint: true,
    destructiveHint: false,
    idempotentHint: true,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {
      indexName: { type: "string", description: "Elasticsearch index to search." },
      query: { type: "string", description: "Lucene/KQL query string." },
      size: { type: "number", description: "Number of results to return." },
      from: { type: "number", description: "Number of results to skip." }
    },
    required: ["query", "indexName"]
  },
  // call() must NOT be async. All host.* calls are synchronous — no await needed.
  call(params) {
    function envOr(key, fallback) {
      try {
        return host.process.env(key) || fallback;
      } catch (_e) {
        return fallback;
      }
    }
    const cfg = host.config;
    const baseUrl = envOr("KIBANA_BASE_URL", cfg.kibana_base_url);
    const username = envOr("KIBANA_USERNAME", cfg.kibana_username);
    const password = envOr("KIBANA_PASSWORD", cfg.kibana_password);
    if (!baseUrl) {
      throw new Error("kibana_base_url must be set in plugin config");
    }
    if (username && !password || !username && password) {
      throw new Error(
        "kibana_username and kibana_password must both be set, or neither. Only one is configured, so the request would be sent unauthenticated."
      );
    }
    const headers = {
      "Content-Type": "application/json",
      "kbn-xsrf": "true"
    };
    if (username && password) {
      headers["Authorization"] = "Basic " + host.utils.btoa(username + ":" + password);
    }
    const size = params.size || 10;
    const from = params.from || 0;
    const indexProblem = kibanaIndexGuard(params.indexName, cfg);
    if (indexProblem) {
      throw new Error(indexProblem);
    }
    const url = baseUrl.replace(/\/$/, "") + "/api/console/proxy?path=" + encodeURIComponent("/" + params.indexName + "/_search?size=" + size + "&from=" + from) + "&method=POST";
    try {
      const resp = host.http.request({
        method: "POST",
        url,
        headers,
        body: params.query
      });
      if (resp.status !== 200) {
        return { success: false, error: "HTTP " + resp.status, body: resp.body };
      }
      return { success: true, data: JSON.parse(resp.body) };
    } catch (err) {
      return { success: false, error: err.message };
    }
  }
};
module.exports = plugin;
