"use strict";
const plugin = {
  name: "gmail",
  description: "List and read gmail",
  version: "1.1.2",
  commit: "none",
  Tags: ["gmail"],
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
        description: "The command to execute.",
        enum: [
          "list_labels",
          "list_messages",
          "read_message",
          "search_messages"
        ]
      },
      label: {
        type: "string",
        description: "The label to use."
      },
      message_id: {
        type: "string",
        description: "The message ID to use."
      },
      query: {
        type: "string",
        description: "The search query to use."
      }
    },
    required: ["CommandEvent"]
  },
  call(params) {
    const opts = host.config.options || {};
    let envUrl = "";
    let envToken = "";
    try {
      envUrl = host.process.env("GMAIL_API_URL") || "";
    } catch (envErr) {
      envUrl = "";
    }
    try {
      envToken = host.process.env("GMAIL_API_TOKEN") || "";
    } catch (envErr) {
      envToken = "";
    }
    let apiUrl = envUrl || opts.ApiUrl || "https://gmail.googleapis.com/gmail/v1";
    const apiToken = envToken || opts.ApiToken || void 0;
    if (!apiUrl) {
      return { success: false, error: "Missing Gmail URL. Set options.ApiUrl in config or the GMAIL_API_URL environment variable." };
    }
    if (!apiToken) {
      return { success: false, error: "Missing Gmail token. Set options.ApiToken in config or the GMAIL_API_TOKEN environment variable." };
    }
    apiUrl = apiUrl.replace(/\/$/, "");
    host.server.logger(3, `gmail: CommandEvent=${params.CommandEvent} apiUrl=${apiUrl}`);
    const get = (path, label) => {
      try {
        const response = host.http.request({
          method: "GET",
          url: apiUrl + path,
          headers: { "Authorization": `Bearer ${apiToken}` }
        });
        const status = response.status;
        if (status < 200 || status >= 300) {
          return { success: false, status, error: `${label}: HTTP ${status}` };
        }
        try {
          return { success: true, status, data: JSON.parse(response.body) };
        } catch (parseErr) {
          return { success: false, status, error: `${label}: response was not JSON` };
        }
      } catch (err) {
        const msg = (err == null ? void 0 : err.message) || String(err);
        return { success: false, error: `${label}: ${msg}` };
      }
    };
    const requireStr = (name) => {
      const v = params[name];
      return typeof v === "string" && v !== "" ? v : null;
    };
    switch (params.CommandEvent) {
      case "list_labels":
        return get("/users/me/labels", "Failed to list labels");
      case "list_messages": {
        const label = requireStr("label");
        if (!label) {
          return { success: false, error: "label is required for list_messages" };
        }
        return get(`/users/me/messages?labelIds=${encodeURIComponent(label)}`, "Failed to list messages");
      }
      case "read_message": {
        const messageId = requireStr("message_id");
        if (!messageId) {
          return { success: false, error: "message_id is required for read_message" };
        }
        return get(`/users/me/messages/${encodeURIComponent(messageId)}`, "Failed to read message");
      }
      case "search_messages": {
        const query = requireStr("query");
        if (!query) {
          return { success: false, error: "query is required for search_messages" };
        }
        return get(`/users/me/messages?q=${encodeURIComponent(query)}`, "Failed to search messages");
      }
      default:
        return { success: false, error: `Unknown CommandEvent: ${params.CommandEvent}` };
    }
  }
};
module.exports = plugin;
