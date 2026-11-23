/// <reference path="../../types/mcphe.d.ts" />

// This use Opensearch Dashboard's proxy for queries
// GET https://<domain>:5601/api/console/proxy?path=%2Fapm%2F_search&method=POST

// L1: a goja-surfaced Go error may not be an Error, so `err.message` is
// undefined and the model receives {error: undefined}. Fall back to String().
function opensearchErrText(err: unknown): string {
  const e = err as { message?: unknown } | null | undefined;
  if (e && typeof e.message === "string" && e.message) return e.message;
  return String(err);
}

/** Cap a string for logging so an upstream error page cannot flood the log. */
function opensearchTruncate(value: unknown, max: number): string {
  const s = String(value === undefined || value === null ? "" : value);
  return s.length > max ? s.slice(0, max) + "… (truncated)" : s;
}

/** L7: clamp a numeric parameter instead of `|| default`, which ate 0. */
function opensearchBoundedInt(raw: unknown, dflt: number, min: number, max: number): number {
  const n = typeof raw === "number" ? raw : parseInt(String(raw === undefined || raw === null ? "" : raw), 10);
  if (!isFinite(n)) return dflt;
  const v = Math.floor(n);
  if (v < min) return min;
  if (v > max) return max;
  return v;
}

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
  version: "1.1.3",
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

    // Build size and from into the URL.
    // L7: `params.size || 10` silently turned an explicit {size:0} into 10 and
    // forwarded an arbitrarily large size unclamped. Bound both.
    const size = opensearchBoundedInt(params.size, 10, 1, 1000);
    const from = opensearchBoundedInt(params.from, 0, 0, 100000);

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
        // L8: the raw upstream body was returned to the model. OpenSearch
        // error bodies carry index names, mappings and stack traces -- internal
        // topology on a path the model can retry at will. Keep it server-side.
        host.server.logger(4, `opensearch: upstream HTTP ${resp.status}: ${opensearchTruncate(resp.body, 512)}`);
        return { success: false, error: "HTTP " + resp.status, status: resp.status };
      }

      try {
        return { success: true, data: JSON.parse(resp.body) };
      } catch (parseErr) {
        return {
          success: false,
          error: "OpenSearch returned a non-JSON body: " + opensearchErrText(parseErr),
          status: resp.status,
        };
      }
    } catch (err) {
      return { success: false, error: opensearchErrText(err) };
    }
  },
};

module.exports = plugin;
