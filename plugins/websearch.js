// MCP websearch plugin
// Search DuckDuckGo    
// Requires: host.httpGet(url)

module.exports = {
  name: 'websearch',
  description: 'Search DuckDuckGo',
  version: '1.0.0',
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
        enum: ['websearch']
      },
      query: {
        type: 'string',
        description: 'The search query.'
      },
      max_results: {    
        type: 'integer',
        description: 'The maximum number of results to return.',
        default: 5
      }
    },
    required: ['CommandEvent', 'query']
  },
    call(params) {
    try {
      const encoded = encodeURIComponent(params.query)

      const res = host.httpGet(
        `https://html.duckduckgo.com/html/?q=${encoded}`,
        {
          headers: {
            'User-Agent':
              'mcphe/1.0 (+duckduckgo-search)'
          }
        }
      )

      const html = res.body;

      const pattern =
        /<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)<\/a>[\s\S]*?(?:result__snippet[^>]*>(.*?)<\/)/g

      const results = []
      let match

      while ((match = pattern.exec(html))) {
        if (results.length >= params.max_results) break

        let [, href, title, snippet] = match

        title = title.replace(/<[^>]+>/g, '').trim()
        snippet = snippet.replace(/<[^>]+>/g, '').trim()

        try {
          const u = new URL(href, 'https://duckduckgo.com')
          const uddg = u.searchParams.get('uddg')
          if (uddg) href = decodeURIComponent(uddg)
        } catch {}

        results.push({
          title,
          url: href,
          snippet
        })
      }

      return {
        query: params.query,
        count: results.length,
        results
      }
    } catch (err) {
      return {
        error: err.message
      }
    }
  }
}
