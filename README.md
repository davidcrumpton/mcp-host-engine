# MCP Host Engine (Go + JavaScript Plugins) Server

![mcp robot](./images/mcphe-192x192.png)

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

1. Call the server via POST to `http://127.0.0.1:8001/rpc`.

## Configuration File Reference (`config.yaml`)

This section details the available settings for the MCP Host Engine. These values can be overridden in your `config.yaml` file to customize the server's behavior, network binding, and plugin security policies.

---

### Basic Server Settings

| Key | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `port` | `string` | `"8001"` | The network port the MCP Host Engine will listen on. |
| `host` | `string` | `"127.0.0.1"` | The network interface to bind the server to. Use `"0.0.0.0"` to listen on all interfaces. |
| `use_https` | `bool` | `false` | If set to `true`, the server will enforce HTTPS communication. |
| `cert_file` | `string` | *(none)* | Path to the SSL certificate file required when `use_https` is `true`. |
| `key_file` | `string` | *(none)* | Path to the private key file required when `use_https` is `true`. |
| `bearer_token` | `string` | *(none)* | A mandatory API key/token used for authenticating external client requests. |
| `pid_file` | `string` | *(none)* | The file path where the server's Process ID (PID) will be written upon startup. This is useful for process management. |

### Plugin and Tool Management

These settings control which plugins are available and what level of access they have.  Note that plugins are loaded at startup and are not dynamically reloadable.  If you change the plugins directory, you will need to restart the server.  If you change any of the other plugin settings, you will need to restart the server for the changes to take effect.  I

| Key | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `plugin_dir` | `string` | `"plugins"` | The local directory where the engine expects to find all JavaScript plugin files. |
| `tools` | `map[string]bool` | `{"ping": true, "wikipedia_search": true, ...}` | A boolean map controlling the activation state of core tools. Set a tool name to `false` to disable it, even if the plugin is present. |
| `plugins` | `map[string]map[string]interface{}` | *See Example* | Provides fine-grained, plugin-specific security and domain restrictions. The top-level key is the plugin name. |

### Security and Logging

| Key | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `verbosity_level` | `int` | `0` | The minimum logging level required for a message to be printed. Higher numbers mean more detailed logging (e.g., `3` might mean DEBUG level). |

---

### Advanced Plugin Restrictions (`plugins` Map)

The `plugins` map allows administrators to restrict the capabilities of individual plugins, enhancing security.

**Structure:**

```yaml
plugins:
  <plugin_name>:
    # Restrictions for this specific plugin
    allowed_domains: [...]
    allowed_write_file_paths: [...]
    allowed_read_file_paths: [...]
    allowed_commands: [...]
    allowed_env_vars: [...]
```

#### Detailed Restrictions

1. **`allowed_domains`**:
    * **Type:** `list<string>`
    * **Purpose:** Restricts which domains a plugin (like `wikipedia_search` or `google_search`) is allowed to connect to via HTTP requests.
    * **Example:**

        ```yaml
        plugins:
          wikipedia_search:
            allowed_domains: ["en.wikipedia.org"]
        ```

2. **`allowed_write_file_paths`**:
    * **Type:** `list<string>`
    * **Purpose:** A list of absolute paths where the plugin is *permitted* to write files. If a path is not listed, writing to it will fail.
    * **Example:**

        ```yaml
        plugins:
          my_tool:
            allowed_write_file_paths: ["/tmp/output/"]
        ```

3. **`allowed_read_file_paths`**:
    * **Type:** `list<string>`
    * **Purpose:** A list of absolute paths where the plugin is *permitted* to read files.
    * **Example:**

        ```yaml
        plugins:
          data_processor:
            allowed_read_file_paths: ["/mnt/data/input.csv"]
        ```

4. **`allowed_commands`**:
    * **Type:** `list<string>`
    * **Purpose:** Limits the commands that a plugin can execute via the `runCommand` method. Each item in the list should be a full command string (e.g., `"ls -l /tmp"`).
    * **Example:**

        ```yaml
        plugins:
          os_utility:
            allowed_commands: ["ls -l", "pwd"]
        ```

5. **`allowed_env_vars`**:
    * **Type:** `list<string>`
    * **Purpose:** Specifies which environment variables the plugin is allowed to access via `getEnv`.
    * **Example:**

        ```yaml
        plugins:
          api_client:
            allowed_env_vars: ["API_KEY", "USER_ID"]
        ```

---

### Example Usage

Here is an example demonstrating how to configure a dedicated analytics plugin (`analytics_reporter`) while keeping the built-in tools restricted.

```yaml
# config.yaml
port: "8080"
host: "127.0.0.1"
use_https: false
bearer_token: "super-secure-mcp-token"
plugins:
  # Security overrides for built-in tools
  wikipedia_search:
    allowed_domains: ["en.wikipedia.org"]
  
  google_search:
    allowed_domains: ["google.com"]
  
  # Custom plugin definition
  analytics_reporter:
    allowed_write_file_paths: ["/var/log/mcp_reports/"]
    allowed_read_file_paths: ["/app/data/metrics.csv"]
    allowed_commands: ["date"] # Only allow the 'date' command
    allowed_env_vars: ["REPORTING_ENV"]
    
tools:
  ping: true
  wikipedia_search: true
  google_search: true
  read_file: true # Explicitly enabling this tool
  run_command: true 
  date_time: true
  

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

## Caveats

This was designed and tested only on LMStudio at this time with `gemma-4-31b` and `gemma-4-26b-a4b-it`. Smaller gemma models had a hard time following instructions and with following security constraints.  Other larger models have had a hard time following instructions as well.  Please report your findings.

## Development

To build and deploy a Docker container, see `Dockerfile`.  To run the server locally, see `tests/README.md` for instructions on building and running the server locally.
