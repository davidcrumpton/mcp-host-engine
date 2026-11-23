/// <reference path="../../types/mcphe.d.ts" />

const plugin = {
  name:        "proxmox_api",
  description: "Query Proxmox for LXCs, VMs, and cluster/node information.",
  version: "1.1.2",
  commit:      "none",
  Tags:        ["infrastructure", "proxmox", "virtualization"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  true,
    openWorldHint:   true,
  },

  inputSchema: {
    type: "object",
    properties: {
      mode: {
        type: "string",
        description: "What to fetch from Proxmox.",
        enum: [
          "list_vms",
          "list_lxcs",
          "list_all_instances",
          "get_vm",
          "get_lxc",
          "instance_status",
          "cluster_resources",
          "cluster_status",
          "node_resources",
          "raw"
        ]
      },
      node: {
        type: "string",
        description: "Proxmox node name (required for some modes)."
      },
      vmid: {
        type: "string",
        description: "VMID of the VM or LXC (required for get_* and instance_status)."
      },
      type: {
        type: "string",
        description: "Instance type for instance_status (qemu or lxc).",
        enum: ["qemu", "lxc"]
      },
      path: {
        type: "string",
        description: "Raw API path under /api2/json for mode=raw (e.g. '/nodes/pve/qemu')."
      }
    },
    required: ["mode"]
  },

  // call() must NOT be async. All host.* calls are synchronous — no await needed.
  call(params: Record<string, unknown>) {
    const cfg = (host.server.config() || {}) as Record<string, any>;

    // ENV VARS ALWAYS OVERRIDE CONFIG.
    // host.process.env THROWS for a key outside allowed_env_vars rather than
    // returning undefined, so treat a throw as "not configured via env" --
    // otherwise a config-only deployment fails on an env var it never used.
    let envBaseUrl = "";
    let envApiToken = "";
    try { envBaseUrl = host.process.env("PROXMOX_BASE_URL") || ""; } catch (envErr) { envBaseUrl = ""; }
    try { envApiToken = host.process.env("PROXMOX_API_TOKEN") || ""; } catch (envErr) { envApiToken = ""; }

    const baseUrl = envBaseUrl || cfg.base_url;
    // <USERNAME>@<REALM>!<TOKEN_NAME>=<GUID>
    const apiToken = envApiToken || cfg.api_token;

    if (!baseUrl || !apiToken) {
      throw new Error("base_url and api_token must be set via env vars or plugin config");
    }

    function buildUrl(path) {
      let base = baseUrl;
      if (base.endsWith("/")) base = base.slice(0, -1);
      if (!path.startsWith("/")) path = "/" + path;
      return base + path;
    }

    // Headers must be a FLAT object: { "Key": "value" }
    function httpGet(apiPath) {
      const headers = {
        "Authorization": "PVEAPIToken=" + apiToken,
        "Accept":        "application/json"
      };

      try {
        // buildUrl can throw on a non-string path, so keep it inside the try.
        const url = buildUrl("/api2/json" + apiPath);
        host.server.logger(4, "proxmox_api GET " + url);
        // host.http.request returns {status, headers, body}; body is the string.
        const resp = host.http.request({ method: "GET", url, headers });

        if (resp.status < 200 || resp.status >= 300) {
          host.server.logger(2, "proxmox_api HTTP error " + resp.status + " for " + url);
          return {
            success: false,
            status:  resp.status,
            error:   "HTTP " + resp.status
          };
        }

        try {
          const parsed = JSON.parse(resp.body);
          return {
            success: true,
            status:  resp.status,
            data:    parsed.data !== undefined ? parsed.data : parsed
          };
        } catch (parseErr) {
          return { success: false, status: resp.status, error: "response was not JSON" };
        }
      } catch (err) {
        const msg = (err as any)?.message || String(err);
        host.server.logger(1, "proxmox_api error: " + msg);
        return { success: false, error: msg };
      }
    }

    function requireField(name) {
      if (!params[name]) {
        throw new Error("Missing required field: " + name + " for mode=" + params.mode);
      }
    }

    const mode = params.mode;

    // ROUTING BY MODE (all read-only)
    if (mode === "list_vms") {
      // QEMU VMs across cluster
      return httpGet("/cluster/resources?type=vm");

    } else if (mode === "list_lxcs") {
      // LXCs across cluster
      return httpGet("/cluster/resources?type=container");

    } else if (mode === "list_all_instances") {
      // All VM-like resources (VMs + LXCs) — Proxmox uses type=vm for both qemu and lxc
      return httpGet("/cluster/resources?type=vm");

    } else if (mode === "get_vm") {
      requireField("node");
      requireField("vmid");
      return httpGet(
        "/nodes/" + encodeURIComponent(params.node) +
        "/qemu/" + encodeURIComponent(params.vmid) + "/config"
      );

    } else if (mode === "get_lxc") {
      requireField("node");
      requireField("vmid");
      return httpGet(
        "/nodes/" + encodeURIComponent(params.node) +
        "/lxc/" + encodeURIComponent(params.vmid) + "/config"
      );

    } else if (mode === "instance_status") {
      requireField("node");
      requireField("vmid");
      requireField("type");
      const t = params.type;
      if (t !== "qemu" && t !== "lxc") {
        throw new Error("type must be 'qemu' or 'lxc' for instance_status");
      }
      return httpGet(
        "/nodes/" + encodeURIComponent(params.node) +
        "/" + t + "/" + encodeURIComponent(params.vmid) + "/status/current"
      );

    } else if (mode === "cluster_resources") {
      // All cluster resources (nodes, storage, VMs, etc.)
      return httpGet("/cluster/resources");

    } else if (mode === "cluster_status") {
      // Cluster health/status
      return httpGet("/cluster/status");

    } else if (mode === "node_resources") {
      requireField("node");
      // Node summary: CPU, memory, etc.
      return httpGet("/nodes/" + encodeURIComponent(params.node) + "/status");

    } else if (mode === "raw") {
      requireField("path");
      // Allow arbitrary /api2/json path for advanced use
      return httpGet(params.path);

    } else {
      throw new Error("Unsupported mode: " + mode);
    }
  }
};

module.exports = plugin;
