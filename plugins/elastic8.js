"use strict";
function elastic8GlobMatch(pattern, value) {
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
function elastic8IndexGuard(index, cfg) {
  const name = typeof index === "string" ? index : "";
  if (!name) {
    return "index is required";
  }
  const configured = cfg && Array.isArray(cfg.allowed_index_patterns) ? cfg.allowed_index_patterns.map(function(p) {
    return String(p);
  }) : [];
  if (configured.length) {
    for (let i = 0; i < configured.length; i++) {
      if (elastic8GlobMatch(configured[i], name)) {
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
  name: "elastic8",
  description: "Fetch data from Elastic8",
  version: "1.1.1",
  commit: "none",
  Tags: ["search", "elastic8"],
  annotations: {
    readOnlyHint: true,
    destructiveHint: false,
    idempotentHint: true,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {
      index: { type: "string", description: "Index to search in." },
      query: { type: "object", description: "Query to search for." }
    },
    required: ["index", "query"]
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
    const cfg = host.server.config();
    const baseUrl = envOr("ELASTIC_BASE_URL", cfg.base_url);
    const apiKey = envOr("ELASTIC_API_KEY", cfg.api_key);
    if (!baseUrl || !apiKey) {
      throw new Error("ELASTIC_BASE_URL and ELASTIC_API_KEY must be set in plugin config or environment variables");
    }
    host.server.logger(3, "elastic8: fetching from index=" + params.index);
    const indexProblem = elastic8IndexGuard(params.index, cfg);
    if (indexProblem) {
      throw new Error(indexProblem);
    }
    const url = baseUrl + "/" + encodeURIComponent(params.index) + "_search";
    const headers = {
      "Authorization": "Bearer " + apiKey,
      "Accept": "application/json"
    };
    try {
      const resp = host.http.rawPost(url, headers, JSON.stringify(params.query));
      host.server.logger(3, "elastic8: response status=" + resp.status);
      if (resp.status === 404) {
        return { success: false, error: "Record not found" };
      }
      if (resp.status !== 200) {
        return { success: false, error: "HTTP error " + resp.status };
      }
      return { success: true, data: JSON.parse(resp.body) };
    } catch (err) {
      host.server.logger(1, "elastic8 error: " + err.message);
      return { success: false, error: err.message };
    }
  }
};
module.exports = plugin;
