/// <reference path="../../types/mcphe.d.ts" />

const plugin = {
    name: "http_request_put",
    description: "Make an HTTP PUT request from the host.",
    version: "1.1.2",
    commit: "none",
    Tags: ["http", "utility"],
    annotations: {
        readOnlyHint:    false,
        destructiveHint: false,
        idempotentHint:  true,
        openWorldHint:   true,
    },
    inputSchema: {
        type: "object",
        properties: {
            url: { type: "string", description: "URL to send the PUT request to." },
            headers: {
                type: "object",
                additionalProperties: { type: "string" },
                description: "Optional headers to include in the request.",
            },
            body: { type: "string", description: "Request body to send." },
        },
        required: ["url", "body"],
    },
    call(params: Record<string, unknown>) {
        const response = host.http.put(params.url as string, params.headers as Record<string, string> || undefined, params.body as string);
        if (response.status !== 200) {
            console.log(`HTTP PUT request to ${params.url} failed with status ${response.status}`);
        } else {
            console.log(`HTTP PUT request to ${params.url} succeeded with status ${response.status}`);
        }
        return `Status: ${response.status}, Body: ${response.body}`;
    },
};

module.exports = plugin;
