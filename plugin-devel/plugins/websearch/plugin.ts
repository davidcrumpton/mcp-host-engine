/// <reference path="../../types/mcphe.d.ts" />

// MCP websearch plugin
// Search DuckDuckGo
// Requires: host.http.request({url, method, headers})

// L16: the result regex uses a lazy `[\s\S]*?` between anchors, so each match
// rescans forward and the scan is roughly quadratic in page length. The host
// will hand us a body up to 32 MiB, so cap before matching. DDG's HTML results
// page is well under 1 MiB; anything larger is not a results page.
const MAX_HTML = 1 << 20; // 1 MiB
const MAX_RESULTS = 50;
const DEFAULT_RESULTS = 5;

// L3: `params.max_results` reached the loop unvalidated, so an omitted or
// non-numeric value made the bound NaN (comparison always false -> loop ran to
// the end of the page) and 0/negative returned count:0. Clamp instead.
function clampMaxResults(raw: unknown): number {
  const n = typeof raw === 'number' ? raw : parseInt(String(raw === undefined || raw === null ? '' : raw), 10);
  if (!isFinite(n) || n < 1) return DEFAULT_RESULTS;
  return n > MAX_RESULTS ? MAX_RESULTS : Math.floor(n);
}

// L1: a goja-surfaced Go error may not be an Error, so `err.message` is
// undefined and the model receives {error: undefined}. Fall back to String().
function errText(err: unknown): string {
  const e = err as { message?: unknown } | null | undefined;
  if (e && typeof e.message === 'string' && e.message) return e.message;
  return String(err);
}

const plugin = {
  name: 'websearch',
  description: 'Search DuckDuckGo',
  version: '1.1.3',
  commit: 'none',
  tags: ['websearch', 'duckduckgo'],
  annotations: {
    readOnlyHint: false,
    destructiveHint: false,
    idempotentHint: false,
    openWorldHint: true,
  },
  inputSchema: {
    type: 'object',
    properties: {
      CommandEvent: {
        type: 'string',
        description: 'The command to execute.',
        enum: ['websearch'],
      },
      query: {
        type: 'string',
        description: 'The search query.',
      },
      max_results: {
        type: 'integer',
        description: 'The maximum number of results to return.',
        default: 5,
        minimum: 1,
        maximum: 50,
      },
    },
    required: ['CommandEvent', 'query'],
  },
  call(params: Record<string, any>) {
    try {
      const maxResults = clampMaxResults(params.max_results);
      const encoded = encodeURIComponent(params.query);

      const res = host.http.request(
        {
          url: `https://html.duckduckgo.com/html/?q=${encoded}`,
          method: "GET",
          headers: {
            'User-Agent': 'mcphe/1.0 (+duckduckgo-search)',
          },
        }
      );

      // L3: the status was never checked, so a 429 or a bot-challenge page
      // parsed to zero results and was reported as a genuine empty search.
      // Distinguish the two.
      if (res.status < 200 || res.status >= 300) {
        return {
          success: false,
          error: `DuckDuckGo returned HTTP ${res.status}`,
          status: res.status,
          query: params.query,
        };
      }

      const html = String(res.body === undefined || res.body === null ? '' : res.body).slice(0, MAX_HTML);

      const pattern =
        /<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)<\/a>[\s\S]*?(?:result__snippet[^>]*>(.*?)<\/)/g;

      const results = [];
      let match: RegExpExecArray | null;

      while ((match = pattern.exec(html))) {
        if (results.length >= maxResults) break;

        let [, href, title, snippet] = match;

        title = title.replace(/<[^>]+>/g, '').trim();
        snippet = snippet.replace(/<[^>]+>/g, '').trim();

        // M13: `new URL()` does not exist in goja, so this threw a
        // ReferenceError on every iteration and the catch below swallowed it --
        // results[].url was always the raw
        // //duckduckgo.com/l/?uddg=... wrapper instead of the target page.
        // Silent, permanent wrong output. Use the injected host.url.
        try {
          const parsed = host.url.parse(href, 'https://duckduckgo.com');
          const uddg = parsed.searchParams.get('uddg');
          if (uddg) href = decodeURIComponent(uddg);
        } catch (err) {
          // Not every href is a redirect wrapper; leave it as-is but say so.
          host.server.logger(4, `websearch: could not de-redirect ${href}: ${errText(err)}`);
        }

        results.push({
          title,
          url: href,
          snippet,
        });
      }

      return {
        success: true,
        query: params.query,
        count: results.length,
        results,
      };
    } catch (err: unknown) {
      return {
        success: false,
        error: errText(err),
      };
    }
  },
};

module.exports = plugin;
