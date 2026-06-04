/**
 * Session-scoped key-value memory tool for llmctrlx.
 * Optimized with Atomic Writes to prevent data corruption.
 *
 * Usage: enable by placing in your active plugins directory
 */

// no else defaults for these constants. User must set these correctly.
// Note: We evaluate them inside call() now so they pick up per-request HTTP headers!

// ── Disk helpers ──────────────────────────────────────────────────────────────

function loadAll(memoryFile) {
  try {
    const content = host.readFile(memoryFile)
    return content ? JSON.parse(content) : {}
  } catch (err) {
    return {}
  }
}

/**
 * Performs an Atomic Write.
 * 1. Write data to a .tmp file.
 * 2. Rename .tmp to the real file (atomic on most OSes).
 */
function saveAll(data, memoryFile, tempFile) {
  try {
    const json = JSON.stringify(data, null, 2)
    host.writeFile(tempFile, json)
    host.renameFile(tempFile, memoryFile)
  } catch (err) {
    try { host.deleteFile(tempFile) } catch(e) {}
  }
}

module.exports = {
  name: "memory",
  description: "Store and retrieve named values that persist across invocations. Use action \"set\" to save a value, \"get\" to retrieve one, \"delete\" to remove one, and \"list\" to see all stored keys.",
  version: "1.0.0",
  commit: "none",
  Tags: ["memory"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  true,
    openWorldHint:   false,
  },
  inputSchema: { 
    type: "object", 
    properties: { 
      action: { type: "string", enum: ["set", "get", "delete", "list"] }, 
      key: { type: "string" }, 
      value: { type: "string" },
      session_id: { type: "string", description: "Optional session ID to partition memory. Defaults to global config session." }
    }, 
    required: ["action"] 
  },
  call(params) {
    const { action, key, value, session_id } = params;
    
    const MEMORY_DIR_BASE = host.config.options.data_dir || '/tmp/mcphe.' + host.pid;
    
    // Session priority: explicit param → MCP session header → config fallback → 'default'
    const currentSession = session_id
      || (host.httpHeaders && host.httpHeaders['Mcp-Session-Id'])
      || host.config.options.session_key
      || 'default';
    
    const memoryFile = `${MEMORY_DIR_BASE}/${currentSession}_memory.json`;
    const tempFile = `${MEMORY_DIR_BASE}/${currentSession}_memory.json.tmp`;
    
    createDataDir()
    let ns = loadAll(memoryFile);
    switch (action) {
      case 'set': {
        if (!key) return 'Error: "key" is required for action "set"'
        if (value === undefined || value === null)
          return 'Error: "value" is required for action "set"'
        ns[key] = String(value)
        saveAll(ns, memoryFile, tempFile)
        return `Memory set: ${key} = ${value} (Session: ${currentSession})`
      }
      case 'get': {
        if (!key) return 'Error: "key" is required for action "get"'
        return String(ns[key])
      }
      case 'delete': {
        if (!key) return 'Error: "key" is required for action "delete"'
        if (!(key in ns)) return `No memory found for key: ${key}`
        delete ns[key]
        saveAll(ns, memoryFile, tempFile)
        return `Memory deleted: ${key} (Session: ${currentSession})`
      }
      case 'list': {
        const entries = Object.entries(ns)
        if (entries.length === 0) return `Memory is empty for session: ${currentSession}`
        return entries.map(([k, v]) => `${k}: ${v}`).join('\n')
      }
      default:
        return `Unknown action: ${action}. Valid actions: set | get | delete | list`
    }
  },
};

// Helper to create data dir if not exists
function createDataDir() {
  const MEMORY_DIR_BASE = host.config.options.data_dir || '/tmp/mcphe.'+host.pid;
  host.makeDir(MEMORY_DIR_BASE)
}
