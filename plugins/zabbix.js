"use strict";
function zabbixErrText(err) {
  const e = err;
  if (e && typeof e.message === "string" && e.message) return e.message;
  return String(err);
}
function boundLimit(rpcParams, def, max) {
  const raw = rpcParams.limit;
  let n = typeof raw === "number" ? raw : parseInt(String(raw), 10);
  if (!isFinite(n) || isNaN(n) || n <= 0) {
    n = def;
  }
  if (n > max) {
    n = max;
  }
  rpcParams.limit = n;
  return rpcParams;
}
const DEFAULT_LIMIT = 100;
const FALLBACK_MAX_LIMIT = 1e3;
function maxLimit() {
  const opts = host.config.options || {};
  const v = parseInt(String(opts.MaxLimit), 10);
  return isFinite(v) && v > 0 ? v : FALLBACK_MAX_LIMIT;
}
const plugin = {
  name: "zabbix",
  description: "Zabbix 6+ MCP tools for monitoring, host management, items, triggers, and events.",
  version: "1.1.4",
  Tags: ["monitoring", "zabbix", "infrastructure"],
  annotations: {
    readOnlyHint: false,
    // M4: this plugin exposes delete_host and update_item, so the hint must
    // say so. These hints are permission signals surfaced to the model; an
    // inaccurate one is a safety defect, not metadata.
    destructiveHint: true,
    idempotentHint: false,
    openWorldHint: true
  },
  inputSchema: {
    type: "object",
    properties: {
      CommandEvent: {
        type: "string",
        description: "Zabbix API command to execute.",
        enum: [
          "login",
          "get_hosts",
          "get_items",
          "get_triggers",
          "get_events",
          "create_host",
          "update_host",
          "delete_host",
          "create_item",
          "update_item",
          "create_trigger",
          "update_trigger"
        ]
      },
      // Generic params bag for specific calls (hostids, filters, etc.)
      params: {
        type: "object",
        description: "Additional Zabbix API parameters for the selected CommandEvent."
      }
    },
    required: ["CommandEvent"]
  },
  call(params) {
    const self = module.exports;
    const { CommandEvent } = params;
    function envOr(key, fallback) {
      try {
        return host.process.env(key) || fallback;
      } catch (_e) {
        return fallback;
      }
    }
    let apiUrl;
    let username;
    let password;
    let authToken;
    try {
      const opts = host.config.options || {};
      apiUrl = envOr("ZABBIX_API_URL", "") || opts.ApiUrl || void 0;
      authToken = envOr("ZABBIX_AUTH_TOKEN", "") || opts.AuthToken || void 0;
      if (authToken) {
        username = void 0;
        password = void 0;
      } else {
        username = envOr("ZABBIX_USER", "") || opts.User || void 0;
        password = envOr("ZABBIX_PASSWORD", "") || opts.zabbixPassword || void 0;
      }
      host.server.logger(
        1,
        `Zabbix plugin called with CommandEvent=${CommandEvent}`
      );
      host.server.logger(
        1,
        `Zabbix plugin config: apiUrl=${apiUrl}, user=${username}, authToken=${authToken ? "***" : "MISSING"}`
      );
    } catch (err) {
      host.server.logger(1, `Zabbix plugin configuration error: ${zabbixErrText(err)}`);
      return {
        success: false,
        error: `Zabbix plugin configuration error: ${zabbixErrText(err)}`
      };
    }
    if (!apiUrl) {
      const errorMsg = "Missing Zabbix API URL in host.config.options.ApiUrl or ZABBIX_API_URL";
      host.server.logger(1, errorMsg);
      return { success: false, error: errorMsg };
    }
    function rpc(method, rpcParams = {}, useAuth = true) {
      var _a;
      const body = {
        jsonrpc: "2.0",
        method,
        params: rpcParams,
        id: Date.now()
      };
      if (useAuth && authToken) {
        body.auth = authToken;
      } else if (useAuth && !authToken) {
        body.auth = null;
      }
      const payload = JSON.stringify(body);
      try {
        const response = host.http.request({
          method: "POST",
          url: apiUrl,
          headers: {
            "Content-Type": "application/json",
            "User-Agent": "mcphe-zabbix-plugin/1.0 (goja)"
          },
          body: payload
        });
        const status = (_a = response.status) != null ? _a : response.statusCode;
        const bodyText = typeof response.body === "string" ? response.body : "";
        if (status >= 200 && status < 300) {
          let parsed;
          try {
            parsed = JSON.parse(bodyText);
          } catch (e) {
            return {
              success: false,
              error: `Failed to parse Zabbix response JSON: ${zabbixErrText(e)}`
            };
          }
          if (parsed.error) {
            return {
              success: false,
              error: parsed.error
            };
          }
          return {
            success: true,
            result: parsed.result
          };
        } else {
          return {
            success: false,
            status,
            error: `Zabbix API HTTP error ${status}`
          };
        }
      } catch (err) {
        return {
          success: false,
          error: `Zabbix API request error: ${zabbixErrText(err)}`
        };
      }
    }
    function doLogin() {
      if (!username || !password) {
        return {
          success: false,
          error: "Missing Zabbix credentials (zabbixUser/zabbixPassword or env ZABBIX_USER/ZABBIX_PASSWORD)"
        };
      }
      const res = rpc(
        "user.login",
        {
          username,
          password
        },
        false
        // login itself does not use auth
      );
      if (res.success && res.result) {
        authToken = res.result;
      }
      return res;
    }
    switch (CommandEvent) {
      case "login": {
        return doLogin();
      }
      case "get_hosts": {
        const p = params.params || {};
        const rpcParams = boundLimit(Object.assign({ output: "extend" }, p), DEFAULT_LIMIT, maxLimit());
        return rpc("host.get", rpcParams, true);
      }
      case "get_items": {
        const p = params.params || {};
        const rpcParams = boundLimit(
          Object.assign({ output: "extend", limit: DEFAULT_LIMIT, sortfield: "itemid", sortorder: "DESC" }, p),
          DEFAULT_LIMIT,
          maxLimit()
        );
        return rpc("item.get", rpcParams, true);
      }
      case "get_triggers": {
        const p = params.params || {};
        const rpcParams = boundLimit(
          Object.assign({ output: "extend", limit: DEFAULT_LIMIT, sortfield: "lastchange", sortorder: "DESC" }, p),
          DEFAULT_LIMIT,
          maxLimit()
        );
        return rpc("trigger.get", rpcParams, true);
      }
      case "get_events": {
        const p = params.params || {};
        const rpcParams = boundLimit(Object.assign({
          output: "extend",
          limit: DEFAULT_LIMIT,
          sortfield: "eventid",
          sortorder: "DESC"
        }, p), DEFAULT_LIMIT, maxLimit());
        return rpc("event.get", rpcParams, true);
      }
      case "create_host": {
        const p = params.params || {};
        return rpc("host.create", p, true);
      }
      case "update_host": {
        const p = params.params || {};
        return rpc("host.update", p, true);
      }
      case "delete_host": {
        const p = params.params || {};
        const rawIds = p.hostid !== void 0 && p.hostid !== null && p.hostid !== "" ? p.hostid : p.hostids;
        const hostids = Array.isArray(rawIds) ? rawIds : rawIds === void 0 || rawIds === null || rawIds === "" ? [] : [rawIds];
        if (!hostids.length) {
          return {
            success: false,
            error: "delete_host requires params.hostid or params.hostids"
          };
        }
        return rpc("host.delete", hostids, true);
      }
      case "create_item": {
        const p = params.params || {};
        return rpc("item.create", p, true);
      }
      case "update_item": {
        const p = params.params || {};
        return rpc("item.update", p, true);
      }
      case "create_trigger": {
        const p = params.params || {};
        return rpc("trigger.create", p, true);
      }
      case "update_trigger": {
        const p = params.params || {};
        return rpc("trigger.update", p, true);
      }
      default: {
        const errorMsg = `Unknown CommandEvent: ${CommandEvent}`;
        host.server.logger(1, errorMsg);
        return {
          success: false,
          error: errorMsg
        };
      }
    }
  }
};
module.exports = plugin;
