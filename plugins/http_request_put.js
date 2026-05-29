module.exports = {
    name: "http_request_put",
    description: "Make an HTTP PUT request from the host.",
    version: "1.0.0",
    commit: "none",
    Tags: ["http", "utility"],
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
    call(params) {
        const response = host.httpPut(params.url, params.headers || {}, params.body);
        return `Status: ${response.status}, Body: ${response.body}`;
    },
};