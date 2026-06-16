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
 * mockHost.http.get directly on the global.
 */

import { vi } from "vitest";
import type {
  Host, HTTPResponse, HostFS, HostHTTP, HostProcess,
  HostExec, HostURL, HostUtils, HostServer, HostMCP,
  URLSearchParamsObject, ParsedURL,
} from "../types/mcphe";

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

function makeMockParsedURL(href: string): ParsedURL {
  const u = new URL(href);
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
  env?: Record<string, string>;
  pluginConfig?: Record<string, unknown>;
} = {}): Host {
  const {
    allowedDomains  = ["example.com", "ifconfig.io", "en.wikipedia.org"],
    allowedEnv      = ["HOME", "PATH", "TMP", "TMPDIR"],
    env             = {},
    allowedReadPaths  = ["/tmp"],
    allowedWritePaths = ["/tmp"],
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

  function checkReadPath(path: string) {
    const allowed = allowedReadPaths.some(p => path.startsWith(p));
    if (!allowed) throw new Error(`error: access to file "${path}" is not allowed`);
  }

  function checkWritePath(path: string) {
    const allowed = allowedWritePaths.some(p => path.startsWith(p));
    if (!allowed) throw new Error(`error: writing to file "${path}" is not allowed`);
  }

  const http: HostHTTP = {
    get:     vi.fn((url: string) => { checkDomain(url); return mockHTTPResponse(); }),
    post:    vi.fn((url: string) => { checkDomain(url); return mockHTTPResponse(); }),
    put:     vi.fn((url: string) => { checkDomain(url); return mockHTTPResponse(); }),
    delete:  vi.fn((url: string) => { checkDomain(url); return mockHTTPResponse(); }),
    options: vi.fn((url: string) => { checkDomain(url); return mockHTTPResponse(); }),
    head:    vi.fn((url: string) => { checkDomain(url); return mockHTTPResponse(); }),
    rawPost: vi.fn((url: string) => { checkDomain(url); return mockHTTPResponse(); }),
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
    runCommand: vi.fn((_cmd: string) => ""),
  };

  const url: HostURL = {
    parse:        (href: string) => makeMockParsedURL(href),
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
