/// <reference path="../../types/mcphe.d.ts" />

/**
 * Home Assistant REST API plugin for mcphe.
 *
 * Configuration (in your config.yaml plugins section):
 *
 *   plugins:
 *     home_assistant:
 *       allowed_domains:
 *         - your-ha-host.local # or IP address
 *       allowed_env_vars:
 *         - "HASS_URL"
 *         - "HASS_TOKEN"
 *       options:
 *         ApiUrl: "http://your-ha-host.local:8123"
 *         ApiToken: "your-long-lived-access-token"
 *
 * The plugin also checks the environment variables HASS_URL and HASS_TOKEN
 * as fallbacks, which is convenient for development.
 */

const plugin = {
  name: "home_assistant",
  description: "Control and query Home Assistant via its REST API. Supports getting/setting entity states, calling services (turn lights on/off, etc.), firing events, listing calendars, rendering templates, and more.",
  version: "1.1.2",
  commit: "none",
  Tags: ["home-automation", "iot", "utility"],
  annotations: {
    readOnlyHint:    false,
    destructiveHint: false,
    idempotentHint:  false,
    openWorldHint:   true,
  },

  inputSchema: {
    type: "object",
    properties: {
      CommandEvent: {
        type: "string",
        description: "The Home Assistant REST API action to execute.",
        enum: [
          "ping",
          "get_config",
          "get_states",
          "get_state",
          "set_state",
          "delete_state",
          "get_services",
          "call_service",
          "get_events",
          "fire_event",
          "get_history",
          "get_logbook",
          "get_calendars",
          "get_calendar_events",
          "render_template",
          "get_error_log",
          "check_config"
        ]
      },

      // --- Entity / state ---
      entity_id: {
        type: "string",
        description: "Entity ID (e.g. 'light.living_room', 'sensor.temperature')."
      },
      state: {
        type: "string",
        description: "The state value to set (for set_state)."
      },
      attributes: {
        type: "object",
        description: "Optional state attributes to set alongside the state (for set_state).",
        additionalProperties: true
      },

      // --- Service calls ---
      domain: {
        type: "string",
        description: "Service domain (e.g. 'light', 'switch', 'script') for call_service."
      },
      service: {
        type: "string",
        description: "Service name (e.g. 'turn_on', 'turn_off', 'toggle') for call_service."
      },
      service_data: {
        type: "object",
        description: "Optional payload for call_service (e.g. {\"entity_id\": \"light.kitchen\", \"brightness\": 128}).",
        additionalProperties: true
      },
      return_response: {
        type: "boolean",
        description: "Set true for services that return data (e.g. weather.get_forecasts).",
        default: false
      },

      // --- Events ---
      event_type: {
        type: "string",
        description: "Event type string for fire_event (e.g. 'my_custom_event')."
      },
      event_data: {
        type: "object",
        description: "Optional JSON data payload to include with fire_event.",
        additionalProperties: true
      },

      // --- History / logbook ---
      start_time: {
        type: "string",
        description: "ISO 8601 timestamp for the beginning of the history/logbook period (e.g. '2024-01-01T00:00:00+00:00'). Defaults to 1 day ago."
      },
      end_time: {
        type: "string",
        description: "ISO 8601 timestamp for the end of the history/logbook period."
      },
      minimal_response: {
        type: "boolean",
        description: "For get_history: return only last_changed and state for intermediate states (faster).",
        default: false
      },
      no_attributes: {
        type: "boolean",
        description: "For get_history: omit attributes from the response (faster).",
        default: false
      },
      significant_changes_only: {
        type: "boolean",
        description: "For get_history: only return significant state changes.",
        default: false
      },

      // --- Calendar ---
      calendar_entity_id: {
        type: "string",
        description: "Calendar entity ID for get_calendar_events (e.g. 'calendar.personal')."
      },
      calendar_start: {
        type: "string",
        description: "Start timestamp for get_calendar_events (ISO 8601)."
      },
      calendar_end: {
        type: "string",
        description: "End timestamp for get_calendar_events (ISO 8601)."
      },

      // --- Template ---
      template: {
        type: "string",
        description: "Jinja2 template string to render (for render_template), e.g. '{{ states(\"sensor.temperature\") }}'."
      }
    },
    required: ["CommandEvent"]
  },

  call(params: Record<string, unknown>) {
    const self = module.exports;

    // ── Load configuration ────────────────────────────────────────────────
    let apiUrl: string, apiToken: string;
    try {
      apiUrl =
        host.process.env("HASS_URL") ||
        host.config.options.ApiUrl ||
        "https://homeassistant.local:8123";
      apiToken =
        host.process.env("HASS_TOKEN") ||
        host.config.options.ApiToken ||
        undefined;
    } catch (err) {
      return { success: false, error: `Failed to load Home Assistant configuration: ${err.message}` };
    }

    if (!apiUrl) {
      return { success: false, error: "Missing Home Assistant URL. Set options.ApiUrl in config or the HASS_URL environment variable." };
    }
    if (!apiToken) {
      return { success: false, error: "Missing Home Assistant token. Set options.ApiToken in config or the HASS_TOKEN environment variable." };
    }

    // Strip trailing slash for consistent URL building
    apiUrl = apiUrl.replace(/\/$/, "");

    host.server.logger(3, `home_assistant: CommandEvent=${params.CommandEvent} apiUrl=${apiUrl}`);

    // ── Shared request helpers ────────────────────────────────────────────
    const authHeaders = {
      "Authorization": `Bearer ${apiToken}`,
      "Content-Type": "application/json"
    };

    function get(path: string) {
      try {
        const resp: any = host.http.get(`${apiUrl}${path}`, { headers: authHeaders });
        return self._handleResponse(resp, path);
      } catch (err) {
        return { success: false, error: `GET ${path} failed: ${err.message}` };
      }
    }

    function post(path: string, body: unknown = null) {
      try {
        const resp = host.http.post(`${apiUrl}${path}`, authHeaders, body ? JSON.stringify(body) : "{}");
        return self._handleResponse(resp, path);
      } catch (err) {
        return { success: false, error: `POST ${path} failed: ${err.message}` };
      }
    }

    function del(path: string) {
      try {
        const resp = host.http.delete(`${apiUrl}${path}`, authHeaders);
        return self._handleResponse(resp, path);
      } catch (err) {
        return { success: false, error: `DELETE ${path} failed: ${err.message}` };
      }
    }

    // ── Command dispatch ──────────────────────────────────────────────────
    switch (params.CommandEvent) {

      // GET /api/ — health check
      case "ping": {
        return get("/api/");
      }

      // GET /api/config — current HA configuration
      case "get_config": {
        return get("/api/config");
      }

      // GET /api/states — all entity states
      case "get_states": {
        return get("/api/states");
      }

      // GET /api/states/<entity_id> — single entity state
      case "get_state": {
        if (!params.entity_id) {
          return { success: false, error: "entity_id is required for get_state." };
        }
        return get(`/api/states/${encodeURIComponent(params.entity_id)}`);
      }

      // POST /api/states/<entity_id> — create or update state
      case "set_state": {
        if (!params.entity_id) {
          return { success: false, error: "entity_id is required for set_state." };
        }
        if (params.state === undefined || params.state === null) {
          return { success: false, error: "state is required for set_state." };
        }
        const body = { state: params.state };
        if (params.attributes) {
          body.attributes = params.attributes;
        }
        return post(`/api/states/${encodeURIComponent(params.entity_id)}`, body);
      }

      // DELETE /api/states/<entity_id>
      case "delete_state": {
        if (!params.entity_id) {
          return { success: false, error: "entity_id is required for delete_state." };
        }
        return del(`/api/states/${encodeURIComponent(params.entity_id)}`);
      }

      // GET /api/services
      case "get_services": {
        return get("/api/services");
      }

      // POST /api/services/<domain>/<service>
      case "call_service": {
        if (!params.domain) return { success: false, error: "domain is required for call_service." };
        if (!params.service) return { success: false, error: "service is required for call_service." };
        const qs = params.return_response ? "?return_response" : "";
        const path = `/api/services/${encodeURIComponent(params.domain)}/${encodeURIComponent(params.service)}${qs}`;
        return post(path, params.service_data || {});
      }

      // GET /api/events
      case "get_events": {
        return get("/api/events");
      }

      // POST /api/events/<event_type>
      case "fire_event": {
        if (!params.event_type) {
          return { success: false, error: "event_type is required for fire_event." };
        }
        return post(`/api/events/${encodeURIComponent(params.event_type)}`, params.event_data || {});
      }

      // GET /api/history/period/<timestamp>?filter_entity_id=...
      case "get_history": {
        if (!params.entity_id) {
          return { success: false, error: "entity_id is required for get_history (used as filter_entity_id)." };
        }
        let path = "/api/history/period";
        if (params.start_time) {
          path += `/${encodeURIComponent(params.start_time)}`;
        }
        const qs = [`filter_entity_id=${encodeURIComponent(params.entity_id)}`];
        if (params.end_time)               qs.push(`end_time=${encodeURIComponent(params.end_time)}`);
        if (params.minimal_response)       qs.push("minimal_response");
        if (params.no_attributes)          qs.push("no_attributes");
        if (params.significant_changes_only) qs.push("significant_changes_only");
        return get(`${path}?${qs.join("&")}`);
      }

      // GET /api/logbook/<timestamp>
      case "get_logbook": {
        let path = "/api/logbook";
        if (params.start_time) {
          path += `/${encodeURIComponent(params.start_time)}`;
        }
        const qs = [];
        if (params.entity_id) qs.push(`entity=${encodeURIComponent(params.entity_id)}`);
        if (params.end_time)   qs.push(`end_time=${encodeURIComponent(params.end_time)}`);
        if (qs.length) path += `?${qs.join("&")}`;
        return get(path);
      }

      // GET /api/calendars
      case "get_calendars": {
        return get("/api/calendars");
      }

      // GET /api/calendars/<entity_id>?start=...&end=...
      case "get_calendar_events": {
        if (!params.calendar_entity_id) {
          return { success: false, error: "calendar_entity_id is required for get_calendar_events." };
        }
        if (!params.calendar_start || !params.calendar_end) {
          return { success: false, error: "calendar_start and calendar_end are required for get_calendar_events." };
        }
        const path = `/api/calendars/${encodeURIComponent(params.calendar_entity_id)}` +
          `?start=${encodeURIComponent(params.calendar_start)}&end=${encodeURIComponent(params.calendar_end)}`;
        return get(path);
      }

      // POST /api/template
      case "render_template": {
        if (!params.template) {
          return { success: false, error: "template is required for render_template." };
        }
        return post("/api/template", { template: params.template });
      }

      // GET /api/error_log
      case "get_error_log": {
        return get("/api/error_log");
      }

      // POST /api/config/core/check_config
      case "check_config": {
        return post("/api/config/core/check_config", null);
      }

      default: {
        return { success: false, error: `Unknown CommandEvent: ${params.CommandEvent}` };
      }
    }
  },

  // ── Internal response normaliser ────────────────────────────────────────
  _handleResponse(resp: any, path: string) {
    const status = resp.status ?? resp.statusCode;
    const bodyText = typeof resp.body === "string" ? resp.body : "";

    if (status >= 200 && status < 300) {
      // Some endpoints (error_log, delete) return plain text or empty bodies
      if (!bodyText || bodyText.trim() === "") {
        return { success: true, result: null };
      }
      try {
        return { success: true, result: JSON.parse(bodyText) };
      } catch (_) {
        // Plain-text response (e.g. error_log, rendered template)
        return { success: true, result: bodyText };
      }
    }

    host.server.logger(1, `home_assistant: ${path} → HTTP ${status}: ${bodyText}`);

    const errorMessages = {
      400: "Bad request — check your parameters.",
      401: "Unauthorized — is the API token correct?",
      404: "Not found — entity or endpoint does not exist.",
      405: "Method not allowed."
    };
    const hint = errorMessages[status] || "";
    return {
      success: false,
      error: `HTTP ${status}${hint ? ": " + hint : ""}`,
      detail: bodyText || undefined
    };
  }
};

module.exports = plugin;
