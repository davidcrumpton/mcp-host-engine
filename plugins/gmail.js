// MCP Gmail plugin
// List and read gmail
// Requires GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET environment variables

// I have not tested this yet since I don't use Gmail, but I don't see why this wouldn't work.

module.exports = {
  name: "gmail",
  description: "List and read gmail",
  version: "1.0.0",
  commit: "none",
  Tags: ["gmail"],
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
    let apiUrl, apiToken;
    try {
      apiUrl =
        host.process.env("GMAIL_API_URL") ||
        host.config.options.ApiUrl ||
        "https://gmail.googleapis.com/gmail/v1";
      apiToken =
        host.process.env("GMAIL_API_TOKEN") ||
        host.config.options.ApiToken ||
        undefined;
    } catch (err) {
      return { success: false, error: `Failed to load Gmail configuration: ${err.message}` };
    }

    if (!apiUrl) {
      return { success: false, error: "Missing Gmail URL. Set options.ApiUrl in config or the GMAIL_API_URL environment variable." };
    }
    if (!apiToken) {
      return { success: false, error: "Missing Gmail token. Set options.ApiToken in config or the GMAIL_API_TOKEN environment variable." };
    }

    // Strip trailing slash for consistent URL building
    apiUrl = apiUrl.replace(/\/$/, "");

    host.server.logger(3, `gmail: CommandEvent=${params.CommandEvent} apiUrl=${apiUrl}`);

    switch (params.CommandEvent) {
      case "list_labels":
        try {
          const url = `${apiUrl}/users/me/labels?access_token=${apiToken}`;
          const response = host.http.get(url);
          return response;
        } catch (err) {
          return { success: false, error: `Failed to list labels: ${err.message}` };
        }
      case "list_messages":
        try {
          const url = `${apiUrl}/users/me/messages?access_token=${apiToken}&labelIds=${params.label}`;
          const response = host.http.get(url);
          return response;
        } catch (err) {
          return { success: false, error: `Failed to list messages: ${err.message}` };
        }
      case "read_message":
        try {
          const url = `${apiUrl}/users/me/messages/${params.message_id}?access_token=${apiToken}`;
          const response = host.http.get(url);
          return response;
        } catch (err) {
          return { success: false, error: `Failed to read message: ${err.message}` };
        }
      case "search_messages":
        try {
          const url = `${apiUrl}/users/me/messages?access_token=${apiToken}&q=${params.query}`;
          const response = host.http.get(url);
          return response;
        } catch (err) {
          return { success: false, error: `Failed to search messages: ${err.message}` };
        }
      default:
        return { success: false, error: `Unknown CommandEvent: ${params.CommandEvent}` };
    }
  },
};
