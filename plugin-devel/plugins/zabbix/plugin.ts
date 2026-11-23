/// <reference path="../../types/mcphe.d.ts" />

// L1: a goja-surfaced Go error may not be an Error, so `err.message` is
// undefined and the model receives "Zabbix API request error: undefined".
function zabbixErrText(err: unknown): string {
  const e = err as { message?: unknown } | null | undefined;
  if (e && typeof e.message === "string" && e.message) return e.message;
  return String(err);
}

/**
 * Clamp an agent-supplied `limit` AFTER the defaults have been merged.
 *
 * M6: Object.assign({limit: 100, ...}, p) let the caller override the bounding
 * default, so {limit: 10000000} -- or a negative, which Zabbix treats as
 * unlimited for some objects -- defeated it entirely. Re-applying the bound
 * after the merge is the fix; the default is only a starting point.
 *
 * The ceiling is operator-configurable (options.MaxLimit) so a large deployment
 * is not silently truncated at an arbitrary number chosen here.
 */
function boundLimit(rpcParams: Record<string, any>, def: number, max: number): Record<string, any> {
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
const FALLBACK_MAX_LIMIT = 1000;

/** Operator-configurable ceiling for a single RPC result set. */
function maxLimit(): number {
  const opts = (host.config.options || {}) as Record<string, any>;
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

  call(params: Record<string, unknown>) {
    const self = module.exports;
    const { CommandEvent } = params;

    // M1: host.process.env THROWS for keys outside allowed_env_vars rather than
    // returning undefined, so `env(k) || options.X` never reached the fallback.
    function envOr(key: string, fallback: string): string {
      try { return host.process.env(key) || fallback; } catch (_e) { return fallback; }
    }

    let apiUrl: string;
    let username: string | undefined;
    let password: string | undefined;
    let authToken: string | undefined;

    // Load config exactly like the GitLab plugin does
    try {
      const opts = (host.config.options || {}) as Record<string, any>;
      apiUrl =
        envOr("ZABBIX_API_URL", "") ||
        opts.ApiUrl ||
        undefined;

      authToken =
        envOr("ZABBIX_AUTH_TOKEN", "") ||
        opts.AuthToken ||
        undefined;

      // skip user and password when token is defined
      if (authToken) {
        username = undefined;
        password = undefined;
      } else {
        username =
          envOr("ZABBIX_USER", "") ||
          opts.User ||
          undefined;
        password =
          envOr("ZABBIX_PASSWORD", "") ||
          opts.zabbixPassword ||
          undefined;
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

    // Core JSON-RPC helper
    function rpc(method: string, rpcParams: Record<string, unknown> = {}, useAuth = true) {
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
          method: 'POST',
          url: apiUrl,
          headers: {
            "Content-Type": "application/json",
            "User-Agent": "mcphe-zabbix-plugin/1.0 (goja)"
          },
          body: payload
        });

        const status = response.status ?? response.statusCode;
        // L2: `response.text?.()` was dead code -- the host returns {status,
        // headers, body} and never a Response object, so the fallback never ran.
        const bodyText = typeof response.body === "string" ? response.body : "";

        if (status >= 200 && status < 300) {
          let parsed;
          try {
            parsed = JSON.parse(bodyText);
          } catch (e) {
            // L8: the raw body was echoed into the model's context. Zabbix
            // error pages carry host names and item keys; keep it server-side.
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
          // L8: status only; the upstream body stays out of the model's context.
          return {
            success: false,
            status: status,
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

    // Optional helper: perform login using configured username/password
    function doLogin() {
      if (!username || !password) {
        return {
          success: false,
          error:
            "Missing Zabbix credentials (zabbixUser/zabbixPassword or env ZABBIX_USER/ZABBIX_PASSWORD)"
        };
      }

      const res = rpc(
        "user.login",
        {
          username: username,
          password: password
        },
        false // login itself does not use auth
      );

      if (res.success && res.result) {
        authToken = res.result;
      }

      return res;
    }

    // Dispatcher
    switch (CommandEvent) {
      case "login": {
        return doLogin();
      }

      case "get_hosts": {
        const p = params.params || {};
        // Provide a sane default if none given, then bound it: previously this
        // had no limit at all and the caller's value went straight through.
        const rpcParams = boundLimit(Object.assign({ output: "extend" }, p), DEFAULT_LIMIT, maxLimit());
        return rpc("host.get", rpcParams, true);
      }

      case "get_items": {
        // limit like get_events

        const p = params.params || {};
        // can't sort by lastvalue and clock as they are not allowed
        const rpcParams = boundLimit(
          Object.assign({ output: "extend", limit: DEFAULT_LIMIT, sortfield: "itemid", sortorder: "DESC" }, p),
          DEFAULT_LIMIT, maxLimit());
        return rpc("item.get", rpcParams, true);
      }

      case "get_triggers": {
        const p = params.params || {};
        const rpcParams = boundLimit(
          Object.assign({ output: "extend", limit: DEFAULT_LIMIT, sortfield: "lastchange", sortorder: "DESC" }, p),
          DEFAULT_LIMIT, maxLimit());
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
        // Expect caller to provide host, groups, interfaces, etc. in params
        return rpc("host.create", p, true);
      }

      case "update_host": {
        const p = params.params || {};
        // Must include hostid in params
        return rpc("host.update", p, true);
      }

      case "delete_host": {
        const p = params.params || {};
        // Zabbix expects an array of hostids
        // L2: `.flat()` is ES2019; tsconfig targets ES2015 and esbuild does not
        // polyfill it, so this depended on goja implementing a method the build
        // does not guarantee. Unwrap by hand.
        const rawIds = p.hostid !== undefined && p.hostid !== null && p.hostid !== ""
          ? p.hostid
          : p.hostids;
        const hostids = Array.isArray(rawIds)
          ? rawIds
          : (rawIds === undefined || rawIds === null || rawIds === "" ? [] : [rawIds]);
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
