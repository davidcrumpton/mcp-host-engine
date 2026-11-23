/**
 * mock-host.ts — test double for the mcphe host global
 *
 * Injected as a global before each test via vitest.config.ts setupFiles.
 * Mirrors the real Go host behavior:
 *   - Domain blocking (throws if not in allowed list)
 *   - Path restrictions (throws if not in allowed list)
 *   - Synchronous returns only
 *
 * Tests override individual methods using vi.fn() or by replacing
 * mockHost.http.request directly on the global.
 */

import { vi } from "vitest";
import { realpathSync } from "node:fs";
import { dirname, basename, resolve as pathResolve } from "node:path";
import type {
  Host, HTTPResponse, HostFS, HostHTTP, HostProcess,
  HostExec, HostURL, HostUtils, HostServer, HostMCP,
  HTTPRequestOptions, URLSearchParamsObject, ParsedURL,
} from "../types/mcphe";

/**
 * L18: resolve symlinks before comparing paths, the way host/fs/fs.go does.
 * For a path that does not exist yet, resolve the parent and re-join the
 * basename, so a non-existent child of a symlinked directory still compares
 * against the same real root as the allow-list entry.
 */
function realish(p: string): string {
  try {
    return realpathSync(p);
  } catch {
    try {
      return pathResolve(realpathSync(dirname(p)), basename(p));
    } catch {
      return pathResolve(p);
    }
  }
}

// ---------------------------------------------------------------------------
// Default HTTP response factory
// ---------------------------------------------------------------------------

export function mockHTTPResponse(overrides: Partial<HTTPResponse> = {}): HTTPResponse {
  return {
    status: 200,
    headers: {},
    body: "",
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Mock URLSearchParams
// ---------------------------------------------------------------------------

function makeMockSearchParams(init = ""): URLSearchParamsObject {
  const params = new URLSearchParams(init.replace(/^\?/, ""));
  return {
    get: (name: string) => params.get(name),
    request: (name: string) => params.get(name),
    getAll: (name: string) => params.getAll(name),
    has: (name: string) => params.has(name),
    set: (name: string, value: string) => params.set(name, value),
    append: (name: string, value: string) => params.append(name, value),
    delete: (name: string) => params.delete(name),
    keys: () => [...params.keys()],
    entries: () => [...params.entries()],
    toString: () => params.toString(),
    size: [...params.keys()].length,
  };
}

// ---------------------------------------------------------------------------
// Mock ParsedURL
// ---------------------------------------------------------------------------

function makeMockParsedURL(href: string, base?: string): ParsedURL {
  // Honour the base argument, like Go's url.Parse/ResolveReference does. Without
  // this a protocol-relative href such as "//host/path?q=1" throws, and the
  // caller silently falls back to the raw value.
  const u = new URL(href, base);
  return {
    href: u.href,
    origin: u.origin,
    protocol: u.protocol,
    username: u.username,
    password: u.password,
    host: u.host,
    hostname: u.hostname,
    port: u.port,
    pathname: u.pathname,
    search: u.search,
    hash: u.hash,
    searchParams: makeMockSearchParams(u.search),
    toString: () => u.href,
  };
}

// ---------------------------------------------------------------------------
// Build the mock host
// ---------------------------------------------------------------------------

export function buildMockHost(options: {
  allowedDomains?: string[];
  allowedReadPaths?: string[];
  allowedWritePaths?: string[];
  allowedEnv?: string[];
  allowedCommands?: string[];
  allowedHeaders?: string[];
  env?: Record<string, string>;
  pluginConfig?: Record<string, unknown>;
} = {}): Host {
  const {
    allowedDomains  = ["example.com", "ifconfig.io", "en.wikipedia.org"],
    allowedEnv      = ["HOME", "PATH", "TMP", "TMPDIR"],
    env             = {},
    allowedReadPaths  = ["/tmp"],
    allowedWritePaths = ["/tmp"],
    allowedCommands   = [],
    allowedHeaders    = [],
    pluginConfig    = {},
  } = options;

  // Domain check mirrors Go isDomainAllowed
  function checkDomain(url: string) {
    let hostname: string;
    try { hostname = new URL(url).hostname; } catch { hostname = url; }
    const allowed = allowedDomains.some(
      d => hostname === d || hostname.endsWith("." + d)
    );
    if (!allowed) {
      throw new Error(`GoError: access to ${hostname} is not allowed`);
    }
  }

  /**
   * L18: this used to be a bare `startsWith`, so an allow-list entry of "/tmp"
   * also permitted "/tmpfoo" and "/tmp/../etc/passwd". host/fs/fs.go compares on
   * a directory boundary after resolving symlinks; mirror both halves or a test
   * passes here and the plugin is refused in production.
   */
  function isWithin(path: string, root: string): boolean {
    const resolvedPath = realish(path);
    const resolvedRoot = realish(root);
    if (resolvedPath === resolvedRoot) return true;
    const prefix = resolvedRoot.endsWith("/") ? resolvedRoot : resolvedRoot + "/";
    return resolvedPath.startsWith(prefix);
  }

  function checkReadPath(path: string) {
    const allowed = allowedReadPaths.some(p => isWithin(path, p));
    if (!allowed) throw new Error(`error: access to file "${path}" is not allowed`);
  }

  function checkWritePath(path: string) {
    const allowed = allowedWritePaths.some(p => isWithin(path, p));
    if (!allowed) throw new Error(`error: writing to file "${path}" is not allowed`);
  }

  /**
   * L18: `allowed_headers` was not modelled at all, so a plugin that sent a
   * header the operator never permitted passed its tests. The Go binding refuses
   * it (applyHeaders), so model that.
   */
  function checkHeaders(headers?: Record<string, string>) {
    if (!headers) return;
    for (const key of Object.keys(headers)) {
      // The Go side treats a "headers" container key as a wrapper, not a header.
      if (key === "headers") continue;
      const permitted = allowedHeaders.some(h => h.toLowerCase() === key.toLowerCase());
      if (!permitted) {
        throw new Error(`GoError: access to header "${key}" is not allowed`);
      }
    }
  }

  const http: HostHTTP = {
    get:     vi.fn((url: string, headers?: Record<string, string>) => { checkDomain(url); checkHeaders(headers); return mockHTTPResponse(); }),
    post:    vi.fn((url: string, headers?: Record<string, string>) => { checkDomain(url); checkHeaders(headers); return mockHTTPResponse(); }),
    put:     vi.fn((url: string, headers?: Record<string, string>) => { checkDomain(url); checkHeaders(headers); return mockHTTPResponse(); }),
    delete:  vi.fn((url: string, headers?: Record<string, string>) => { checkDomain(url); checkHeaders(headers); return mockHTTPResponse(); }),
    options: vi.fn((url: string, headers?: Record<string, string>) => { checkDomain(url); checkHeaders(headers); return mockHTTPResponse(); }),
    head:    vi.fn((url: string, headers?: Record<string, string>) => { checkDomain(url); checkHeaders(headers); return mockHTTPResponse(); }),
    // L17: accepts a bare URL string as well, because that is the one thing the
    // real binding refuses -- so a plugin written against the old positional
    // .d.ts fails here rather than in production.
    request: vi.fn((opts: HTTPRequestOptions | string): HTTPResponse => {
      const url = typeof opts === "string" ? opts : opts.url;
      checkDomain(url);
      // L18: headers were never inspected, so an unpermitted header passed.
      checkHeaders(typeof opts === "string" ? undefined : opts.headers);
      return mockHTTPResponse();
    }) as unknown as HostHTTP["request"],
    rawPost: vi.fn((url: string, headers?: Record<string, string>) => { checkDomain(url); checkHeaders(headers); return mockHTTPResponse(); }),
  };

  const fs: HostFS = {
    readFile:    vi.fn((p: string) => { checkReadPath(p);  return ""; }),
    writeFile:   vi.fn((p: string) => { checkWritePath(p); }),
    listFiles:   vi.fn((p: string) => { checkReadPath(p);  return []; }),
    deleteFile:  vi.fn((p: string) => { checkWritePath(p); }),
    renameFile:  vi.fn((o: string, n: string) => { checkWritePath(o); checkWritePath(n); }),
    makeDir:     vi.fn((p: string) => { checkWritePath(p); }),
    rmDir:       vi.fn((p: string) => { checkWritePath(p); }),
    isDir:       vi.fn((_p: string) => false),
    exists:      vi.fn((_p: string) => false),
    readStream:  vi.fn(),
    writeStream: vi.fn(),
  };

  const process: HostProcess = {
    env: vi.fn((key: string) => {
      if (!allowedEnv.includes(key)) {
        throw new Error(`GoError: access to env var "${key}" is not allowed`);
      }
      return env[key] ?? "";
    }),
    config: vi.fn(() => pluginConfig),
  };

  const exec: HostExec = {
    /**
     * L18: `runCommand` accepted anything, so command rejection -- one of the
     * host's main guardrails -- was untestable. The Go side splits the string
     * into an argv and requires argv[0] to be in `allowed_commands`.
     */
    runCommand: vi.fn((cmd: string) => {
      const argv = String(cmd).match(/"[^"]*"|'[^']*'|\S+/g) || [];
      const program = argv.length ? (argv[0] || "").replace(/^["']|["']$/g, "") : "";
      if (!allowedCommands.includes(program)) {
        throw new Error(`GoError: command "${program}" is not allowed`);
      }
      return "";
    }),
  };

  const url: HostURL = {
    parse:        (href: string, base?: string) => makeMockParsedURL(href, base),
    canParse:     (href: string) => { try { new URL(href); return true; } catch { return false; } },
    resolve:      (base: string, rel: string) => new URL(rel, base).href,
    searchParams: (init: string) => makeMockSearchParams(init),
    encode:       (s: string) => encodeURIComponent(s),
    decode:       (s: string) => decodeURIComponent(s),
  };

  const utils: HostUtils = {
    btoa: (s: string) => Buffer.from(s).toString("base64"),
    atob: (s: string) => Buffer.from(s, "base64").toString("utf-8"),
  };

  const logger = vi.fn((_level: number, _msg: string) => {});

  const server: HostServer = {
    version:     () => "test",
    commit:      () => "none",
    logger,
    config:      vi.fn(() => pluginConfig),
    httpHeaders: {},
  };

  const mcp: HostMCP = {
    version: () => "test",
    commit:  () => "none",
    logger,
    config:  () => pluginConfig,
  };

  return {
    fs, http, process, exec, url, utils, server, mcp,
    config:      pluginConfig,
    pid:         "99999",
    httpHeaders: {},
    logger,
    // Legacy aliases
    readFile:   fs.readFile,
    writeFile:  fs.writeFile,
    request:    http.request,
    httpGet:    http.get,
    httpPost:   http.post,
    httpPut:    http.put,
    httpDelete: http.delete,
    getEnv:     process.env,
    runCommand: exec.runCommand,
    makeDir:    fs.makeDir,
    rmDir:      fs.rmDir,
  };
}

// ---------------------------------------------------------------------------
// Install as global — called from vitest setupFiles
// ---------------------------------------------------------------------------

export function installMockHost(options?: Parameters<typeof buildMockHost>[0]): Host {
  const mockHost = buildMockHost(options);
  // @ts-expect-error — injecting global for goja plugin tests
  globalThis.host = mockHost;
  return mockHost;
}
