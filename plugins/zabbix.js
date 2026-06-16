"use strict";
const plugin = {
  name: "zabbix",
  description: "Zabbix 6+ MCP tools for monitoring, host management, items, triggers, and events.",
  version: "1.1.0",
  Tags: ["monitoring", "zabbix", "infrastructure"],
  annotations: {
    readOnlyHint: false,
    destructiveHint: false,
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
    let apiUrl;
    let username;
    let password;
    let authToken;
    try {
      apiUrl = host.process.env("ZABBIX_API_URL") || host.config.options.ApiUrl || void 0;
      authToken = host.process.env("ZABBIX_AUTH_TOKEN") || host.config.options.AuthToken || void 0;
      authToken = host.process.env("ZABBIX_AUTH_TOKEN") || host.config.options.AuthToken || void 0;
      if (authToken) {
        username = void 0;
        password = void 0;
      } else {
        username = host.process.env("ZABBIX_USER") || host.config.options.User || void 0;
        password = host.process.env("ZABBIX_PASSWORD") || host.config.options.zabbixPassword || void 0;
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
      host.server.logger(1, `Zabbix plugin configuration error: ${err.message}`);
      return {
        success: false,
        error: `Zabbix plugin configuration error: ${err.message}`
      };
    }
    if (!apiUrl) {
      const errorMsg = "Missing Zabbix API URL in host.config.options.ApiUrl or ZABBIX_API_URL";
      host.server.logger(1, errorMsg);
      return { success: false, error: errorMsg };
    }
    function rpc(method, rpcParams = {}, useAuth = true) {
      var _a, _b, _c;
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
        const response = host.http.post(
          apiUrl,
          {
            "Content-Type": "application/json",
            "User-Agent": "mcphe-zabbix-plugin/1.0 (goja)"
          },
          payload
        );
        const status = (_a = response.status) != null ? _a : response.statusCode;
        const bodyText = typeof response.body === "string" ? response.body : (_c = (_b = response.text) == null ? void 0 : _b.call(response)) != null ? _c : "";
        if (status >= 200 && status < 300) {
          let parsed;
          try {
            parsed = JSON.parse(bodyText);
          } catch (e) {
            return {
              success: false,
              error: `Failed to parse Zabbix response JSON: ${e.message}, raw=${bodyText}`
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
            error: `Zabbix API HTTP error ${status}: ${bodyText}`
          };
        }
      } catch (err) {
        return {
          success: false,
          error: `Zabbix API request error: ${err.message}`
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
        const rpcParams = Object.assign({ output: "extend" }, p);
        return rpc("host.get", rpcParams, true);
      }
      case "get_items": {
        const p = params.params || {};
        const rpcParams = Object.assign({ output: "extend", limit: 100, sortfield: "itemid", sortorder: "DESC" }, p);
        return rpc("item.get", rpcParams, true);
      }
      case "get_triggers": {
        const p = params.params || {};
        const rpcParams = Object.assign({ output: "extend", limit: 100, sortfield: "lastchange", sortorder: "DESC" }, p);
        return rpc("trigger.get", rpcParams, true);
      }
      case "get_events": {
        const p = params.params || {};
        const rpcParams = Object.assign({
          output: "extend",
          limit: 100,
          sortfield: "eventid",
          sortorder: "DESC"
        }, p);
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
        const hostids = p.hostids || p.hostid ? [p.hostid || p.hostids].flat() : [];
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
