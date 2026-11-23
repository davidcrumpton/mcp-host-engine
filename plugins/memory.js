"use strict";
const MAX_VALUE_BYTES = 64 * 1024;
const MAX_ENTRIES = 1e3;
function errText(err) {
  return err && err.message || String(err);
}
function memoryDir() {
  const opts = host.config && host.config.options || {};
  const dir = opts.data_dir;
  if (typeof dir !== "string" || dir.trim() === "") {
    return null;
  }
  return dir.replace(/\/+$/, "");
}
function ensureDataDir(dir) {
  try {
    if (host.fs.exists(dir)) {
      return host.fs.isDir(dir) ? null : `data_dir exists but is not a directory: ${dir}`;
    }
    host.fs.makeDir(dir);
    return null;
  } catch (err) {
    try {
      if (host.fs.exists(dir) && host.fs.isDir(dir)) return null;
    } catch (_e) {
    }
    return `could not create data_dir ${dir}: ${errText(err)}`;
  }
}
function safeSessionName(session) {
  let out = "";
  for (let i = 0; i < session.length; i++) {
    const ch = session.charAt(i);
    out += /[A-Za-z0-9._-]/.test(ch) ? ch : "%" + ("0" + ch.charCodeAt(0).toString(16)).slice(-2).toUpperCase();
  }
  return /^\.+$/.test(out) ? out.replace(/\./g, "%2E") : out;
}
function loadAll(memoryFile) {
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
function saveAll(data, memoryFile, tempFile) {
  let json;
  try {
    json = JSON.stringify(data);
  } catch (err) {
    return `could not serialise memory: ${errText(err)}`;
  }
  try {
    host.fs.writeFile(tempFile, json);
    host.fs.renameFile(tempFile, memoryFile);
    return null;
  } catch (err) {
    try {
      host.fs.deleteFile(tempFile);
    } catch (_e) {
    }
    return `could not write memory file: ${errText(err)}`;
  }
}
const plugin = {
  name: "memory",
  description: 'Store and retrieve named values that persist across invocations. Use action "set" to save a value, "get" to retrieve one, "delete" to remove one, and "list" to see all stored keys.',
  version: "1.2.0",
  commit: "none",
  Tags: ["memory"],
  annotations: {
    readOnlyHint: false,
    destructiveHint: true,
    idempotentHint: false,
    openWorldHint: false
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
  call(params) {
    const { action, key, value } = params;
    const dir = memoryDir();
    if (!dir) {
      return {
        success: false,
        error: 'memory plugin requires an explicit data_dir. Set `options: { data_dir: "/var/lib/mcphe/memory" }` in the memory plugin config. There is no default: a guessable /tmp path is world-writable.'
      };
    }
    const headers = host.server && host.server.httpHeaders || {};
    const headerSession = headers["Mcp-Session-Id"];
    const opts = host.config && host.config.options || {};
    const supplied = params.session_id;
    let currentSession;
    if (supplied !== void 0 && supplied !== null && String(supplied) !== "") {
      const requested = String(supplied);
      if (headerSession && requested !== String(headerSession)) {
        return {
          success: false,
          error: "session_id does not match the session this request is authenticated as. Memory is partitioned per session; you cannot read or write another session's store."
        };
      }
      currentSession = requested;
    } else {
      currentSession = String(
        headerSession || opts.session_key || "default"
      );
    }
    const fileBase = `${dir}/${safeSessionName(currentSession)}_memory.json`;
    const memoryFile = fileBase;
    const tempFile = fileBase + ".tmp";
    const dirErr = ensureDataDir(dir);
    if (dirErr) {
      return { success: false, error: dirErr };
    }
    const loaded = loadAll(memoryFile);
    if (!loaded.ok) {
      return { success: false, error: loaded.error };
    }
    const ns = loaded.data;
    switch (action) {
      case "set": {
        if (!key) return 'Error: "key" is required for action "set"';
        if (value === void 0 || value === null)
          return 'Error: "value" is required for action "set"';
        const strValue = String(value);
        if (strValue.length > MAX_VALUE_BYTES) {
          return {
            success: false,
            error: `value is ${strValue.length} bytes; the limit is ${MAX_VALUE_BYTES}. Store a reference instead of the whole payload.`
          };
        }
        const isNew = !(String(key) in ns);
        if (isNew && Object.keys(ns).length >= MAX_ENTRIES) {
          return {
            success: false,
            error: `session already holds ${MAX_ENTRIES} keys, which is the limit. Delete some keys first.`
          };
        }
        ns[String(key)] = strValue;
        const writeErr = saveAll(ns, memoryFile, tempFile);
        if (writeErr) {
          return { success: false, error: writeErr };
        }
        return {
          success: true,
          result: `Memory set: ${key} = ${value} (Session: ${currentSession})`
        };
      }
      case "get": {
        if (!key) return 'Error: "key" is required for action "get"';
        if (!(String(key) in ns)) {
          return { success: false, error: `No memory found for key: ${key}` };
        }
        return {
          success: true,
          result: String(ns[String(key)])
        };
      }
      case "delete": {
        if (!key) return 'Error: "key" is required for action "delete"';
        if (!(String(key) in ns)) return `No memory found for key: ${key}`;
        delete ns[String(key)];
        const writeErr = saveAll(ns, memoryFile, tempFile);
        if (writeErr) {
          return { success: false, error: writeErr };
        }
        return {
          success: true,
          result: `Memory deleted: ${key} (Session: ${currentSession})`
        };
      }
      case "list": {
        const entries = Object.entries(ns);
        if (entries.length === 0) return {
          success: true,
          result: `Memory is empty for session: ${currentSession}`
        };
        return {
          success: true,
          result: entries.map(([k, v]) => `${k}: ${v}`).join("\n")
        };
      }
      default:
        return {
          success: false,
          error: `Unknown action: ${action}. Valid actions: set | get | delete | list`
        };
    }
  }
};
module.exports = plugin;
