/// <reference path="../../types/mcphe.d.ts" />

// This use Opensearch Dashboard's proxy for queries
// GET https://<domain>:5601/api/console/proxy?path=%2Fapm%2F_search&method=POST

/**
 * Match an index name against one of the operator's glob patterns.
 * Only '*' is special; everything else is escaped.
 */
function opensearchGlobMatch(pattern: string, value: string): boolean {
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

/**
 * M14: validate an agent-chosen index name before it reaches the request.
 *
 * Returns an error string, or null when the index is acceptable.
 *
 * If `allowed_index_patterns` is configured it is authoritative: the index must
 * match one of the globs. With no allow-list, wildcards are refused unless
 * `allow_wildcard_index: true`, because '*' expands to every index the
 * credential can see -- which is how a search tool becomes a dump-everything
 * tool, security indices included.
 */
function opensearchIndexGuard(index: unknown, cfg: Record<string, any>): string | null {
  const name = typeof index === "string" ? index : "";
  if (!name) {
    return "index is required";
  }

  const configured = cfg && Array.isArray(cfg.allowed_index_patterns)
    ? (cfg.allowed_index_patterns as unknown[]).map(function (p) { return String(p); })
    : [];

  if (configured.length) {
    for (let i = 0; i < configured.length; i++) {
      if (opensearchGlobMatch(configured[i], name)) {
        return null;
      }
    }
    return `index ${JSON.stringify(name)} is not permitted by allowed_index_patterns ` +
           `(${configured.join(", ")}).`;
  }

  if (name.indexOf("*") !== -1 && cfg.allow_wildcard_index !== true) {
    return `index ${JSON.stringify(name)} contains '*', which expands to every index the ` +
           `credential can see, security indices included. Set allowed_index_patterns ` +
           `(or allow_wildcard_index: true) in the plugin config to permit it.`;
  }

  return null;
}

const plugin = {
  name:        "opensearch",
  description: "Fetch data from OpenSearch Dashboard/OpenSearch using query string syntax.",
  version: "1.1.2",
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
    // M1: host.process.env THROWS for keys outside allowed_env_vars rather than
    // returning undefined, so `env(k) || cfg.y` never reached the fallback and
    // a config-only setup failed inside the enclosing catch.
    function envOr(key: string, fallback: string): string {
      try { return host.process.env(key) || fallback; } catch (_e) { return fallback; }
    }

    const cfg     = host.config;

    // take from ENV first then cfg
    const baseUrl = envOr("OPENSEARCH_BASE_URL", cfg.opensearch_base_url as string);
    const username = envOr("OPENSEARCH_USERNAME", cfg.opensearch_username as string);
    const password = envOr("OPENSEARCH_PASSWORD", cfg.opensearch_password as string);

    if (!baseUrl) {
      throw new Error("opensearch_base_url must be set in plugin config");
    }

    // M3: require both or neither. Previously a half-configured credential
    // silently sent the request with NO Authorization header at all, and the
    // model received whatever an anonymous caller gets -- no error, no warning.
    if ((username && !password) || (!username && password)) {
      throw new Error(
        "opensearch_username and opensearch_password must both be set, or neither. " +
        "Only one is configured, so the request would be sent unauthenticated."
      );
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

    // M14: indexName is agent-chosen with no allow-list. encodeURIComponent does
    // not escape '*', so {indexName:"*"} reached every index the credential can
    // see, security indices included. Checked before the request is built.
    const indexProblem = opensearchIndexGuard(params.indexName, cfg as Record<string, any>);
    if (indexProblem) {
      throw new Error(indexProblem);
    }

    const url = baseUrl.replace(/\/$/, "")
      + "/api/console/proxy?path=" + encodeURIComponent("/" + params.indexName + "/_search?size=" + size + "&from=" + from) + "&method=POST";

    try {
      const resp = host.http.request({
        method: 'POST',
        url: url,
        headers: headers,
        body: params.query
      });

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
