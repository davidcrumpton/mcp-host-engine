/// <reference path="../../types/mcphe.d.ts" />

/**
 * Session-scoped key-value memory tool for mcphe.
 * Optimized with Atomic Writes to prevent data corruption.
 *
 * Usage: enable by placing in your active plugins directory
 */

// Caps. Memory is a convenience store, not a database: an unbounded value makes
// every later call re-read and re-parse it, and every write re-serialise it.
const MAX_VALUE_BYTES = 64 * 1024;   // 64 KiB per value
const MAX_ENTRIES     = 1000;        // keys per session

// ── Small helpers ─────────────────────────────────────────────────────────────

function errText(err: any): string {
  return (err && err.message) || String(err);
}

/**
 * The configured data directory, or null when the operator has not set one.
 *
 * M12: this used to default to '/tmp/mcphe.' + host.pid -- a world-writable,
 * guessable location with no mode set, so another local user could pre-create
 * the directory and read or replace the store. There is no safe default for a
 * persistent data directory, so require an explicit one instead of inventing
 * one. host.config.options is also guarded: it is absent unless configured.
 */
function memoryDir(): string | null {
  const opts = (host.config && host.config.options) || ({} as Record<string, any>);
  const dir = (opts as Record<string, any>).data_dir;
  if (typeof dir !== "string" || dir.trim() === "") {
    return null;
  }
  return dir.replace(/\/+$/, "");
}

/**
 * Create the data directory if needed.
 *
 * M12: previously ran on every call outside any try/catch, so an existing
 * directory (or a permissions problem) threw a raw error out of call().
 * Returns an error string, or null on success.
 */
function ensureDataDir(dir: string): string | null {
  try {
    if (host.fs.exists(dir)) {
      return host.fs.isDir(dir) ? null : `data_dir exists but is not a directory: ${dir}`;
    }
    host.fs.makeDir(dir);
    return null;
  } catch (err) {
    // Tolerate the directory appearing between the check and the make.
    try {
      if (host.fs.exists(dir) && host.fs.isDir(dir)) return null;
    } catch (_e) { /* fall through to the error below */ }
    return `could not create data_dir ${dir}: ${errText(err)}`;
  }
}

/**
 * Session name -> a safe filename component.
 *
 * The session id reaches a filesystem path, so anything that can escape the
 * directory ('/', '..') must not survive. Anything outside [A-Za-z0-9._-] is
 * percent-encoded, which is reversible and collision-free.
 */
function safeSessionName(session: string): string {
  let out = "";
  for (let i = 0; i < session.length; i++) {
    const ch = session.charAt(i);
    out += /[A-Za-z0-9._-]/.test(ch) ? ch : "%" + ("0" + ch.charCodeAt(0).toString(16)).slice(-2).toUpperCase();
  }
  // A name made only of dots would still be ".." or "." to the OS.
  return /^\.+$/.test(out) ? out.replace(/\./g, "%2E") : out;
}

// ── Disk helpers ──────────────────────────────────────────────────────────────

/**
 * Load the store.
 *
 * M10: this used to catch every error and return {}, so a corrupt file silently
 * became an empty store -- and the next `set` wrote a one-key map over it,
 * destroying every prior entry. "Missing" and "unparseable" are now different
 * answers, and the caller refuses to write in the second case.
 */
function loadAll(memoryFile: string): { ok: boolean, data?: Record<string, any>, error?: string } {
  let exists = false;
  try {
    exists = host.fs.exists(memoryFile);
  } catch (err) {
    return { ok: false, error: `could not stat memory file: ${errText(err)}` };
  }
  if (!exists) {
    return { ok: true, data: {} };
  }

  let content = "";
  try {
    content = host.fs.readFile(memoryFile);
  } catch (err) {
    return { ok: false, error: `could not read memory file: ${errText(err)}` };
  }
  if (!content || content.trim() === "") {
    return { ok: true, data: {} };
  }

  let parsed;
  try {
    parsed = JSON.parse(content);
  } catch (err) {
    return {
      ok: false,
      error: `memory file is corrupt (invalid JSON) and will not be overwritten: ${errText(err)}`
    };
  }
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    return {
      ok: false,
      error: "memory file is corrupt (not a JSON object) and will not be overwritten"
    };
  }
  return { ok: true, data: parsed };
}

/**
 * Performs an Atomic Write.
 * 1. Write data to a .tmp file.
 * 2. Rename .tmp to the real file (atomic on most OSes).
 *
 * M9: this used to swallow every failure and return nothing, so `set` reported
 * success after a failed write and a later `get` returned nothing. Returns an
 * error string, or null on success, and the caller propagates it.
 */
function saveAll(data: Record<string, any>, memoryFile: string, tempFile: string): string | null {
  let json;
  try {
    // Compact JSON (M11): indenting inflated the file on every write.
    json = JSON.stringify(data);
  } catch (err) {
    return `could not serialise memory: ${errText(err)}`;
  }

  try {
    host.fs.writeFile(tempFile, json);
    host.fs.renameFile(tempFile, memoryFile);
    return null;
  } catch (err) {
    try { host.fs.deleteFile(tempFile); } catch (_e) { /* best effort */ }
    return `could not write memory file: ${errText(err)}`;
  }
}

const plugin = {
  name: "memory",
  description: "Store and retrieve named values that persist across invocations. Use action \"set\" to save a value, \"get\" to retrieve one, \"delete\" to remove one, and \"list\" to see all stored keys.",
  version: "1.2.0",
  commit: "none",
  Tags: ["memory"],
  annotations: {
    readOnlyHint:    false,
    destructiveHint: true,
    idempotentHint:  false,
    openWorldHint:   false,
  },
  inputSchema: {
    type: "object",
    properties: {
      action: { type: "string", enum: ["set", "get", "delete", "list"] },
      key: { type: "string" },
      value: {
        type: "string",
        description: `Value to store. Capped at ${MAX_VALUE_BYTES} bytes; at most ${MAX_ENTRIES} keys per session.`
      }
    },
    required: ["action"]
  },
  call(params: Record<string, unknown>) {
    const { action, key, value } = params;

    const dir = memoryDir();
    if (!dir) {
      return {
        success: false,
        error: "memory plugin requires an explicit data_dir. Set `options: { data_dir: \"/var/lib/mcphe/memory\" }` " +
               "in the memory plugin config. There is no default: a guessable /tmp path is world-writable."
      };
    }

    // H11: the Mcp-Session-Id header is the only value tied to the caller's
    // identity, and params.session_id used to override it unconditionally.
    // Session ids are frequently visible in earlier tool output, so enumeration
    // was realistic: {"action":"list","session_id":"<victim>"} returned the
    // victim's whole store. A supplied id must now agree with the header.
    const headers = (host.server && host.server.httpHeaders) || ({} as Record<string, string>);
    const headerSession = (headers as Record<string, string>)["Mcp-Session-Id"];
    const opts = (host.config && host.config.options) || ({} as Record<string, any>);
    const supplied = params.session_id;

    let currentSession: string;
    if (supplied !== undefined && supplied !== null && String(supplied) !== "") {
      const requested = String(supplied);
      if (headerSession && requested !== String(headerSession)) {
        return {
          success: false,
          error: "session_id does not match the session this request is authenticated as. " +
                 "Memory is partitioned per session; you cannot read or write another session's store."
        };
      }
      currentSession = requested;
    } else {
      currentSession = String(
        headerSession ||
        (opts as Record<string, any>).session_key ||
        "default"
      );
    }

    const fileBase    = `${dir}/${safeSessionName(currentSession)}_memory.json`;
    const memoryFile  = fileBase;
    const tempFile    = fileBase + ".tmp";

    const dirErr = ensureDataDir(dir);
    if (dirErr) {
      return { success: false, error: dirErr };
    }

    const loaded = loadAll(memoryFile);
    if (!loaded.ok) {
      return { success: false, error: loaded.error };
    }
    const ns = loaded.data as Record<string, any>;

    switch (action) {
      case 'set': {
        if (!key) return 'Error: "key" is required for action "set"'
        if (value === undefined || value === null)
          return 'Error: "value" is required for action "set"'

        const strValue = String(value);
        if (strValue.length > MAX_VALUE_BYTES) {
          return {
            success: false,
            error: `value is ${strValue.length} bytes; the limit is ${MAX_VALUE_BYTES}. ` +
                   `Store a reference instead of the whole payload.`
          };
        }

        const isNew = !(String(key) in ns);
        if (isNew && Object.keys(ns).length >= MAX_ENTRIES) {
          return {
            success: false,
            error: `session already holds ${MAX_ENTRIES} keys, which is the limit. ` +
                   `Delete some keys first.`
          };
        }

        ns[String(key)] = strValue;

        // M9: propagate a write failure instead of claiming success.
        const writeErr = saveAll(ns, memoryFile, tempFile);
        if (writeErr) {
          return { success: false, error: writeErr };
        }
        return {
          success: true,
          result: `Memory set: ${key} = ${value} (Session: ${currentSession})`
        }
      }
      case 'get': {
        if (!key) return 'Error: "key" is required for action "get"'
        if (!(String(key) in ns)) {
          return { success: false, error: `No memory found for key: ${key}` };
        }
        return {
          success: true,
          result: String(ns[String(key)])
        }
      }
      case 'delete': {
        if (!key) return 'Error: "key" is required for action "delete"'
        if (!(String(key) in ns)) return `No memory found for key: ${key}`
        delete ns[String(key)]

        const writeErr = saveAll(ns, memoryFile, tempFile);
        if (writeErr) {
          return { success: false, error: writeErr };
        }
        return {
          success: true,
          result: `Memory deleted: ${key} (Session: ${currentSession})`
        }
      }
      case 'list': {
        const entries = Object.entries(ns)
        if (entries.length === 0) return {
          success: true,
          result: `Memory is empty for session: ${currentSession}`
        }
        return {
          success: true,
          result: entries.map(([k, v]) => `${k}: ${v}`).join('\n')
        }
      }
      default:
        return {
          success: false,
          error: `Unknown action: ${action}. Valid actions: set | get | delete | list`
        }
    }
  },
};

module.exports = plugin;
