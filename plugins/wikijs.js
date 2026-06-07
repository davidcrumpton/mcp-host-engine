module.exports = {
  name: "wikijs",
  description: "WikiJS MCP tools for page management, search, and content operations using GraphQL API.",
  version: "1.0.0",
  Tags: ["wiki", "documentation", "content-management", "graphql"],
  annotations: {
    readOnlyHint: false,
    destructiveHint: true, // Some operations (e.g., delete) are destructive
    idempotentHint: false,
    openWorldHint: true
  },

  inputSchema: {
    type: "object",
    properties: {
      CommandEvent: {
        type: "string",
        description: "WikiJS GraphQL command to execute.",
        enum: [
          "search_pages",
          "get_page",
          "create_page",
          "update_page",
          "delete_page",
          "get_page_history",
          "restore_page"
        ]
      },
      params: {
        type: "object",
        description: "Additional parameters for the selected CommandEvent.",
        properties: {
          query: { type: "string", description: "Search query for pages." },
          pageId: { type: "number", description: "ID of the page to fetch/update/delete." },
          title: { type: "string", description: "Title of the page to create/update." },
          path: { type: "string", description: "Path of the page (e.g., 'my-page')." },
          content: { type: "string", description: "Markdown content for page creation/update." },
          locale: { type: "string", description: "Locale of the page (e.g., 'en')." },
          reason: { type: "string", description: "Reason for deletion/restoration." },
          revisionId: { type: "number", description: "Revision ID to restore." }
        }
      }
    },
    required: ["CommandEvent"]
  },

  call(params) {
    const { CommandEvent } = params;
    let apiUrl;
    let apiToken;

    // Load config
    try {
      apiUrl = host.process.env("WIKIJS_API_URL") || host.config.options.ApiUrl;
      apiToken = host.process.env("WIKIJS_API_TOKEN") || host.config.options.ApiToken;
    } catch (e) {
      return { success: false, error: "Failed to load WikiJS configuration." };
    }

    // Helper function for GraphQL requests
    const graphqlRequest = (query, variables = {}) => {
      if (!apiToken) {
        return { success: false, error: "WikiJS API token is required." };
      }

      const headers = {
        "Content-Type": "application/json",
        Authorization: `Bearer ${apiToken}`
      };

      try {
        const response = host.http.rawPost(`${apiUrl}/graphql`, headers, JSON.stringify({ query, variables }));
        return response;
      } catch (error) {
        return { success: false, error: error.message };
      }
    };

    // Debug log
    host.logger(4, `CommandEvent: ${CommandEvent}`);
    host.logger(4, `Params: ${JSON.stringify(params.params)}`);
    // Command handling
    switch (CommandEvent) {
      // Search for pages
        case "search_pages": {
        const { query } = params.params || {};
        const graphqlQuery = `
            query SearchPages($query: String!) {
            pages {
                search(query: $query) {
                results {
                    id
                    path
                    title
                    locale
                }
                suggestions
                totalHits
                }
            }
            }
        `;
        host.logger(1, `GraphQL Query: ${graphqlQuery}`);
        host.logger(1, `GraphQL Variables: ${JSON.stringify({ query })}`);
        return graphqlRequest(graphqlQuery, { query });
        }

      // Get a specific page
      case "get_page": {
        const { pageId } = params.params || {};
        const graphqlQuery = `
          query GetPage($id: Int!) {
            pages {
              single(id: $id) {
                id
                path
                title
                content
                createdAt
                updatedAt
              }
            }
          }
        `;
        return graphqlRequest(graphqlQuery, { id: pageId });
      }

      // Create a new page
      case "create_page": {
        const { title, path, content, locale = "en" } = params.params || {};
        const graphqlQuery = `
          mutation CreatePage($title: String!, $path: String!, $content: String!, $locale: String!) {
            pages {
              create(
                title: $title
                path: $path
                content: $content
                locale: $locale
              ) {
                responseResult {
                  succeeded
                  slug
                  message
                }
                page {
                  id
                  path
                  title
                }
              }
            }
          }
        `;
        return graphqlRequest(graphqlQuery, { title, path, content, locale });
      }

      // Update an existing page
      case "update_page": {
        const { pageId, title, path, content, locale = "en" } = params.params || {};
        const graphqlQuery = `
          mutation UpdatePage($id: Int!, $title: String!, $path: String!, $content: String!, $locale: String!) {
            pages {
              update(
                id: $id
                title: $title
                path: $path
                content: $content
                locale: $locale
              ) {
                responseResult {
                  succeeded
                  slug
                  message
                }
                page {
                  id
                  path
                  title
                }
              }
            }
          }
        `;
        return graphqlRequest(graphqlQuery, { id: pageId, title, path, content, locale });
      }

      // Delete a page
      case "delete_page": {
        const { pageId, reason } = params.params || {};
        const graphqlQuery = `
          mutation DeletePage($id: Int!, $reason: String) {
            pages {
              delete(id: $id, reason: $reason) {
                responseResult {
                  succeeded
                  slug
                  message
                }
              }
            }
          }
        `;
        return graphqlRequest(graphqlQuery, { id: pageId, reason });
      }

      // Get page history
      case "get_page_history": {
        const { pageId } = params.params || {};
        const graphqlQuery = `
          query GetPageHistory($id: Int!) {
            pages {
              single(id: $id) {
                history {
                  id
                  createdAt
                  updatedAt
                  editor {
                    id
                    name
                  }
                }
              }
            }
          }
        `;
        return graphqlRequest(graphqlQuery, { id: pageId });
      }

      // Restore a page revision
      case "restore_page": {
        const { pageId, revisionId, reason } = params.params || {};
        const graphqlQuery = `
          mutation RestorePage($id: Int!, $revisionId: Int!, $reason: String) {
            pages {
              restore(id: $id, revisionId: $revisionId, reason: $reason) {
                responseResult {
                  succeeded
                  slug
                  message
                }
              }
            }
          }
        `;
        return graphqlRequest(graphqlQuery, { id: pageId, revisionId, reason });
      }

      // Unknown command
      default: {
        return { success: false, error: `Unknown CommandEvent: ${CommandEvent}` };
      }
    }
  }
};