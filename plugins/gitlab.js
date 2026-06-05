module.exports = {
  name: "gitlab_search",
  description: "GitLab search tools for projects, issues, and merge requests.  Returns ID numbers to be used with the gitlab_extended plugin for more detailed operations.",
  version: "1.0.0",
  commit: "none",
  Tags: ["development", "utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  false,
    openWorldHint:   true,
  },
  inputSchema: {
    type: "object",
    properties: {
      CommandEvent: {
        type: "string",
        description: "The command to execute.",
        enum: ["searchProjects", "searchIssues", "searchMergeRequests"]
      },
      query: {
        type: "string",
        description: "Search query."
      },
      page: {
        type: "integer",
        description: "Page number for pagination.",
        minimum: 1,
        default: 1
      },
      perPage: {
        type: "integer",
        description: "Results per page.",
        minimum: 1,
        maximum: 100,
        default: 20
      }
    },
    required: ["CommandEvent", "query"]
  },
  call(params) {
    const { CommandEvent, query } = params;
    const page = params.page ?? 1;
    const perPage = params.perPage ?? 20;

    const encodedQuery = encodeURIComponent(query);

    let apiKey;
    try {
      apiKey = host.getEnv("GITLAB_API_KEY") || host.config.options.gitlabApiKey || undefined;
      const baseUrl = host.config.options.gitlabBaseUrl || "https://gitlab.com";
      
      host.logger(1, `GitLab plugin called with CommandEvent=${CommandEvent}, query="${query}", page=${page}, perPage=${perPage}`);
      host.logger(1, `GitLab plugin config: apiKey=${apiKey ? '***' : 'MISSING'}, baseUrl=${baseUrl}`);
    } catch (err) {
      host.logger(1, `GitLab plugin configuration error: ${err.message}`);
      return {
        success: false,
        error: `GitLab plugin configuration error: ${err.message}`
      };
    }
    const baseUrl = host.config.options.gitlabBaseUrl || "https://gitlab.com";

    host.logger(1, `GitLab plugin called with CommandEvent=${CommandEvent}, query="${query}", page=${page}, perPage=${perPage}`);
    host.logger(1, `GitLab plugin config: apiKey=${apiKey}, baseUrl=${baseUrl}`);

    // Check for API key first
    if (!apiKey) {
      const errorMsg = "Missing GitLab API key in host.config.options.gitlabApiKey";
      host.logger(1, errorMsg);
      return {
        success: false,
        result: errorMsg
      };
    }

    const token = `Bearer ${apiKey}`;

        let scope;
    switch (CommandEvent) {
      case "searchProjects":
        scope = "projects";
        break;
      case "searchIssues":
        scope = "issues";
        break;
      case "searchMergeRequests":
        scope = "merge_requests";
        break;
      default:
        const errorMsg = `Unknown CommandEvent: ${CommandEvent}`;
        host.logger(1, errorMsg);
        return {
          success: false,
          error: errorMsg
        };
    }

    const url =
      `${baseUrl}/api/v4/search` +
      `?scope=${encodeURIComponent(scope)}` +
      `&search=${encodedQuery}` +
      `&page=${encodeURIComponent(page)}` +
      `&per_page=${encodeURIComponent(perPage)}`;

    host.logger(1, `Making request to: ${url}`);

    try {
      const response = host.httpGet(url, {
        headers: {
          "Authorization": token,
          "User-Agent": "mcphe-gitlab-plugin/1.0 (node.js)"
        }
      });
      
      const status = response.status ?? response.statusCode;
      const body = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(body);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${body}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  }
};