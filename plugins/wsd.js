"use strict";
const plugin = {
  name: "wsd",
  description: "Workspace Daemon (wsd): prepare ephemeral dev workspaces, run commands, pull diffs/artifacts, open GitLab merge requests, check status, and tear down via the local wsd Go daemon.",
  version: "1.0.0",
  commit: "none",
  Tags: ["devtools", "workspace", "gitlab", "automation"],
  annotations: {
    readOnlyHint: false,
    destructiveHint: true,
    idempotentHint: false,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {
      CommandEvent: {
        type: "string",
        description: "wsd action to execute.",
        enum: [
          "prepare_workspace",
          "exec_command",
          "get_artifacts",
          "commit_and_pr",
          "workspace_status",
          "destroy_workspace"
        ]
      },
      // --- prepare_workspace ---
      repo_url: {
        type: "string",
        description: "Git URL to clone (https or ssh). Required for prepare_workspace and optionally used by commit_and_pr."
      },
      ref: {
        type: "string",
        description: "Branch, tag, or commit to check out. Defaults to 'main'. Used by prepare_workspace.",
        default: "main"
      },
      // --- shared ---
      workspace_id: {
        type: "string",
        description: "ID returned by prepare_workspace. Required for exec_command, get_artifacts, commit_and_pr, workspace_status, destroy_workspace."
      },
      // --- exec_command ---
      command: {
        type: "string",
        description: "Shell command to execute, e.g. 'go test ./...' or 'pytest'. Required for exec_command."
      },
      allow_network: {
        type: "boolean",
        description: "MANDATORY for exec_command: true if the command needs internet access, false for local-only tasks (more secure)."
      },
      timeout_seconds: {
        type: "integer",
        description: "Max seconds to allow the command to run before it's killed (exec_command). Default 120.",
        default: 120
      },
      secrets: {
        type: "array",
        description: "List of secret reference names to grant for this exec call only (exec_command).",
        items: { type: "string" }
      },
      // --- get_artifacts ---
      paths: {
        type: "string",
        description: "Optional comma-separated list of paths to scope the diff to (get_artifacts)."
      },
      // --- commit_and_pr ---
      commit_message: {
        type: "string",
        description: "Git commit message for the changes (commit_and_pr)."
      },
      pr_title: {
        type: "string",
        description: "Title for the merge request (commit_and_pr)."
      },
      pr_description: {
        type: "string",
        description: "Optional longer description for the merge request (commit_and_pr)."
      },
      gitlab_project_path: {
        type: "string",
        description: "URL-encoded namespace/project path, or numeric project ID (commit_and_pr)."
      }
    },
    required: ["CommandEvent"]
  },
  call(params) {
    const self = module.exports;
    const { CommandEvent } = params;
    let baseUrl, token;
    try {
      baseUrl = host.process.env("WSD_BASE_URL") || host.config.options.base_url || "https://docker-vm.crumpton.org:9080";
      token = host.process.env("WSD_TOKEN") || host.config.options.token || void 0;
    } catch (err) {
      return { success: false, error: `Failed to load wsd configuration: ${err.message}` };
    }
    if (!token) {
      return { success: false, error: "Missing wsd bearer token. Set options.token in config or the WSD_TOKEN environment variable." };
    }
    baseUrl = baseUrl.replace(/\/$/, "");
    host.server.logger(3, `wsd: CommandEvent=${CommandEvent} baseUrl=${baseUrl}`);
    switch (CommandEvent) {
      case "prepare_workspace":
        return self.prepareWorkspace(params, token, baseUrl);
      case "exec_command":
        return self.execCommand(params, token, baseUrl);
      case "get_artifacts":
        return self.getArtifacts(params, token, baseUrl);
      case "commit_and_pr":
        return self.commitAndPr(params, token, baseUrl);
      case "workspace_status":
        return self.workspaceStatus(params, token, baseUrl);
      case "destroy_workspace":
        return self.destroyWorkspace(params, token, baseUrl);
      default:
        return { success: false, error: `Unknown CommandEvent: ${CommandEvent}` };
    }
  },
  // ── Internal response normaliser ────────────────────────────────────────
  _handleResponse(resp, label) {
    const status = resp.status;
    const bodyText = typeof resp.body === "string" ? resp.body : "";
    if (status >= 200 && status < 300) {
      try {
        return { success: true, result: JSON.parse(bodyText) };
      } catch (e) {
        return { success: true, result: bodyText };
      }
    }
    host.server.logger(1, `wsd: ${label} \u2192 HTTP ${status}: ${bodyText}`);
    return { success: false, error: `HTTP ${status}`, detail: bodyText || void 0 };
  },
  prepareWorkspace(params, token, baseUrl) {
    if (!params.repo_url) {
      return { success: false, error: "repo_url is required for prepare_workspace." };
    }
    const headers = {
      "Authorization": "Bearer " + token,
      "Content-Type": "application/json"
    };
    const body = JSON.stringify({
      repo_url: params.repo_url,
      ref: params.ref || "main"
    });
    try {
      const resp = host.http.post(baseUrl + "/workspaces", headers, body);
      return this._handleResponse(resp, "prepare_workspace");
    } catch (err) {
      return { success: false, error: `Error preparing workspace: ${err.message}` };
    }
  },
  execCommand(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for exec_command." };
    }
    if (!params.command) {
      return { success: false, error: "command is required for exec_command." };
    }
    if (typeof params.allow_network !== "boolean") {
      return { success: false, error: "allow_network (boolean) is mandatory for exec_command." };
    }
    const headers = {
      "Authorization": "Bearer " + token,
      "Content-Type": "application/json"
    };
    const body = JSON.stringify({
      command: params.command,
      timeout_seconds: params.timeout_seconds || 120,
      secrets: params.secrets || [],
      allow_network: params.allow_network
    });
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/exec";
    try {
      const resp = host.http.post(url, headers, body);
      return this._handleResponse(resp, "exec_command");
    } catch (err) {
      return { success: false, error: `Error executing command: ${err.message}` };
    }
  },
  getArtifacts(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for get_artifacts." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    let url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/artifacts";
    if (params.paths) {
      url += "?paths=" + encodeURIComponent(params.paths);
    }
    try {
      const resp = host.http.get(url, headers);
      return this._handleResponse(resp, "get_artifacts");
    } catch (err) {
      return { success: false, error: `Error fetching artifacts: ${err.message}` };
    }
  },
  commitAndPr(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for commit_and_pr." };
    }
    if (!params.commit_message) {
      return { success: false, error: "commit_message is required for commit_and_pr." };
    }
    if (!params.pr_title) {
      return { success: false, error: "pr_title is required for commit_and_pr." };
    }
    const headers = {
      "Authorization": "Bearer " + token,
      "Content-Type": "application/json"
    };
    const payload = {
      commit_message: params.commit_message,
      pr_title: params.pr_title,
      pr_description: params.pr_description || "",
      repo_url: params.repo_url || ""
    };
    if (params.gitlab_project_path) {
      payload.gitlab_project_path = params.gitlab_project_path;
    }
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/commit-and-pr";
    try {
      const resp = host.http.post(url, headers, JSON.stringify(payload));
      return this._handleResponse(resp, "commit_and_pr");
    } catch (err) {
      return { success: false, error: `Error committing and opening PR: ${err.message}` };
    }
  },
  workspaceStatus(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for workspace_status." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/status";
    try {
      const resp = host.http.get(url, headers);
      return this._handleResponse(resp, "workspace_status");
    } catch (err) {
      return { success: false, error: `Error fetching status: ${err.message}` };
    }
  },
  destroyWorkspace(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for destroy_workspace." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id);
    try {
      const resp = host.http.delete(url, headers);
      if (resp.status >= 200 && resp.status < 300) {
        return { success: true, result: `Workspace ${params.workspace_id} destroyed.` };
      }
      return { success: false, error: `HTTP ${resp.status}`, detail: resp.body };
    } catch (err) {
      return { success: false, error: `Error destroying workspace: ${err.message}` };
    }
  }
};
module.exports = plugin;
