/// <reference path="../../types/mcphe.d.ts" />

// I have not tested this but it looks like it should work.

/**
 * Match an index name against one of the operator's glob patterns.
 * Only '*' is special; everything else is escaped.
 */
function elastic7GlobMatch(pattern: string, value: string): boolean {
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
function elastic7IndexGuard(index: unknown, cfg: Record<string, any>): string | null {
  const name = typeof index === "string" ? index : "";
  if (!name) {
    return "index is required";
  }

  const configured = cfg && Array.isArray(cfg.allowed_index_patterns)
    ? (cfg.allowed_index_patterns as unknown[]).map(function (p) { return String(p); })
    : [];

  if (configured.length) {
    for (let i = 0; i < configured.length; i++) {
      if (elastic7GlobMatch(configured[i], name)) {
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
  name:        "elastic7",
  description: "Fetch data from Elastic7",
  version: "1.1.1",
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
    // M1: host.process.env THROWS for keys outside allowed_env_vars rather than
    // returning undefined, so `env(k) || cfg.y` never reached the fallback and
    // a config-only setup failed inside the enclosing catch.
    function envOr(key: string, fallback: string): string {
      try { return host.process.env(key) || fallback; } catch (_e) { return fallback; }
    }

    const cfg     = host.server.config();
    const baseUrl = envOr("ELASTIC_BASE_URL", cfg.base_url as string);
    const apiKey  = envOr("ELASTIC_API_KEY", cfg.api_key as string);

    if (!baseUrl || !apiKey) {
      throw new Error("ELASTIC_BASE_URL and ELASTIC_API_KEY must be set in plugin config or environment variables");
    }

    host.server.logger(3, "elastic7: fetching from index=" + params.index);

    // M14: the index is agent-chosen and there was no allow-list. encodeURIComponent
    // correctly blocks '/', '?', '#' and '&', but it does not escape '*', so
    // {index:"*"} reached every index the credential can see -- including
    // security/system indices such as .opendistro_security*. Checked before the
    // request is built.
    const indexProblem = elastic7IndexGuard(params.index, cfg as Record<string, any>);
    if (indexProblem) {
      throw new Error(indexProblem);
    }

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
