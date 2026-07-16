"use strict";
/**
 * wsd.js — Workspace Daemon plugin for mcphe
 *
 * Ports the OpenWebUI "Workspace Daemon (wsd)" tool to a synchronous goja
 * plugin. Prepares ephemeral dev workspaces, runs commands in them, pulls
 * diffs/artifacts, opens GitLab merge requests, checks status, and tears
 * workspaces down.
 *
 * Config (config.yaml):
 *
 *   plugins:
 *     wsd:
 *       allowed_domains: ["docker-vm.crumpton.org"]
 *       allowed_env_vars: ["WSD_BASE_URL", "WSD_TOKEN"]
 *       options:
 *         base_url: "https://docker-vm.crumpton.org:9080"
 *         # token is better supplied via WSD_TOKEN env var than hardcoded here
 *
 * goja constraints observed:
 *   - call() is synchronous — no async/await, host.http.* calls block.
 *   - Headers passed to host.http.* are a flat {key: string} object.
 *   - No `this` inside call() — use `const self = module.exports`.
 *
 * Note: host.http.* has a fixed ~15s server-side timeout (see
 * host/httpclient/httpclient.go). The original Python tool's per-call
 * timeout_seconds/exec timeout_seconds are still sent to the daemon (which
 * may run the command in the background / poll), but mcphe itself will not
 * hold a single HTTP request open longer than the host's configured client
 * timeout. Long-running exec_command calls should be designed by the wsd
 * daemon to return quickly (e.g. job id + poll), or the host timeout should
 * be raised if you control that code.
 */

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
          "safe_edit",
          "list_files",
          "read_file",
          "write_file",
          "start_process",
          "list_processes",
          "get_process_status",
          "send_process_input",
          "kill_process",
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
      // --- safe_edit ---
      path: {
        type: "string",
        description: "Path to the file to edit (safe_edit)."
      },
      old_text: {
        type: "string",
        description: "Text to replace (safe_edit)."
      },
      new_text: {
        type: "string",
        description: "Text to replace with (safe_edit)."
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
      },
      // --- file system tools ---
      list_files: {
        type: "string",
        description: "Path to list files in. Default /workspace (commit_and_pr)."
      },
      read_file: {
        type: "string",
        description: "Path to read file from. Default /workspace (commit_and_pr)."
      },
      write_file: {
        type: "string",
        description: "Path to write file to. Default /workspace (commit_and_pr)."
      },
      start_process: {
        type: "object",
        description: "Start a background process.",
        properties: {
          workspace_id: {
            type: "string",
            description: "ID returned by prepare_workspace."
          },
          command: {
            type: "string",
            description: "Shell command to run in the background, e.g. 'npm run dev'."
          },
          allow_network: {
            type: "boolean",
            description: "Set to True if the process needs internet access."
          }
        }
      },
      list_processes: {
        type: "object",
        description: "List background processes.",
        properties: {
          workspace_id: {
            type: "string",
            description: "ID returned by prepare_workspace."
          }
        }
      },
      get_process_status: {
        type: "object",
        description: "Get the status of a background process.",
        properties: {
          workspace_id: {
            type: "string",
            description: "ID returned by prepare_workspace."
          },
          process_id: {
            type: "string",
            description: "ID of the process to get the status of."
          }
        }
      },
      send_process_input: {
        type: "object",
        description: "Send input to a background process.",
        properties: {
          workspace_id: {
            type: "string",
            description: "ID returned by prepare_workspace."
          },
          process_id: {
            type: "string",
            description: "ID of the process to send input to."
          },
          input: {
            type: "string",
            description: "Input to send to the process."
          }
        }
      },
      kill_process: {
        type: "object",
        description: "Kill a background process.",
        properties: {
          workspace_id: {
            type: "string",
            description: "ID returned by prepare_workspace."
          },
          process_id: {
            type: "string",
            description: "ID of the process to kill."
          }
        }
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
  },

  list_files(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for list_files." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/files";
    try {
      const resp = host.http.get(url, headers);
      return this._handleResponse(resp, "list_files");
    } catch (err) {
      return { success: false, error: `Error listing files: ${err.message}` };
    }
  },

  read_file(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for read_file." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/files/" + encodeURIComponent(params.path);
    try {
      const resp = host.http.get(url, headers);
      return this._handleResponse(resp, "read_file");
    } catch (err) {
      return { success: false, error: `Error reading file: ${err.message}` };
    }
  },

  write_file(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for write_file." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/files/" + encodeURIComponent(params.path);
    try {
      const resp = host.http.post(url, headers, params.content);
      return this._handleResponse(resp, "write_file");
    } catch (err) {
      return { success: false, error: `Error writing file: ${err.message}` };
    }
  },

  start_process(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for start_process." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/processes";
    try {
      const resp = host.http.post(url, headers, params.command);
      return this._handleResponse(resp, "start_process");
    } catch (err) {
      return { success: false, error: `Error starting process: ${err.message}` };
    }
  },
  list_processes(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for list_processes." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/processes";
    try {
      const resp = host.http.get(url, headers);
      return this._handleResponse(resp, "list_processes");
    } catch (err) {
      return { success: false, error: `Error listing processes: ${err.message}` };
    }
  },
  get_process_status(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for get_process_status." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/processes/" + encodeURIComponent(params.process_id);
    try {
      const resp = host.http.get(url, headers);
      return this._handleResponse(resp, "get_process_status");
    } catch (err) {
      return { success: false, error: `Error getting process status: ${err.message}` };
    }
  },
  send_process_input(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for send_process_input." };
    }
    if (!params.process_id) {
      return { success: false, error: "process_id is required for send_process_input." };
    }
    if (!params.input) {
      return { success: false, error: "input is required for send_process_input." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/processes/" + encodeURIComponent(params.process_id) + "/input";
    try {
      const resp = host.http.post(url, headers, params.input);
      return this._handleResponse(resp, "send_process_input");
    } catch (err) {
      return { success: false, error: `Error sending process input: ${err.message}` };
    }
  },
  kill_process(params, token, baseUrl) {
    if (!params.workspace_id) {
      return { success: false, error: "workspace_id is required for kill_process." };
    }
    if (!params.process_id) {
      return { success: false, error: "process_id is required for kill_process." };
    }
    const headers = {
      "Authorization": "Bearer " + token
    };
    const url = baseUrl + "/workspaces/" + encodeURIComponent(params.workspace_id) + "/processes/" + encodeURIComponent(params.process_id);
    try {
      const resp = host.http.delete(url, headers);
      return this._handleResponse(resp, "kill_process");
    } catch (err) {
      return { success: false, error: `Error killing process: ${err.message}` };
    }
  }
};


module.exports = plugin;

/*
Will match new functions with goja's call()

    # -------------------------------------------------------------------------
    # File System Tools
    # -------------------------------------------------------------------------

    def list_files(self, workspace_id: str, path: str) -> str:
        """
        Return a structured listing of files and directories at a given path
        inside the workspace. Equivalent to a single-level directory listing.

        :param workspace_id: ID returned by prepare_workspace.
        :param path: Absolute path inside the container to list, e.g. "/workspace/repo".
        :return: JSON string with an "entries" array, each entry containing
                 name, path, is_dir, and size (bytes for files, 0 for directories).
        """
        try:
            resp = requests.get(
                self._url(f"/workspaces/{workspace_id}/files"),
                headers=self._headers(),
                params={"path": path},
                timeout=self.valves.timeout_seconds,
            )
            resp.raise_for_status()
            return resp.text
        except requests.exceptions.RequestException as e:
            return f"Error listing files: {e}"

    def read_file(self, workspace_id: str, path: str) -> str:
        """
        Read the full contents of a specific file inside the workspace.

        :param workspace_id: ID returned by prepare_workspace.
        :param path: Absolute path to the file inside the container,
                     e.g. "/workspace/repo/main.go".
        :return: JSON string with a "content" field containing the file text.
        """
        try:
            resp = requests.get(
                self._url(f"/workspaces/{workspace_id}/file"),
                headers=self._headers(),
                params={"path": path},
                timeout=self.valves.timeout_seconds,
            )
            resp.raise_for_status()
            return resp.text
        except requests.exceptions.RequestException as e:
            return f"Error reading file: {e}"

    def write_file(self, workspace_id: str, path: str, content: str) -> str:
        """
        Write text content to a file inside the workspace, creating or overwriting it.
        Use safe_edit instead when you only want to replace a specific substring.

        :param workspace_id: ID returned by prepare_workspace.
        :param path: Absolute path to write inside the container,
                     e.g. "/workspace/repo/config.json".
        :param content: Full text content to write to the file.
        :return: Confirmation message on success, or an error.
        """
        try:
            resp = requests.put(
                self._url(f"/workspaces/{workspace_id}/file"),
                headers=self._headers(),
                json={"path": path, "content": content},
                timeout=self.valves.timeout_seconds,
            )
            resp.raise_for_status()
            return f"File {path} written successfully."
        except requests.exceptions.RequestException as e:
            return f"Error writing file: {e}"

    # -------------------------------------------------------------------------
    # Background Process Management Tools
    # -------------------------------------------------------------------------

    def start_process(
        self,
        workspace_id: str,
        command: str,
        allow_network: bool = False,
    ) -> str:
        """
        Launch a long-running command inside the workspace as a tracked background
        process. Returns immediately with a process ID; use get_process_status to
        poll for output and check whether it has finished. Ideal for servers,
        file watchers, or any command that should run concurrently while other
        work continues.

        :param workspace_id: ID returned by prepare_workspace.
        :param command: Shell command to run in the background, e.g. "npm run dev".
        :param allow_network: Set to True if the process needs internet access.
        :return: JSON string with id, command, and initial status ("running").
        """
        try:
            resp = requests.post(
                self._url(f"/workspaces/{workspace_id}/processes"),
                headers=self._headers(),
                json={"command": command, "allow_network": allow_network},
                timeout=self.valves.timeout_seconds,
            )
            resp.raise_for_status()
            return resp.text
        except requests.exceptions.RequestException as e:
            return f"Error starting process: {e}"

    def list_processes(self, workspace_id: str) -> str:
        """
        List all tracked background processes for a workspace, including their
        current status (running, exited, or killed) and exit codes where available.

        :param workspace_id: ID returned by prepare_workspace.
        :return: JSON string with a "processes" array.
        """
        try:
            resp = requests.get(
                self._url(f"/workspaces/{workspace_id}/processes"),
                headers=self._headers(),
                timeout=self.valves.timeout_seconds,
            )
            resp.raise_for_status()
            return resp.text
        except requests.exceptions.RequestException as e:
            return f"Error listing processes: {e}"

    def get_process_status(self, workspace_id: str, process_id: str) -> str:
        """
        Get the buffered stdout/stderr output and current status of a tracked
        background process. Output is accumulated in memory (up to 1 MiB per
        stream); call repeatedly to poll for new output as the process runs.

        :param workspace_id: ID returned by prepare_workspace.
        :param process_id: Process ID returned by start_process.
        :return: JSON string with id, command, status, exit_code, stdout, and stderr.
        """
        try:
            resp = requests.get(
                self._url(f"/workspaces/{workspace_id}/processes/{process_id}"),
                headers=self._headers(),
                timeout=self.valves.timeout_seconds,
            )
            resp.raise_for_status()
            return resp.text
        except requests.exceptions.RequestException as e:
            return f"Error getting process status: {e}"

    def send_process_input(
        self, workspace_id: str, process_id: str, input_text: str
    ) -> str:
        """
        Write text to the standard input (stdin) of a running background process.
        Useful for answering interactive prompts or driving a REPL.

        :param workspace_id: ID returned by prepare_workspace.
        :param process_id: Process ID returned by start_process.
        :param input_text: Text to write to stdin (newline not appended automatically).
        :return: Confirmation message on success, or an error.
        """
        try:
            resp = requests.post(
                self._url(
                    f"/workspaces/{workspace_id}/processes/{process_id}/input"
                ),
                headers=self._headers(),
                json={"input": input_text},
                timeout=self.valves.timeout_seconds,
            )
            resp.raise_for_status()
            return f"Input sent to process {process_id}."
        except requests.exceptions.RequestException as e:
            return f"Error sending process input: {e}"

    def kill_process(self, workspace_id: str, process_id: str) -> str:
        """
        Terminate a background process by ID. If the process has already exited,
        it is simply removed from the tracking table. Use this to stop long-running
        servers or file watchers when they are no longer needed.

        :param workspace_id: ID returned by prepare_workspace.
        :param process_id: Process ID returned by start_process.
        :return: Confirmation message on success, or an error.
        """
        try:
            resp = requests.delete(
                self._url(
                    f"/workspaces/{workspace_id}/processes/{process_id}"
                ),
                headers=self._headers(),
                timeout=self.valves.timeout_seconds,
            )
            resp.raise_for_status()
            return f"Process {process_id} killed."
        except requests.exceptions.RequestException as e:
            return f"Error killing process: {e}"

*/