"use strict";
const plugin = {
  name:        "outlook_365",
  description: "Microsoft Outlook 365.",
  version: "1.1.2",
  commit:      "none",
  Tags:        ["office365", "email"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  true,
    openWorldHint:   true,
  },
  inputSchema: {
    type: "object",
    properties: {
      CommandEvent: {
        type: "string",
        description: "Command event to perform.",
        enum: ["list_labels", "list_messages", "read_message", "search_messages"]
      },
      label: { type: "string", description: "Label to list messages from." },
      message_id: { type: "string", description: "Message ID to read." },
      query: { type: "string", description: "Query to search messages with." },
    },
    required: ["CommandEvent"],
  },

  // call() must NOT be async. All host.* calls are synchronous — no await needed.
  call(params: Record<string, unknown>) {
    const opts = (host.config.options || {}) as Record<string, any>;

    // host.process.env THROWS for a key outside allowed_env_vars rather than
    // returning undefined, so treat a throw as "not configured via env" and
    // fall through to the config value.
    let envUrl = "";
    let envToken = "";
    try { envUrl = host.process.env("OUTLOOK_API_URL") || ""; } catch (envErr) { envUrl = ""; }
    try { envToken = host.process.env("OUTLOOK_API_TOKEN") || ""; } catch (envErr) { envToken = ""; }

    let apiUrl: string = envUrl || opts.ApiUrl || "https://outlook.office365.com/api/v2.0/";
    const apiToken: string | undefined = envToken || opts.ApiToken || undefined;

    if (!apiUrl) {
      return { success: false, error: "Missing Outlook 365 URL. Set options.ApiUrl in config or the OUTLOOK_API_URL environment variable." };
    }
    if (!apiToken) {
      return { success: false, error: "Missing Outlook 365 token. Set options.ApiToken in config or the OUTLOOK_API_TOKEN environment variable." };
    }

    // Strip trailing slash for consistent URL building
    apiUrl = apiUrl.replace(/\/$/, "");

    host.server.logger(3, `outlook_365: CommandEvent=${params.CommandEvent} apiUrl=${apiUrl}`);

    switch (params.CommandEvent) {
      case "list_labels":
        return outlookGet(apiUrl, "/users/me/labels", apiToken, "Failed to list labels");

      case "list_messages": {
        const label = outlookRequireStr(params, "label");
        if (!label) {
          return { success: false, error: "label is required for list_messages" };
        }
        return outlookGet(apiUrl, `/users/me/messages?labelIds=${encodeURIComponent(label)}`, apiToken, "Failed to list messages");
      }

      case "read_message": {
        const messageId = outlookRequireStr(params, "message_id");
        if (!messageId) {
          return { success: false, error: "message_id is required for read_message" };
        }
        return outlookGet(apiUrl, `/users/me/messages/${encodeURIComponent(messageId)}`, apiToken, "Failed to read message");
      }

      case "search_messages": {
        const query = outlookRequireStr(params, "query");
        if (!query) {
          return { success: false, error: "query is required for search_messages" };
        }
        return outlookGet(apiUrl, `/users/me/messages?search=${encodeURIComponent(query)}`, apiToken, "Failed to search messages");
      }

      default:
        return { success: false, error: `Unknown CommandEvent: ${params.CommandEvent}` };
    }
  },
};

function outlookRequireStr(params: Record<string, unknown>, name: string): string | null {
  const v = params[name];
  return typeof v === "string" && v !== "" ? v : null;
}

// host.http.request returns {status, headers, body} -- NOT a JSON string.
// Parsing the response object coerces it to "[object Object]" and throws.
// The token goes in an Authorization header, never in the query string.
function outlookGet(apiUrl: string, path: string, apiToken: string, label: string) {
  try {
    const response = host.http.request({
      method: "GET",
      url: apiUrl + path,
      headers: { "Authorization": `Bearer ${apiToken}` }
    });
    const status = response.status;
    if (status < 200 || status >= 300) {
      return { success: false, status: status, error: `${label}: HTTP ${status}` };
    }
    try {
      return { success: true, status: status, data: JSON.parse(response.body) };
    } catch (parseErr) {
      return { success: false, status: status, error: `${label}: response was not JSON` };
    }
  } catch (err) {
    const msg = (err as any)?.message || String(err);
    return { success: false, error: `${label}: ${msg}` };
  }
}

module.exports = plugin;
