module.exports = {
  name: "gitea",
  description: "Extended Gitea tools for project management, file operations, and collaboration.",
  version: "1.0.4",
  commit: "none",
  Tags: ["development", "utility", "gitea"],
  annotations: {
    readOnlyHint:    false,
    destructiveHint: true,
    idempotentHint:  false,
    openWorldHint:   true
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
      description: {
        type: "string",
        description: "Project description"
      },
      visibility: {
        type: "string",
        description: "Visibility level. Defaults to private. Gitea's instance-wide \"internal\" level is not supported: the API call would otherwise create a public repository.",
        enum: ["private", "public"]
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
    },
    required: ["CommandEvent"]
  },

  call: function (params: Record<string, unknown>): Record<string, unknown> {
    const self = module.exports;
    const { CommandEvent } = params;

    const opts = (host.config.options || {}) as Record<string, any>;

    // host.process.env THROWS for a key outside allowed_env_vars rather than
    // returning undefined, so treat a throw as "not configured via env" and
    // fall through to the config value.
    let envKey = "";
    let envUrl = "";
    try { envKey = host.process.env("GITEA_API_KEY") || ""; } catch (envErr) { envKey = ""; }
    try { envUrl = host.process.env("GITEA_API_URL") || ""; } catch (envErr) { envUrl = ""; }

    const giteaApiKey = envKey || opts.giteaApiKey;
    const giteaApiUrl = envUrl || opts.giteaApiUrl || "https://try.gitea.io";
    if (!giteaApiKey) {
      return {
        success: false,
        error: "GITEA_API_KEY or Plugin 'options.giteaApiKey' is required"
      };
    }
    const apiToken = `Bearer ${giteaApiKey}`;
    const headers = {
      'Authorization': apiToken,
      'Content-Type': 'application/json'
    };
    switch (CommandEvent) {
      case "create_or_update_file":
        return self.createOrUpdateFile(params, headers, giteaApiUrl);
      case "push_files":
        return self.pushFiles(params, headers, giteaApiUrl);
      case "search_repositories":
        return self.searchRepositories(params, headers, giteaApiUrl);
      case "create_repository":
        return self.createRepository(params, headers, giteaApiUrl);
      case "list_project_files":
        return self.listProjectFiles(params, headers, giteaApiUrl);
      case "get_file_contents":
        return self.getFileContents(params, headers, giteaApiUrl);
      case "create_issue":
        return self.createIssue(params, headers, giteaApiUrl);
      case "create_merge_request":
        return self.createMergeRequest(params, headers, giteaApiUrl);
      case "fork_repository":
        return self.forkRepository(params, headers, giteaApiUrl);
      case "create_branch":
        return self.createBranch(params, headers, giteaApiUrl);
      case "search_projects":
        return self.searchProjects(params, headers, giteaApiUrl);
      case "search_issues":
        return self.searchIssues(params, headers, giteaApiUrl);
      case "search_merge_requests":
        return self.searchMergeRequests(params, headers, giteaApiUrl);
      default:
        return {
          success: false,
          error: `Unknown CommandEvent: ${CommandEvent}`
        };
    }
  },
  createOrUpdateFile: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { project_id, file_path, content, commit_message, branch } = params;
    let apiUrl = `${baseUrl}/api/v1/repos/${project_id}/contents/${file_path}`;
    if (branch) {
      apiUrl += `?branch=${branch}`;
    }
    const fileResponse = host.http.request({
      method: 'GET',
      url: apiUrl,
      headers: headers
    })
    let sha = null;
    // Only a 200 means the file already exists. A 404 here is the normal
    // "create" path; parsing its error body would throw or set a bogus sha.
    if (fileResponse.status === 200 && fileResponse.body) {
      try {
        const fileData = JSON.parse(fileResponse.body);
        sha = fileData.sha || null;
      } catch (e) {
        sha = null;
      }
    }
    const body = {
      message: commit_message || `Update ${file_path}`,
      content: content,
      branch: branch || "main",
      sha: sha
    };
    const response = host.http.request({
      method: 'PUT',
      url: apiUrl,
      headers: headers,
      body: JSON.stringify(body)
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `File ${file_path} ${sha ? 'updated' : 'created'} successfully`,
      data: data
    };
  },
  pushFiles:  function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { project_id, files, commit_message, branch } = params;
    const apiUrl = `${baseUrl}/api/v1/repos/${project_id}/contents/files`;
    const body = {
      message: commit_message as string || "Push files",
      files: files as Record<string, string>,
      branch: branch as string || "main"
    };
    const response = host.http.request({
      method: 'PUT',
      url: apiUrl,
      headers: headers,
      body: JSON.stringify(body)
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    // `files` is a Record<path, content> per the declared type, so it has no
    // .length; count keys instead of reporting "Pushed undefined files".
    const fileCount = files && typeof files === "object" ? Object.keys(files).length : 0;
    return {
      success: true,
      result: `Pushed ${fileCount} files successfully`,
      data: data
    };
  },
  searchRepositories: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { search, page = 1, per_page = 20 } = params;
    const apiUrl = `${baseUrl}/api/v1/repos/search?q=${encodeURIComponent(search as string)}&page=${page}&limit=${per_page}`;
    const response = host.http.request({
      method: 'GET',
      url: apiUrl,
      headers: headers
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Found ${data.total} repositories`,
      data: data.data
    };
  },
  createRepository:  function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { name, description, visibility, initialize_with_readme } = params;
    const apiUrl = `${baseUrl}/api/v1/user/repos`;

    // M5: `private: visibility === 'private'` made every unrecognised or omitted
    // value public. Gitea's "internal" means "any logged-in user on this
    // instance", which is not something this plugin can express as a boolean,
    // so it is refused rather than silently downgraded to public. Anything that
    // is not an explicit "public" is created private.
    if (visibility !== undefined && visibility !== "private" && visibility !== "public") {
      return {
        success: false,
        error: `visibility ${JSON.stringify(visibility)} is not supported; use "private" or "public". ` +
               `Gitea's "internal" visibility is instance-wide and is not exposed here, ` +
               `because the request would otherwise be sent as a public repository.`
      };
    }

    const body = {
      name: name,
      description: description,
      private: visibility !== "public",
      gitignores: "Node",
      license: "MIT",
      readme: initialize_with_readme ? "default" : ""
    };
    const response = host.http.request({
      method: 'POST',
      url: apiUrl,
      headers: headers,
      body: JSON.stringify(body)
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Repository ${name} created successfully`,
      data: data
    };
  },
  listProjectFiles: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { project_id, branch } = params;
    let apiUrl = `${baseUrl}/api/v1/repos/${project_id}/contents`;
    if (branch) {
      apiUrl += `?ref=${branch}`;
    }
    const response = host.http.request({
      method: 'GET',
      url: apiUrl,
      headers: headers
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Found ${data.length} files`,
      data: data
    };
  },
  getFileContents: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { project_id, file_path, ref } = params;
    let apiUrl = `${baseUrl}/api/v1/repos/${project_id}/contents/${file_path}`;
    if (ref) {
      apiUrl += `?ref=${ref}`;
    }
    const response = host.http.request({
      method: 'GET',
      url: apiUrl,
      headers: headers
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `File contents for ${file_path}`,
      data: data
    };
  },
  createIssue: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { project_id, title, description, assignee_ids, labels } = params;
    const apiUrl = `${baseUrl}/api/v1/repos/${project_id}/issues`;
    const body = {
      title: title,
      content: description,
      assignee: assignee_ids ? assignee_ids : undefined,
      labels: labels
    };
    const response = host.http.request({
      method: 'POST',
      url: apiUrl,
      headers: headers,
      body: JSON.stringify(body)
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Issue ${data.id} created successfully`,
      data: data
    };
  },
  createMergeRequest: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { project_id, title, description, source_branch, target_branch, draft, allow_collaboration } = params;
    const apiUrl = `${baseUrl}/api/v1/repos/${project_id}/pulls`;
    const body = {
      title: title,
      body: description,
      head: source_branch,
      base: target_branch,
      draft: draft,
      allow_maintainer_edit: allow_collaboration
    };
    const response = host.http.request({
      method: 'POST',
      url: apiUrl,
      headers: headers,
      body: JSON.stringify(body)
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Merge request ${data.number} created successfully`,
      data: data
    };
  },
  forkRepository: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { project_id, namespace } = params;
    const apiUrl = `${baseUrl}/api/v1/repos/${project_id}/forks`;
    const body = {
      organization: namespace
    };
    const response = host.http.request({
      method: 'POST',
      url: apiUrl,
      headers: headers,
      body: JSON.stringify(body)
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Repository forked successfully`,
      data: data
    };
  },
  createBranch: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { project_id, ref, branch } = params;
    const apiUrl = `${baseUrl}/api/v1/repos/${project_id}/branches`;
    const body = {
      ref: ref,
      branch_name: branch
    };
    const response = host.http.request({
      method: 'POST',
      url: apiUrl,
      headers: headers,
      body: JSON.stringify(body)
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Branch ${branch} created successfully`,
      data: data
    };
  },
  searchProjects: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { search, page = 1, per_page = 20 } = params;
    const apiUrl = `${baseUrl}/api/v1/repos/search?q=${encodeURIComponent(search as string)}&page=${page}&limit=${per_page}`;
    const response = host.http.request({
      method: 'GET',
      url: apiUrl,
      headers: headers
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Found ${data.total} repositories`,
      data: data.data
    };
  },
  searchIssues: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { search, page = 1, per_page = 20 } = params;
    const apiUrl = `${baseUrl}/api/v1/issues/search?q=${encodeURIComponent(search as string)}&page=${page}&limit=${per_page}`;
    const response = host.http.request({
      method: 'GET',
      url: apiUrl,
      headers: headers
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Found ${data.total} issues`,
      data: data.data
    };
  },
  searchMergeRequests: function (params: Record<string, unknown>, headers: Record<string, unknown>, baseUrl: string): Record<string, unknown> {
    const { search, page = 1, per_page = 20 } = params;
    const apiUrl = `${baseUrl}/api/v1/pulls/search?q=${encodeURIComponent(search as string)}&page=${page}&limit=${per_page}`;
    const response = host.http.request({
      method: 'GET',
      url: apiUrl,
      headers: headers
    });
    const r = giteaResult(response);
    if (!r.ok) {
      return {
        success: false,
        error: r.error
      };
    }
    const data = r.data;
    return {
      success: true,
      result: `Found ${data.total} merge requests`,
      data: data.data
    };
  }
};
type GiteaResult = { ok: true; data: any } | { ok: false; error: string };

// Decide success from the HTTP status, not from a substring of the body.
//
// The previous check was `response.body.includes("ok")`, which was wrong in
// both directions:
//
//   - Gitea's write endpoints (create/update file, push files, create repo,
//     issue, merge request, branch, fork) return the created object, e.g.
//     FileResponse = {content, commit}. There is no "ok" field, so every
//     *successful* write was reported as a failure -- and the caller was told
//     to retry a write that had already landed.
//   - Error bodies frequently contain "ok" as a substring: Gitea answers an
//     expired or under-scoped token with {"message":"token is required"} (401)
//     or {"message":"token does not have at least one of required scope(s)..."}
//     (403). Both contain "ok" inside "token", so every *failed* write was
//     reported as success.
//
// host.http.request returns {status, headers, body} and does not throw on a
// non-2xx status, so the status is always available and is the real signal.
function giteaResult(response: any): GiteaResult {
  const status = response && typeof response.status === "number" ? response.status : 0;
  const body = response && typeof response.body === "string" ? response.body : "";

  if (status < 200 || status >= 300) {
    let detail = "";
    try {
      const parsed = JSON.parse(body);
      detail = parsed.message || JSON.stringify(parsed);
    } catch (e) {
      detail = body ? body.slice(0, 200) : "";
    }
    return {
      ok: false,
      error: detail ? `HTTP ${status}: ${detail}` : `HTTP ${status}`
    };
  }

  if (!body) {
    return { ok: false, error: "No response from server" };
  }

  try {
    return { ok: true, data: JSON.parse(body) };
  } catch (e) {
    return { ok: false, error: `HTTP ${status}: response was not JSON` };
  }
}
