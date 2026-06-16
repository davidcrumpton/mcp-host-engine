/**
 * mcphe host type definitions
 *
 * goja runtime constraints — enforced by these types:
 *   - call() must NOT be async — return types are synchronous
 *   - No fetch, no URL constructor, no Node.js builtins
 *   - Headers are a flat { key: string } object, never nested under "headers:"
 *   - Use const self = module.exports inside call() if you need self-reference
 *   - host.http.get(url, headers) — headers is the second arg, flat object
 */

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

export interface HTTPResponse {
  status: number;
  headers: Record<string, string[]>;
  body: string;
}

export interface HostHTTP {
  get(url: string, headers?: Record<string, string>): HTTPResponse;
  post(url: string, headers: Record<string, string>, body: string): HTTPResponse;
  put(url: string, headers: Record<string, string>, body: string): HTTPResponse;
  delete(url: string, headers?: Record<string, string>): HTTPResponse;
  options(url: string, headers?: Record<string, string>): HTTPResponse;
  head(url: string, headers?: Record<string, string>): HTTPResponse;
  /** Like post but accepts an already-serialized string body */
  rawPost(url: string, headers: Record<string, string>, body: string): HTTPResponse;
  /**
   * Make an HTTP request with any HTTP method.
   * `body` can be a string or a byte array (ArrayBuffer or Uint8Array).
   * For binary bodies, set `Content-Type` in `headers` accordingly.
   */
  request(method: string, url: string, headers?: Record<string, string>, body?: string | ArrayBuffer | Uint8Array): HTTPResponse;
}

// ---------------------------------------------------------------------------
// Filesystem
// ---------------------------------------------------------------------------

export interface HostFS {
  readFile(path: string): string;
  writeFile(path: string, content: string): void;
  listFiles(path: string): string[];
  deleteFile(path: string): void;
  renameFile(oldPath: string, newPath: string): void;
  makeDir(path: string): void;
  rmDir(path: string): void;
  isDir(path: string): boolean;
  exists(path: string): boolean;
  readStream(path: string, options: Record<string, unknown>, callback: (chunk: string) => void): void;
  writeStream(path: string, options: Record<string, unknown>, callback: () => string): void;
}

// ---------------------------------------------------------------------------
// Process / exec
// ---------------------------------------------------------------------------

export interface HostProcess {
  env(key: string): string;
  config(): Record<string, unknown>;
}

export interface HostExec {
  runCommand(command: string): string;
}

// ---------------------------------------------------------------------------
// URL (browser-compatible API, Go stdlib backed)
// ---------------------------------------------------------------------------

export interface URLSearchParamsObject {
  get(name: string): string | null;
  getAll(name: string): string[];
  has(name: string): boolean;
  set(name: string, value: string): void;
  append(name: string, value: string): void;
  delete(name: string): void;
  keys(): string[];
  entries(): [string, string][];
  toString(): string;
  size: number;
}

export interface ParsedURL {
  href: string;
  origin: string;
  protocol: string;
  username: string;
  password: string;
  host: string;
  hostname: string;
  port: string;
  pathname: string;
  search: string;
  hash: string;
  searchParams: URLSearchParamsObject;
  toString(): string;
}

export interface HostURL {
  parse(href: string, base?: string): ParsedURL;
  canParse(href: string, base?: string): boolean;
  resolve(base: string, relative: string): string;
  searchParams(init: string): URLSearchParamsObject;
  encode(str: string): string;
  decode(str: string): string;
}

// ---------------------------------------------------------------------------
// Utils
// ---------------------------------------------------------------------------

export interface HostUtils {
  btoa(str: string): string;
  atob(str: string): string;
}

// ---------------------------------------------------------------------------
// Server / logging
// ---------------------------------------------------------------------------

export type LoggerFn = (level: number, message: string) => void;

export interface HostServer {
  version(): string;
  commit(): string;
  logger: LoggerFn;
  config(pluginName?: string): Record<string, unknown>;
  httpHeaders: Record<string, string>;
}

// ---------------------------------------------------------------------------
// MCP
// ---------------------------------------------------------------------------

export interface HostMCP {
  version(): string;
  commit(): string;
  logger: LoggerFn;
  config(): Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// Top-level host object
// ---------------------------------------------------------------------------

export interface Host {
  // Namespaced (current/preferred)
  fs: HostFS;
  http: HostHTTP;
  process: HostProcess;
  exec: HostExec;
  url: HostURL;
  utils: HostUtils;
  server: HostServer;
  mcp: HostMCP;

  // Plugin config shortcut (same as server.config(pluginName))
  config: Record<string, unknown>;

  // Informational
  pid: string;
  httpHeaders: Record<string, string>;
  logger: LoggerFn;

  // Legacy flat API — still works, but prefer namespaced above
  /** @deprecated use host.fs.readFile */
  readFile(path: string): string;
  /** @deprecated use host.fs.writeFile */
  writeFile(path: string, content: string): void;
  /** @deprecated use host.http.get */
  httpGet(url: string, headers?: Record<string, string>): HTTPResponse;
  /** @deprecated use host.http.post */
  httpPost(url: string, headers: Record<string, string>, body: string): HTTPResponse;
  /** @deprecated use host.http.put */
  httpPut(url: string, headers: Record<string, string>, body: string): HTTPResponse;
  /** @deprecated use host.http.delete */
  httpDelete(url: string, headers?: Record<string, string>): HTTPResponse;
  /** @deprecated use host.process.env */
  getEnv(key: string): string;
  /** @deprecated use host.exec.runCommand */
  runCommand(command: string): string;
  /** @deprecated use host.fs.makeDir */
  makeDir(path: string): void;
  /** @deprecated use host.fs.rmDir */
  rmDir(path: string): void;
}

// ---------------------------------------------------------------------------
// Plugin shape
// ---------------------------------------------------------------------------

export interface PluginAnnotations {
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
  idempotentHint?: boolean;
  openWorldHint?: boolean;
}

export interface JSONSchema {
  type: string;
  properties?: Record<string, JSONSchemaProperty>;
  required?: string[];
  additionalProperties?: boolean | JSONSchema;
}

export interface JSONSchemaProperty {
  type: string;
  description?: string;
  enum?: string[];
  default?: unknown;
  items?: JSONSchemaProperty;
  properties?: Record<string, JSONSchemaProperty>;
  minimum?: number;
  maximum?: number;
}

/**
 * Every mcphe plugin must export an object matching this interface.
 *
 * call() is intentionally synchronous — goja does not support async/await
 * unless the event loop extension is added. All host.* calls block and
 * return values directly.
 */
export interface MCPPlugin<TParams = Record<string, unknown>> {
  name: string;
  description: string;
  version: string;
  commit?: string;
  Tags?: string[];
  annotations?: PluginAnnotations;
  inputSchema: JSONSchema;
  /** Synchronous. Never async. Return value becomes the MCP tool result. */
  call(params: TParams): unknown;
}

// ---------------------------------------------------------------------------
// Globals injected by the mcphe runtime
// ---------------------------------------------------------------------------

declare global {
  const host: Host;

  // CommonJS module shim injected by the mcphe loader
  const module: { exports: unknown };
  let exports: unknown;
}
