# MCP Server Hosting Engine (Go + JavaScript Plugins)

This repository contains a Go-based MCP server that runs tools as JavaScript plugins.

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
