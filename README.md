# MCP Host Engine (Go + JavaScript Plugins) Server

This repository contains a Go-based MCP server that runs tools as JavaScript plugins.  This allows versitility for the MCP server as many servers are product specific requiring one for GitHub, one for Google Docs, etc.  This allows you to plugin only what you need to create your own custom server  as a service  to be used by LLMs, AI agents, etc.  You can run the server on your local machine or on a remote server, and you can also run it as a Docker container.

## Features

- JSON-RPC 2.0 `/rpc` endpoint
- Pluggable tool support via `plugins/*.js`
- Configurable host, port, bearer auth, allowed domains, and enabled tools
- Built-in host APIs exposed to plugins (`readFile`, `runCommand`, `httpGet`, `getEnv`, `config`)

## Run

1. Install dependencies and build:

```bash
go mod tidy
make build
```

1. Start the server:

```bash
./mcphe config.yaml
```

1. Call the server via POST to `http://127.0.0.1:8000/rpc`.

## Plugin development

The `mcphe` server provides custom calls for the JavaScript plugin to use.  These custom calls are documented in the documentation for the `mcphe` server.  The documentation is available at `docs/index.html`.

Create a JavaScript plugin in `plugins/` with `module.exports = { name, description, inputSchema, call }`.
The `call` function receives the JSON arguments object and may return a string, object, array, or primitive.

Example tool:

```js
module.exports = {
  name: "ping",
  description: "A simple ping/pong tool for testing.",
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    return "pong";
  }
};
```
