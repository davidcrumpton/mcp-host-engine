"use strict";
function opensearchErrText(err) {
  const e = err;
  if (e && typeof e.message === "string" && e.message) return e.message;
  return String(err);
}
function opensearchTruncate(value, max) {
  const s = String(value === void 0 || value === null ? "" : value);
  return s.length > max ? s.slice(0, max) + "\u2026 (truncated)" : s;
}
function opensearchBoundedInt(raw, dflt, min, max) {
  const n = typeof raw === "number" ? raw : parseInt(String(raw === void 0 || raw === null ? "" : raw), 10);
  if (!isFinite(n)) return dflt;
  const v = Math.floor(n);
  if (v < min) return min;
  if (v > max) return max;
  return v;
}
function opensearchGlobMatch(pattern, value) {
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
function opensearchIndexGuard(index, cfg) {
  const name = typeof index === "string" ? index : "";
  if (!name) {
    return "index is required";
  }
  const configured = cfg && Array.isArray(cfg.allowed_index_patterns) ? cfg.allowed_index_patterns.map(function(p) {
    return String(p);
  }) : [];
  if (configured.length) {
    for (let i = 0; i < configured.length; i++) {
      if (opensearchGlobMatch(configured[i], name)) {
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
  name: "opensearch",
  description: "Fetch data from OpenSearch Dashboard/OpenSearch using query string syntax.",
  version: "1.1.3",
  commit: "none",
  Tags: ["search", "opensearch"],
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
    const baseUrl = envOr("OPENSEARCH_BASE_URL", cfg.opensearch_base_url);
    const username = envOr("OPENSEARCH_USERNAME", cfg.opensearch_username);
    const password = envOr("OPENSEARCH_PASSWORD", cfg.opensearch_password);
    if (!baseUrl) {
      throw new Error("opensearch_base_url must be set in plugin config");
    }
    if (username && !password || !username && password) {
      throw new Error(
        "opensearch_username and opensearch_password must both be set, or neither. Only one is configured, so the request would be sent unauthenticated."
      );
    }
    const headers = {
      "Content-Type": "application/json",
      "kbn-xsrf": "true",
      "osd-xsrf": "true"
    };
    if (username && password) {
      headers["Authorization"] = "Basic " + host.utils.btoa(username + ":" + password);
    }
    const size = opensearchBoundedInt(params.size, 10, 1, 1e3);
    const from = opensearchBoundedInt(params.from, 0, 0, 1e5);
    const indexProblem = opensearchIndexGuard(params.indexName, cfg);
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
        host.server.logger(4, `opensearch: upstream HTTP ${resp.status}: ${opensearchTruncate(resp.body, 512)}`);
        return { success: false, error: "HTTP " + resp.status, status: resp.status };
      }
      try {
        return { success: true, data: JSON.parse(resp.body) };
      } catch (parseErr) {
        return {
          success: false,
          error: "OpenSearch returned a non-JSON body: " + opensearchErrText(parseErr),
          status: resp.status
        };
      }
    } catch (err) {
      return { success: false, error: opensearchErrText(err) };
    }
  }
};
module.exports = plugin;
