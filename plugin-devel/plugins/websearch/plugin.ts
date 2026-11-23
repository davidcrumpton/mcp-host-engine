/// <reference path="../../types/mcphe.d.ts" />

// MCP websearch plugin
// Search DuckDuckGo
// Requires: host.http.request(method, url, headers?)

const plugin = {
  name: 'websearch',
  description: 'Search DuckDuckGo',
  version: '1.1.2',
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
      },
    },
    required: ['CommandEvent', 'query'],
  },
  call(params: Record<string, any>) {
    try {
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

      const html = res.body;

      const pattern =
        /<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)<\/a>[\s\S]*?(?:result__snippet[^>]*>(.*?)<\/)/g;

      const results = [];
      let match: RegExpExecArray | null;

      while ((match = pattern.exec(html))) {
        if (results.length >= params.max_results) break;

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
          host.server.logger(4, `websearch: could not de-redirect ${href}: ${(err as Error).message}`);
        }

        results.push({
          title,
          url: href,
          snippet,
        });
      }

      return {
        query: params.query,
        count: results.length,
        results,
      };
    } catch (err: any) {
      return {
        error: err.message,
      };
    }
  },
};

module.exports = plugin;
