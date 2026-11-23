"use strict";
const plugin = {
  name: "proxmox_api",
  description: "Query Proxmox for LXCs, VMs, and cluster/node information.",
  version: "1.1.3",
  commit: "none",
  Tags: ["infrastructure", "proxmox", "virtualization"],
  annotations: {
    readOnlyHint: true,
    destructiveHint: false,
    idempotentHint: true,
    openWorldHint: true
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
  call(params) {
    const cfg = host.server.config() || {};
    let envBaseUrl = "";
    let envApiToken = "";
    try {
      envBaseUrl = host.process.env("PROXMOX_BASE_URL") || "";
    } catch (envErr) {
      envBaseUrl = "";
    }
    try {
      envApiToken = host.process.env("PROXMOX_API_TOKEN") || "";
    } catch (envErr) {
      envApiToken = "";
    }
    const baseUrl = envBaseUrl || cfg.base_url;
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
    function httpGet(apiPath) {
      const headers = {
        "Authorization": "PVEAPIToken=" + apiToken,
        "Accept": "application/json"
      };
      try {
        const url = buildUrl("/api2/json" + apiPath);
        host.server.logger(4, "proxmox_api GET " + url);
        const resp = host.http.request({ method: "GET", url, headers });
        if (resp.status < 200 || resp.status >= 300) {
          host.server.logger(2, "proxmox_api HTTP error " + resp.status + " for " + url);
          return {
            success: false,
            status: resp.status,
            error: "HTTP " + resp.status
          };
        }
        try {
          const parsed = JSON.parse(resp.body);
          return {
            success: true,
            status: resp.status,
            data: parsed.data !== void 0 ? parsed.data : parsed
          };
        } catch (parseErr) {
          return { success: false, status: resp.status, error: "response was not JSON" };
        }
      } catch (err) {
        const msg = (err == null ? void 0 : err.message) || String(err);
        host.server.logger(1, "proxmox_api error: " + msg);
        return { success: false, error: msg };
      }
    }
    function requireField(name) {
      if (!params[name]) {
        throw new Error("Missing required field: " + name + " for mode=" + params.mode);
      }
    }
    function sanitiseRawPath(rawPath) {
      const path = String(rawPath);
      if (/[\u0000-\u001f\u007f]/.test(path)) {
        throw new Error("mode=raw path contains control characters");
      }
      if (path.indexOf("#") !== -1 || path.indexOf("?") !== -1) {
        throw new Error("mode=raw path must not contain '#' or '?'; pass the path only");
      }
      const segments = path.split("/").filter(function(s) {
        return s.length > 0;
      });
      if (!segments.length) {
        throw new Error("mode=raw path is empty");
      }
      const out = [];
      for (let i = 0; i < segments.length; i++) {
        let seg;
        try {
          seg = decodeURIComponent(segments[i]);
        } catch (e) {
          throw new Error("mode=raw path has an invalid percent-encoding: " + segments[i]);
        }
        if (seg === "." || seg === "..") {
          throw new Error("mode=raw path must not contain '.' or '..' segments");
        }
        if (seg.indexOf("/") !== -1 || seg.indexOf("\\") !== -1) {
          throw new Error("mode=raw path segment must not contain a slash: " + seg);
        }
        out.push(encodeURIComponent(seg));
      }
      return "/" + out.join("/");
    }
    const mode = params.mode;
    if (mode === "list_vms") {
      return httpGet("/cluster/resources?type=vm");
    } else if (mode === "list_lxcs") {
      return httpGet("/cluster/resources?type=container");
    } else if (mode === "list_all_instances") {
      return httpGet("/cluster/resources?type=vm");
    } else if (mode === "get_vm") {
      requireField("node");
      requireField("vmid");
      return httpGet(
        "/nodes/" + encodeURIComponent(params.node) + "/qemu/" + encodeURIComponent(params.vmid) + "/config"
      );
    } else if (mode === "get_lxc") {
      requireField("node");
      requireField("vmid");
      return httpGet(
        "/nodes/" + encodeURIComponent(params.node) + "/lxc/" + encodeURIComponent(params.vmid) + "/config"
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
        "/nodes/" + encodeURIComponent(params.node) + "/" + t + "/" + encodeURIComponent(params.vmid) + "/status/current"
      );
    } else if (mode === "cluster_resources") {
      return httpGet("/cluster/resources");
    } else if (mode === "cluster_status") {
      return httpGet("/cluster/status");
    } else if (mode === "node_resources") {
      requireField("node");
      return httpGet("/nodes/" + encodeURIComponent(params.node) + "/status");
    } else if (mode === "raw") {
      requireField("path");
      if (cfg.allow_raw_paths !== true) {
        return {
          success: false,
          error: 'mode="raw" is disabled. It forwards an arbitrary path to the Proxmox API under the plugin\'s token, so an operator must opt in with `allow_raw_paths: true` in the proxmox plugin config. Prefer one of the specific modes.'
        };
      }
      return httpGet(sanitiseRawPath(params.path));
    } else {
      throw new Error("Unsupported mode: " + mode);
    }
  }
};
module.exports = plugin;
