"use strict";
function str(v) {
  return typeof v === "string" ? v : v === void 0 || v === null ? "" : String(v);
}
function isObj(v) {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}
const wsdPlugin = {
  name: "wsd",
  description: "Workspace Daemon (wsd): prepare ephemeral dev workspaces, run commands, edit/read/write files, manage background processes, pull diffs/artifacts, open pull/merge requests, check status, and tear down via the local wsd Go daemon.",
  version: "1.1.0",
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
          "safe_edit",
          "commit_and_pr",
          "workspace_status",
          "destroy_workspace",
          "list_files",
          "read_file",
          "write_file",
          "start_process",
          "list_processes",
          "get_process_status",
          "send_process_input",
          "kill_process"
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
      env_overrides: {
        type: "object",
        description: "Optional map pinning the environment for prepare_workspace. Supported keys: language, version, package_manager, install_command, base_image, post_create_command.",
        properties: {
          language: { type: "string" },
          version: { type: "string" },
          package_manager: { type: "string" },
          install_command: { type: "string" },
          base_image: { type: "string" },
          post_create_command: { type: "string" }
        }
      },
      // --- shared ---
      workspace_id: {
        type: "string",
        description: "ID returned by prepare_workspace. Required for exec_command, get_artifacts, safe_edit, commit_and_pr, workspace_status, destroy_workspace, list_files, read_file, write_file, start_process, list_processes, get_process_status, send_process_input, kill_process."
      },
      // --- exec_command / start_process ---
      command: {
        type: "string",
        description: "Shell command to execute, e.g. 'go test ./...' or 'pytest'. Required for exec_command and start_process."
      },
      allow_network: {
        type: "boolean",
        description: "MANDATORY for exec_command: true if the command needs internet access, false for local-only tasks (more secure). Optional for start_process (default false)."
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
      // --- get_artifacts / list_files / read_file / write_file ---
      paths: {
        type: "string",
        description: "Optional comma-separated list of paths to scope the diff to (get_artifacts)."
      },
      path: {
        type: "string",
        description: "Absolute path inside the workspace. Required for list_files, read_file, and write_file."
      },
      content: {
        type: "string",
        description: "Full text content to write to the file (write_file)."
      },
      // --- safe_edit ---
      file_path: {
        type: "string",
        description: "Absolute path of the file to edit inside the workspace (safe_edit)."
      },
      pattern: {
        type: "string",
        description: "Exact literal string to find (not a regex) (safe_edit)."
      },
      replacement: {
        type: "string",
        description: "String to replace every occurrence of pattern with (safe_edit)."
      },
      // --- commit_and_pr ---
      commit_message: {
        type: "string",
        description: "Git commit message for the changes (commit_and_pr)."
      },
      pr_title: {
        type: "string",
        description: "Title for the pull/merge request (commit_and_pr)."
      },
      pr_description: {
        type: "string",
        description: "Optional longer description for the pull/merge request (commit_and_pr)."
      },
      forge_type: {
        type: "string",
        description: "Type of the git forge, e.g. github, gitlab, gitea (commit_and_pr)."
      },
      project_path: {
        type: "string",
        description: "Project path or owner/repo format for the forge (commit_and_pr)."
      },
      gitlab_project_path: {
        type: "string",
        description: "URL-encoded namespace/project path, or numeric project ID (legacy, use project_path) (commit_and_pr)."
      },
      // --- background processes ---
      process_id: {
        type: "string",
        description: "Process ID returned by start_process. Required for get_process_status, send_process_input, kill_process."
      },
      input_text: {
        type: "string",
        description: "Text to write to stdin (newline not appended automatically) (send_process_input)."
      }
    },
    required: ["CommandEvent"]
  },
  call(params) {
    var _a, _b;
    const self = module.exports;
    const CommandEvent = str(params.CommandEvent);
    let baseUrl;
    let token;
    try {
      baseUrl = host.process.env("WSD_BASE_URL") || str((_a = host.config.options) == null ? void 0 : _a.base_url) || "https://docker-vm.crumpton.org:9080";
      token = host.process.env("WSD_TOKEN") || str((_b = host.config.options) == null ? void 0 : _b.token) || "";
    } catch (err) {
      return { success: false, error: `Failed to load wsd configuration: ${err.message}` };
    }
    if (!token) {
      return {
        success: false,
        error: "Missing wsd bearer token. Set options.token in config or the WSD_TOKEN environment variable."
      };
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
      case "safe_edit":
        return self.safeEdit(params, token, baseUrl);
      case "commit_and_pr":
        return self.commitAndPr(params, token, baseUrl);
      case "workspace_status":
        return self.workspaceStatus(params, token, baseUrl);
      case "destroy_workspace":
        return self.destroyWorkspace(params, token, baseUrl);
      case "list_files":
        return self.listFiles(params, token, baseUrl);
      case "read_file":
        return self.readFile(params, token, baseUrl);
      case "write_file":
        return self.writeFile(params, token, baseUrl);
      case "start_process":
        return self.startProcess(params, token, baseUrl);
      case "list_processes":
        return self.listProcesses(params, token, baseUrl);
      case "get_process_status":
        return self.getProcessStatus(params, token, baseUrl);
      case "send_process_input":
        return self.sendProcessInput(params, token, baseUrl);
      case "kill_process":
        return self.killProcess(params, token, baseUrl);
      default:
        return { success: false, error: `Unknown CommandEvent: ${CommandEvent}` };
    }
  },
  // ── Internal helpers ────────────────────────────────────────────────────
  _headers(token, json) {
    const h = { Authorization: "Bearer " + token };
    if (json) h["Content-Type"] = "application/json";
    return h;
  },
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
  _require(params, fields, event) {
    for (const f of fields) {
      if (params[f] === void 0 || params[f] === null || params[f] === "") {
        return { success: false, error: `${f} is required for ${event}.` };
      }
    }
    return null;
  },
  _wsUrl(baseUrl, workspaceId, suffix) {
    return baseUrl + "/workspaces/" + encodeURIComponent(str(workspaceId)) + suffix;
  },
  // ── Workspace lifecycle ─────────────────────────────────────────────────
  prepareWorkspace(params, token, baseUrl) {
    const missing = this._require(params, ["repo_url"], "prepare_workspace");
    if (missing) return missing;
    const payload = {
      repo_url: params.repo_url,
      ref: str(params.ref) || "main"
    };
    if (isObj(params.env_overrides) && Object.keys(params.env_overrides).length > 0) {
      payload.env_overrides = params.env_overrides;
    }
    try {
      const resp = host.http.post(baseUrl + "/workspaces", this._headers(token, true), JSON.stringify(payload));
      return this._handleResponse(resp, "prepare_workspace");
    } catch (err) {
      return { success: false, error: `Error preparing workspace: ${err.message}` };
    }
  },
  execCommand(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "command"], "exec_command");
    if (missing) return missing;
    if (typeof params.allow_network !== "boolean") {
      return { success: false, error: "allow_network (boolean) is mandatory for exec_command." };
    }
    const body = JSON.stringify({
      command: params.command,
      timeout_seconds: params.timeout_seconds || 120,
      secrets: Array.isArray(params.secrets) ? params.secrets : [],
      allow_network: params.allow_network
    });
    try {
      const resp = host.http.post(this._wsUrl(baseUrl, params.workspace_id, "/exec"), this._headers(token, true), body);
      return this._handleResponse(resp, "exec_command");
    } catch (err) {
      return { success: false, error: `Error executing command: ${err.message}` };
    }
  },
  getArtifacts(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id"], "get_artifacts");
    if (missing) return missing;
    let url = this._wsUrl(baseUrl, params.workspace_id, "/artifacts");
    if (params.paths) url += "?paths=" + encodeURIComponent(str(params.paths));
    try {
      const resp = host.http.get(url, this._headers(token, false));
      return this._handleResponse(resp, "get_artifacts");
    } catch (err) {
      return { success: false, error: `Error fetching artifacts: ${err.message}` };
    }
  },
  safeEdit(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "file_path", "pattern"], "safe_edit");
    if (missing) return missing;
    const body = JSON.stringify({
      file_path: params.file_path,
      pattern: params.pattern,
      replacement: str(params.replacement)
    });
    try {
      const resp = host.http.post(this._wsUrl(baseUrl, params.workspace_id, "/safe-edit"), this._headers(token, true), body);
      return this._handleResponse(resp, "safe_edit");
    } catch (err) {
      return { success: false, error: `Error editing file: ${err.message}` };
    }
  },
  commitAndPr(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "commit_message", "pr_title"], "commit_and_pr");
    if (missing) return missing;
    const payload = {
      commit_message: params.commit_message,
      pr_title: params.pr_title,
      pr_description: str(params.pr_description),
      repo_url: str(params.repo_url)
    };
    if (params.forge_type) payload.forge_type = params.forge_type;
    if (params.project_path) payload.project_path = params.project_path;
    if (params.gitlab_project_path) payload.gitlab_project_path = params.gitlab_project_path;
    try {
      const resp = host.http.post(
        this._wsUrl(baseUrl, params.workspace_id, "/commit-and-pr"),
        this._headers(token, true),
        JSON.stringify(payload)
      );
      return this._handleResponse(resp, "commit_and_pr");
    } catch (err) {
      return { success: false, error: `Error committing and opening PR: ${err.message}` };
    }
  },
  workspaceStatus(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id"], "workspace_status");
    if (missing) return missing;
    try {
      const resp = host.http.get(this._wsUrl(baseUrl, params.workspace_id, "/status"), this._headers(token, false));
      return this._handleResponse(resp, "workspace_status");
    } catch (err) {
      return { success: false, error: `Error fetching status: ${err.message}` };
    }
  },
  destroyWorkspace(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id"], "destroy_workspace");
    if (missing) return missing;
    try {
      const resp = host.http.delete(this._wsUrl(baseUrl, params.workspace_id, ""), this._headers(token, false));
      if (resp.status >= 200 && resp.status < 300) {
        return { success: true, result: `Workspace ${params.workspace_id} destroyed.` };
      }
      return { success: false, error: `HTTP ${resp.status}`, detail: resp.body };
    } catch (err) {
      return { success: false, error: `Error destroying workspace: ${err.message}` };
    }
  },
  // ── File system ─────────────────────────────────────────────────────────
  listFiles(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "path"], "list_files");
    if (missing) return missing;
    const url = this._wsUrl(baseUrl, params.workspace_id, "/files") + "?path=" + encodeURIComponent(str(params.path));
    try {
      const resp = host.http.get(url, this._headers(token, false));
      return this._handleResponse(resp, "list_files");
    } catch (err) {
      return { success: false, error: `Error listing files: ${err.message}` };
    }
  },
  readFile(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "path"], "read_file");
    if (missing) return missing;
    const url = this._wsUrl(baseUrl, params.workspace_id, "/file") + "?path=" + encodeURIComponent(str(params.path));
    try {
      const resp = host.http.get(url, this._headers(token, false));
      return this._handleResponse(resp, "read_file");
    } catch (err) {
      return { success: false, error: `Error reading file: ${err.message}` };
    }
  },
  writeFile(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "path"], "write_file");
    if (missing) return missing;
    const body = JSON.stringify({ path: params.path, content: str(params.content) });
    try {
      const resp = host.http.put(this._wsUrl(baseUrl, params.workspace_id, "/file"), this._headers(token, true), body);
      if (resp.status >= 200 && resp.status < 300) {
        return { success: true, result: `File ${params.path} written successfully.` };
      }
      return { success: false, error: `HTTP ${resp.status}`, detail: resp.body };
    } catch (err) {
      return { success: false, error: `Error writing file: ${err.message}` };
    }
  },
  // ── Background processes ────────────────────────────────────────────────
  startProcess(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "command"], "start_process");
    if (missing) return missing;
    const body = JSON.stringify({
      command: params.command,
      allow_network: typeof params.allow_network === "boolean" ? params.allow_network : false
    });
    try {
      const resp = host.http.post(this._wsUrl(baseUrl, params.workspace_id, "/processes"), this._headers(token, true), body);
      return this._handleResponse(resp, "start_process");
    } catch (err) {
      return { success: false, error: `Error starting process: ${err.message}` };
    }
  },
  listProcesses(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id"], "list_processes");
    if (missing) return missing;
    try {
      const resp = host.http.get(this._wsUrl(baseUrl, params.workspace_id, "/processes"), this._headers(token, false));
      return this._handleResponse(resp, "list_processes");
    } catch (err) {
      return { success: false, error: `Error listing processes: ${err.message}` };
    }
  },
  getProcessStatus(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "process_id"], "get_process_status");
    if (missing) return missing;
    const url = this._wsUrl(baseUrl, params.workspace_id, "/processes/" + encodeURIComponent(str(params.process_id)));
    try {
      const resp = host.http.get(url, this._headers(token, false));
      return this._handleResponse(resp, "get_process_status");
    } catch (err) {
      return { success: false, error: `Error getting process status: ${err.message}` };
    }
  },
  sendProcessInput(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "process_id"], "send_process_input");
    if (missing) return missing;
    const body = JSON.stringify({ input: str(params.input_text) });
    const url = this._wsUrl(
      baseUrl,
      params.workspace_id,
      "/processes/" + encodeURIComponent(str(params.process_id)) + "/input"
    );
    try {
      const resp = host.http.post(url, this._headers(token, true), body);
      if (resp.status >= 200 && resp.status < 300) {
        return { success: true, result: `Input sent to process ${params.process_id}.` };
      }
      return { success: false, error: `HTTP ${resp.status}`, detail: resp.body };
    } catch (err) {
      return { success: false, error: `Error sending process input: ${err.message}` };
    }
  },
  killProcess(params, token, baseUrl) {
    const missing = this._require(params, ["workspace_id", "process_id"], "kill_process");
    if (missing) return missing;
    const url = this._wsUrl(baseUrl, params.workspace_id, "/processes/" + encodeURIComponent(str(params.process_id)));
    try {
      const resp = host.http.delete(url, this._headers(token, false));
      if (resp.status >= 200 && resp.status < 300) {
        return { success: true, result: `Process ${params.process_id} killed.` };
      }
      return { success: false, error: `HTTP ${resp.status}`, detail: resp.body };
    } catch (err) {
      return { success: false, error: `Error killing process: ${err.message}` };
    }
  }
};
module.exports = wsdPlugin;
