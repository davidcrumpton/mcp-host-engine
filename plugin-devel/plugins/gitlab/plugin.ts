/// <reference path="../../types/mcphe.d.ts" />

const plugin = {
  name: "gitlab",
  description: "Extended GitLab tools for project management, file operations, and collaboration.",
  version: "1.1.1",
  commit: "none",
  Tags: ["development", "utility", "gitlab"],
  annotations: {
    readOnlyHint:    false,
    destructiveHint: true,
    idempotentHint:  false,
    openWorldHint:   true,
  },
  inputSchema: {
    type: "object",
    properties: {
      CommandEvent: {
        type: "string",
        description: "The command to execute.",
        enum: [
          "create_or_update_file",
          "push_files",
          "search_repositories",
          "create_repository",
          "list_project_files",
          "get_file_contents",
          "create_issue",
          "create_merge_request",
          "fork_repository",
          "create_branch",
          "search_projects",
          "search_issues",
          "search_merge_requests"
        ]
      },
      project_id: {
        type: "string",
        description: "Project ID or URL-encoded path"
      },
      file_path: {
        type: "string",
        description: "Path where to create/update the file"
      },
      content: {
        type: "string",
        description: "Content of the file"
      },
      commit_message: {
        type: "string",
        description: "Commit message"
      },
      branch: {
        type: "string",
        description: "Branch to create/update the file in"
      },
      previous_path: {
        type: "string",
        description: "Path of the file to move/rename"
      },
      files: {
        type: "array",
        description: "Files to push, each with file_path and content",
        items: {
          type: "object",
          properties: {
            file_path: { type: "string" },
            content: { type: "string" }
          },
          required: ["file_path", "content"]
        }
      },
      search: {
        type: "string",
        description: "Search query"
      },
      page: {
        type: "integer",
        description: "Page number for pagination (REQUIRED for search APIs, defaults to 1)",
        minimum: 1,
        default: 1
      },
      per_page: {
        type: "integer",
        description: "Results per page",
        minimum: 1,
        maximum: 100,
        default: 20
      },
      name: {
        type: "string",
        description: "Project name"
      },
      visibility: {
        type: "string",
        description: "Visibility level",
        enum: ["private", "internal", "public"]
      },
      initialize_with_readme: {
        type: "boolean",
        description: "Initialize with README"
      },
      ref: {
        type: "string",
        description: "Branch/tag/commit to get contents from"
      },
      title: {
        type: "string",
        description: "Title for issue or merge request"
      },
      description: {
        type: "string",
        description: "Description for issue or merge request, or project"
      },
      assignee_ids: {
        type: "array",
        description: "User IDs to assign",
        items: { type: "number" }
      },
      labels: {
        type: "array",
        description: "Labels to add",
        items: { type: "string" }
      },
      milestone_id: {
        type: "number",
        description: "Milestone ID"
      },
      source_branch: {
        type: "string",
        description: "Branch containing changes"
      },
      target_branch: {
        type: "string",
        description: "Branch to merge into"
      },
      draft: {
        type: "boolean",
        description: "Create as draft MR"
      },
      allow_collaboration: {
        type: "boolean",
        description: "Allow commits from upstream members"
      },
      namespace: {
        type: "string",
        description: "Namespace to fork to"
      },
      ref: {
        type: "string",
        description: "Source branch/commit for new branch"
      }
    },
    required: ["CommandEvent"]
  },
  call(params: Record<string, unknown>) {
    const self = module.exports; 
    const { CommandEvent } = params;
    const page = params.page ?? 1;
    const perPage = params.per_page ?? 20;

    let apiKey;
    try {
      apiKey = host.process.env("GITLAB_API_KEY") || host.config.options.gitlabApiKey || undefined;
      const baseUrl = host.config.options.gitlabBaseUrl || "https://gitlab.com";
      
      host.server.logger(1, `GitLab extended plugin called with CommandEvent=${CommandEvent}`);
      host.server.logger(1, `GitLab plugin config: apiKey=${apiKey ? '***' : 'MISSING'}, baseUrl=${baseUrl}`);
    } catch (err) {
      host.server.logger(1, `GitLab plugin configuration error: ${err.message}`);
      return {
        success: false,
        error: `GitLab plugin configuration error: ${err.message}`
      };
    }

    const baseUrl = host.config.options.gitlabBaseUrl || "https://gitlab.com";

    host.server.logger(1, `GitLab extended plugin called with CommandEvent=${CommandEvent}`);
    host.server.logger(1, `GitLab plugin config: apiKey=${apiKey}, baseUrl=${baseUrl}`);

    // Check for API key first
    if (!apiKey) {
      const errorMsg = "Missing GitLab API key in host.config.options.gitlabApiKey";
      host.server.logger(1, errorMsg);
      return {
        success: false,
        result: errorMsg
      };
    }

    const token = `Bearer ${apiKey}`;

    switch (CommandEvent) {
      case "create_or_update_file":
        return self.createOrUpdateFile(params, token, baseUrl);
      case "push_files":
        return self.pushFiles(params, token, baseUrl);
      case "search_repositories":
        return self.searchRepositories(params, token, baseUrl);
      case "create_repository":
        return self.createRepository(params, token, baseUrl);
      case "list_project_files":
        return self.listProjectFiles(params, token, baseUrl);
      case "get_file_contents":
        return self.getFileContents(params, token, baseUrl);
      case "create_issue":
        return self.createIssue(params, token, baseUrl);
      case "create_merge_request":
        return self.createMergeRequest(params, token, baseUrl);
      case "fork_repository":
        return self.forkRepository(params, token, baseUrl);
      case "create_branch":
        return self.createBranch(params, token, baseUrl);
      case "search_projects":
        return self.searchProjects(params, token, baseUrl);
      case "search_issues":
        return self.searchIssues(params, token, baseUrl);
      case "search_merge_requests":
        return self.searchMergeRequests(params, token, baseUrl);
      case "update_file":
        // For backward compatibility, treat "update_file" as "create_or_update_file"
        return self.createOrUpdateFile(params, token, baseUrl);
      default:
        const errorMsg = `Unknown CommandEvent: ${CommandEvent}`;
        host.server.logger(1, errorMsg);
        return {
          success: false,
          error: errorMsg
        };
    }
  },

  createOrUpdateFile(params, token, baseUrl) {
    const {
      project_id,
      file_path,
      content,
      commit_message,
      branch,
      previous_path
    } = params;

    const url = `${baseUrl}/api/v4/projects/${encodeURIComponent(project_id)}/repository/files/${encodeURIComponent(file_path)}`;
    
    const body = {
      branch: branch,
      content: content,
      commit_message: commit_message
    };

    if (previous_path) {
      body.previous_path = previous_path;
    }

    try {
      const response = host.httpPut(url, {
        headers: {
          "Authorization": token,
          "Content-Type": "application/json",
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      }, JSON.stringify(body));
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },

  pushFiles(params, token, baseUrl) {
    const {
      project_id,
      branch,
      files,
      commit_message
    } = params;

    const url = `${baseUrl}/api/v4/projects/${encodeURIComponent(project_id)}/repository/commits`;
    
    const body = {
      branch: branch,
      commit_message: commit_message,
      actions: files.map(file => ({
        action: "create",
        file_path: file.file_path,
        content: file.content
      }))
    };

    try {
      const response = host.httpPost(url, {
        headers: {
          "Authorization": token,
          "Content-Type": "application/json",
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      }, JSON.stringify(body));
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },

  searchRepositories(params, token, baseUrl) {
    const {
      search,
      page,
      per_page
    } = params;

    const url = `${baseUrl}/api/v4/projects/search?search=${encodeURIComponent(search)}&page=${page}&per_page=${per_page}`;
    
    try {
      const response = host.http.get(url, {
        headers: {
          "Authorization": token,
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      });
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },

  createRepository(params, token, baseUrl) {
    const {
      name,
      description,
      visibility,
      initialize_with_readme
    } = params;

    const url = `${baseUrl}/api/v4/projects`;
    
    const body = {
      name: name,
      description: description,
      visibility: visibility,
      initialize_with_readme: initialize_with_readme
    };

    try {
      const response = host.httpPost(url, {
        headers: {
          "Authorization": token,
          "Content-Type": "application/json",
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      }, JSON.stringify(body));
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },
  listProjectFiles(params, token, baseUrl) {
    const {
      project_id,
      ref,
      page,
      per_page
    } = params;

    let url = `${baseUrl}/api/v4/projects/${encodeURIComponent(project_id)}/repository/tree?per_page=${per_page}&page=${page}`;
    
    if (ref) {
      url += `&ref=${encodeURIComponent(ref)}`;
    }
    
    try {
      const response = host.http.get(url, {
        headers: {
          "Authorization": token,
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      });
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },
  getFileContents(params, token, baseUrl) {
    const {
      project_id,
      file_path,
      ref
    } = params;

    let url = `${baseUrl}/api/v4/projects/${encodeURIComponent(project_id)}/repository/files/${encodeURIComponent(file_path)}`;
    
    if (ref) {
      url += `?ref=${encodeURIComponent(ref)}`;
    }
    
    try {
      const response = host.http.get(url, {
        headers: {
          "Authorization": token,
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      });
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },

  createIssue(params, token, baseUrl) {
    const {
      project_id,
      title,
      description,
      assignee_ids,
      labels,
      milestone_id
    } = params;

    const url = `${baseUrl}/api/v4/projects/${encodeURIComponent(project_id)}/issues`;
    
    const body = {
      title: title,
      description: description,
      assignee_ids: assignee_ids,
      labels: labels,
      milestone_id: milestone_id
    };

    try {
      const response = host.httpPost(url, {
        headers: {
          "Authorization": token,
          "Content-Type": "application/json",
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      }, JSON.stringify(body));
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },

  createMergeRequest(params, token, baseUrl) {
    const {
      project_id,
      title,
      description,
      source_branch,
      target_branch,
      draft,
      allow_collaboration
    } = params;

    const url = `${baseUrl}/api/v4/projects/${encodeURIComponent(project_id)}/merge_requests`;
    
    const body = {
      title: title,
      description: description,
      source_branch: source_branch,
      target_branch: target_branch,
      draft: draft,
      allow_collaboration: allow_collaboration
    };

    try {
      const response = host.httpPost(url, {
        headers: {
          "Authorization": token,
          "Content-Type": "application/json",
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      }, JSON.stringify(body));
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },

  forkRepository(params, token, baseUrl) {
    const {
      project_id,
      namespace
    } = params;

    const url = `${baseUrl}/api/v4/projects/${encodeURIComponent(project_id)}/fork`;
    
    const body = namespace ? { namespace: namespace } : {};

    try {
      const response = host.httpPost(url, {
        headers: {
          "Authorization": token,
          "Content-Type": "application/json",
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      }, JSON.stringify(body));
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },

  createBranch(params, token, baseUrl) {
    const {
      project_id,
      branch,
      ref
    } = params;

    const url = `${baseUrl}/api/v4/projects/${encodeURIComponent(project_id)}/repository/branches`;
    
    const body = {
      branch: branch,
      ref: ref
    };

    try {
      const response = host.httpPost(url, {
        headers: {
          "Authorization": token,
          "Content-Type": "application/json",
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      }, JSON.stringify(body));
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },
  searchProjects(params, token, baseUrl) {
    const {
      search = '',
      page = 1,
      per_page = 20
    } = params;

    if (!search || search.trim() === '') {
     return { success: false, error: "Search query cannot be empty" };
    }
    // Proper URL: https://gitlab.crumpton.org/api/v4/search?scope=projects&search=mcp&page=1&per_page=20
    const encodedQuery = encodeURIComponent(search);
    const url =
      `${baseUrl}/api/v4/search` +
      `?scope=projects` +
      `&search=${encodedQuery}` +
      `&page=${encodeURIComponent(page)}` +
      `&per_page=${encodeURIComponent(per_page)}`;
    
    try {
      const response = host.http.get(url, {
        headers: {
          "Authorization": token,
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      });
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },
  searchIssues(params, token, baseUrl) {
    const {
      search = '',
      page = 1,
      per_page = 20
    } = params;

    if (!search || search.trim() === '') {
      return { success: false, error: "Search query cannot be empty" };
    }
    const encodedQuery = encodeURIComponent(search);
    const url =
      `${baseUrl}/api/v4/search` +
      `?scope=issues` +
      `&search=${encodedQuery}` +
      `&page=${encodeURIComponent(page)}` +
      `&per_page=${encodeURIComponent(per_page)}`;
    
    try {
      const response = host.http.get(url, {
        headers: {
          "Authorization": token,
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      });
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
        };
      }
    } catch (err) {
      return {
        success: false,
        error: `GitLab API request error: ${err.message}`
      };
    }
  },
  searchMergeRequests(params, token, baseUrl) {
    const {
      search = '',
      page = 1,
      per_page = 20
    } = params;

    if (!search || search.trim() === '') {
      return { success: false, error: "Search query cannot be empty" };
    }
    const encodedQuery = encodeURIComponent(search);
    const url =
      `${baseUrl}/api/v4/search` +
      `?scope=merge_requests` +
      `&search=${encodedQuery}` +
      `&page=${encodeURIComponent(page)}` +
      `&per_page=${encodeURIComponent(per_page)}`;
    
    try {
      const response = host.http.get(url, {
        headers: {
          "Authorization": token,
          "User-Agent": "mcphe-gitlab-extended-plugin/1.0 (node.js)"
        }
      });
      
      const status = response.status ?? response.statusCode;
      const bodyText = typeof response.body === "string" ? response.body : response.text?.() ?? "";

      if (status >= 200 && status < 300) {
        const payload = JSON.parse(bodyText);
        return {
          success: true,
          result: payload
        };
      } else {
        return {
          success: false,
          error: `GitLab API request failed with status ${status}: ${bodyText}`
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

module.exports = plugin;
