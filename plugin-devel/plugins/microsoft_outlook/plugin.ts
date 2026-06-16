"use strict";
const plugin = {
  name:        "outlook_365",
  description: "Microsoft Outlook 365.",
  version: "1.1.0",
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
    let apiUrl, apiToken;
    try {
      apiUrl =
        host.process.env("OUTLOOK_API_URL") ||
        host.config.options.ApiUrl ||
        "https://outlook.office365.com/api/v2.0/";
      apiToken =
        host.process.env("OUTLOOK_API_TOKEN") ||
        host.config.options.ApiToken ||
        undefined;
    } catch (err) {
      return { success: false, error: `Failed to load Outlook 365 configuration: ${err.message}` };
    }

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
        return listMicrosoftOutlookLabels(params, apiUrl, apiToken);
      case "list_messages":
        return listMicrosoftOutlookMessages(params, apiUrl, apiToken);
      case "read_message":
        return readMicrosoftOutlookMessage(params, apiUrl, apiToken);
      case "search_messages":
        return searchMicrosoftOutlookMessages(params, apiUrl, apiToken);
      default:
        return { success: false, error: `Unknown CommandEvent: ${params.CommandEvent}` };
    }
  },
};

function listMicrosoftOutlookLabels(params: Record<string, unknown>, apiUrl: string, apiToken: string) {
  try {
    const url = `${apiUrl}/users/me/labels?access_token=${apiToken}`;
    const response = host.http.get(url);
    return response;
  } catch (err) {
    return { success: false, error: `Failed to list labels: ${err.message}` };
  }
}

function listMicrosoftOutlookMessages(params: Record<string, unknown>, apiUrl: string, apiToken: string) {
  try {
    const url = `${apiUrl}/users/me/messages?access_token=${apiToken}&labelIds=${params.label}`;
    const response = host.http.get(url);
    return response;
  } catch (err) {
    return { success: false, error: `Failed to list messages: ${err.message}` };
  }
}

function readMicrosoftOutlookMessage(params: Record<string, unknown>, apiUrl: string, apiToken: string) {
  try {
    const url = `${apiUrl}/users/me/messages/${params.message_id}?access_token=${apiToken}`;
    const response = host.http.get(url);
    return response;
  } catch (err) {
    return { success: false, error: `Failed to read message: ${err.message}` };
  }
}

function searchMicrosoftOutlookMessages(params: Record<string, unknown>, apiUrl: string, apiToken: string) {
  try {
    const url = `${apiUrl}/users/me/messages?access_token=${apiToken}&search=${params.query}`;
    const response = host.http.get(url);
    return response;
  } catch (err) {
    return { success: false, error: `Failed to search messages: ${err.message}` };
  }
}

module.exports = plugin;
