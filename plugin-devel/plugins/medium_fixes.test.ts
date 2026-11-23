/**
 * medium_fixes.test.ts
 *
 * Regression tests for the Medium findings in SECURITY_FINDINGS_mcphe-plugins.md
 * (plus the two High findings that shared the same files: H11 and H12).
 *
 * Each test asserts the behaviour that was wrong, so it fails against the old
 * code and passes against the fix.
 */

import { describe, it, expect, vi, beforeEach } from "vitest";
import { installMockHost, buildMockHost } from "./mock-host";

let mockHost: ReturnType<typeof installMockHost>;

beforeEach(() => {
  vi.clearAllMocks();
  mockHost = installMockHost();
});

import * as memoryModule from "./memory/plugin";
import * as elastic7Module from "./elastic7/plugin";
import * as giteaModule from "./gitea/plugin";
import * as proxmoxModule from "./proxmox/plugin";
import * as kibanaModule from "./kibana/plugin";
import * as zabbixModule from "./zabbix/plugin";
import * as websearchModule from "./websearch/plugin";

const memory = memoryModule as any;
const elastic7 = elastic7Module as any;
const gitea = giteaModule as any;
const proxmox = proxmoxModule as any;
const kibana = kibanaModule as any;
const zabbix = zabbixModule as any;
const websearch = websearchModule as any;

// ── M1: env-over-config must be reachable ────────────────────────────────────

describe("M1: env(x) || cfg.y falls back when the host denies access", () => {
  it("elastic7 uses config when the env var is not allow-listed", () => {
    // Default mock allow-list is HOME/PATH/TMP/TMPDIR, so ELASTIC_BASE_URL throws.
    const host = buildMockHost({
      pluginConfig: { base_url: "https://es.example.com", api_key: "cfg-key" },
    });
    (globalThis as any).host = host;

    let requestedUrl = "";
    (host.http.rawPost as any).mockImplementation((url: string) => {
      requestedUrl = url;
      return { status: 200, headers: {}, body: '{"hits":{"hits":[]}}' };
    });

    const res = elastic7.call({ index: "logs", query: {} });

    // Before the fix this threw "ELASTIC_BASE_URL and ELASTIC_API_KEY must be set".
    expect(res.success).not.toBe(false);
    expect(requestedUrl).toContain("https://es.example.com");
  });

  it("elastic7 still prefers a configured env var over config", () => {
    const host = buildMockHost({
      allowedEnv: ["ELASTIC_BASE_URL", "ELASTIC_API_KEY"],
      env: { ELASTIC_BASE_URL: "https://env.example.com", ELASTIC_API_KEY: "env-key" },
      pluginConfig: { base_url: "https://cfg.example.com", api_key: "cfg-key" },
    });
    (globalThis as any).host = host;

    let requestedUrl = "";
    (host.http.rawPost as any).mockImplementation((url: string) => {
      requestedUrl = url;
      return { status: 200, headers: {}, body: '{"hits":{"hits":[]}}' };
    });

    elastic7.call({ index: "logs", query: {} });
    expect(requestedUrl).toContain("https://env.example.com");
  });
});

// ── M3: half-configured credentials must not go out unauthenticated ─────────

describe("M3: kibana refuses a half-configured credential", () => {
  it("throws instead of sending the request with no Authorization header", () => {
    const host = buildMockHost({
      pluginConfig: {
        kibana_base_url: "https://kibana.example.com",
        kibana_username: "elastic", // password deliberately absent
      },
    });
    (globalThis as any).host = host;

    const requestSpy = host.http.request as any;
    requestSpy.mockReturnValue({ status: 200, headers: {}, body: "{}" });

    expect(() => kibana.call({ query: "*", indexName: "logs" })).toThrow(/both be set, or neither/);
    // The important part: no unauthenticated request went out at all.
    expect(requestSpy).not.toHaveBeenCalled();
  });

  it("sends Basic auth when both halves are present", () => {
    const host = buildMockHost({
      pluginConfig: {
        kibana_base_url: "https://kibana.example.com",
        kibana_username: "elastic",
        kibana_password: "hunter2",
      },
    });
    (globalThis as any).host = host;
    const requestSpy = host.http.request as any;
    requestSpy.mockReturnValue({ status: 200, headers: {}, body: '{"hits":{"hits":[]}}' });

    kibana.call({ query: "*", indexName: "logs" });
    const sent = requestSpy.mock.calls[0][0];
    expect(sent.headers.Authorization).toMatch(/^Basic /);
  });
});

// ── M5: gitea visibility ────────────────────────────────────────────────────

describe("M5: gitea never silently creates a public repository", () => {
  function giteaHost() {
    const host = buildMockHost({
      pluginConfig: { options: { giteaApiKey: "tok", giteaApiUrl: "https://gitea.example.com" } },
    });
    (globalThis as any).host = host;
    const requestSpy = host.http.request as any;
    requestSpy.mockReturnValue({
      status: 201,
      headers: {},
      body: JSON.stringify({ id: 1, full_name: "me/repo" }),
    });
    return { host, requestSpy };
  }

  it("rejects visibility:'internal' rather than creating a public repo", () => {
    const { requestSpy } = giteaHost();
    const res = gitea.call({ CommandEvent: "create_repository", name: "r", visibility: "internal" });

    expect(res.success).toBe(false);
    expect(res.error).toMatch(/internal/i);
    expect(requestSpy).not.toHaveBeenCalled();
  });

  it("defaults an omitted visibility to private, not public", () => {
    const { requestSpy } = giteaHost();
    gitea.call({ CommandEvent: "create_repository", name: "r" });

    const body = JSON.parse(requestSpy.mock.calls[0][0].body);
    // Before the fix `visibility === 'private'` was false when omitted -> public.
    expect(body.private).toBe(true);
  });

  it("still honours an explicit public request", () => {
    const { requestSpy } = giteaHost();
    gitea.call({ CommandEvent: "create_repository", name: "r", visibility: "public" });

    const body = JSON.parse(requestSpy.mock.calls[0][0].body);
    expect(body.private).toBe(false);
  });
});

// ── M6: zabbix limit bound survives the merge ──────────────────────────────

describe("M6: zabbix re-applies the limit bound after merging agent params", () => {
  function zabbixHost(options: Record<string, unknown> = {}) {
    const host = buildMockHost({
      pluginConfig: {
        options: { ApiUrl: "https://zabbix.example.com/api_jsonrpc.php", AuthToken: "t" },
      },
    });
    (globalThis as any).host = host;
    const requestSpy = host.http.request as any;
    requestSpy.mockReturnValue({ status: 200, headers: {}, body: JSON.stringify({ result: [] }) });
    return { host, requestSpy };
  }

  it("clamps an oversized limit", () => {
    const { requestSpy } = zabbixHost();
    zabbix.call({ CommandEvent: "get_events", params: { limit: 10000000 } });

    const sent = JSON.parse(requestSpy.mock.calls[0][0].body);
    expect(sent.params.limit).toBe(1000); // FALLBACK_MAX_LIMIT
  });

  it("replaces a negative limit (Zabbix treats some as unlimited)", () => {
    const { requestSpy } = zabbixHost();
    zabbix.call({ CommandEvent: "get_events", params: { limit: -1 } });

    const sent = JSON.parse(requestSpy.mock.calls[0][0].body);
    expect(sent.params.limit).toBe(100); // DEFAULT_LIMIT
  });

  it("keeps a sane caller-supplied limit", () => {
    const { requestSpy } = zabbixHost();
    zabbix.call({ CommandEvent: "get_events", params: { limit: 25 } });

    const sent = JSON.parse(requestSpy.mock.calls[0][0].body);
    expect(sent.params.limit).toBe(25);
  });
});

// ── M2: proxmox raw path ───────────────────────────────────────────────────

describe("M2: proxmox mode=raw is opt-in and traversal-safe", () => {
  function proxmoxHost(options: Record<string, unknown> = {}) {
    const host = buildMockHost({
      pluginConfig: {
        base_url: "https://pve.example.com:8006",
        api_token: "user@pam!tok=secret",
        ...options,
      },
    });
    (globalThis as any).host = host;
    const requestSpy = host.http.request as any;
    requestSpy.mockReturnValue({ status: 200, headers: {}, body: "{}" });
    return { host, requestSpy };
  }

  it("refuses raw paths unless the operator opts in", () => {
    const { requestSpy } = proxmoxHost();
    const res = proxmox.call({ mode: "raw", path: "/access/users" });

    expect(res.success).toBe(false);
    expect(res.error).toMatch(/allow_raw_paths/);
    expect(requestSpy).not.toHaveBeenCalled();
  });

  it("rejects a traversal path even when raw is enabled", () => {
    const { requestSpy } = proxmoxHost({ allow_raw_paths: true });

    expect(() => proxmox.call({ mode: "raw", path: "/../../etc/passwd" })).toThrow(/\.\./);
    expect(requestSpy).not.toHaveBeenCalled();
  });

  it("rejects an encoded traversal path", () => {
    const { requestSpy } = proxmoxHost({ allow_raw_paths: true });

    expect(() => proxmox.call({ mode: "raw", path: "/%2e%2e/%2e%2e/etc" })).toThrow(/\.\./);
    expect(requestSpy).not.toHaveBeenCalled();
  });

  it("allows a normal path when opted in", () => {
    const { requestSpy } = proxmoxHost({ allow_raw_paths: true });
    proxmox.call({ mode: "raw", path: "/nodes/pve/qemu" });

    expect(requestSpy.mock.calls[0][0].url).toContain("/api2/json/nodes/pve/qemu");
  });
});

// ── M13: websearch must use host.url, not `new URL` ────────────────────────

describe("M13: websearch de-redirects via host.url", () => {
  it("unwraps the duckduckgo redirect to the target URL", () => {
    const host = buildMockHost({
      allowedDomains: ["html.duckduckgo.com", "duckduckgo.com"],
    });
    (globalThis as any).host = host;

    const html =
      '<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage">' +
      "Example</a><span class=\"result__snippet\">snippet</span>";

    (host.http.request as any).mockReturnValue({ status: 200, headers: {}, body: html });

    const res = websearch.call({ query: "test", max_results: 5 });
    // Before the fix this was the raw //duckduckgo.com/l/?uddg=... wrapper.
    expect(res.results[0].url).toBe("https://example.com/page");
  });
});

// ── M9/M10/M11/M12/H11: memory ─────────────────────────────────────────────

describe("memory (M9-M12, H11)", () => {
  function memoryHost(options: Record<string, unknown> = {}) {
    const host = buildMockHost({
      pluginConfig: { options: { data_dir: "/tmp/mcphe-test" }, ...options },
    });
    (globalThis as any).host = host;
    return host;
  }

  it("M12: refuses to run without an explicit data_dir", () => {
    const host = buildMockHost({ pluginConfig: {} });
    (globalThis as any).host = host;

    const res = memory.call({ action: "set", key: "a", value: "b" });
    expect(res.success).toBe(false);
    expect(res.error).toMatch(/data_dir/);
  });

  it("M9: reports a write failure instead of claiming success", () => {
    const host = memoryHost();
    (host.fs.writeFile as any).mockImplementation(() => {
      throw new Error("disk full");
    });

    const res = memory.call({ action: "set", key: "a", value: "b" });
    expect(res.success).toBe(false);
    expect(res.error).toMatch(/disk full/);
  });

  it("M10: refuses to overwrite a corrupt store", () => {
    const host = memoryHost();
    (host.fs.exists as any).mockReturnValue(true);
    (host.fs.isDir as any).mockReturnValue(true);
    (host.fs.readFile as any).mockReturnValue("{not json");

    const res = memory.call({ action: "set", key: "a", value: "b" });
    expect(res.success).toBe(false);
    expect(res.error).toMatch(/corrupt/);
    // Critically: nothing was written over the damaged file.
    expect(host.fs.writeFile).not.toHaveBeenCalled();
  });

  it("M11: rejects a value over the size cap", () => {
    const host = memoryHost();
    (host.fs.exists as any).mockReturnValue(false);

    const res = memory.call({ action: "set", key: "a", value: "x".repeat(70 * 1024) });
    expect(res.success).toBe(false);
    expect(res.error).toMatch(/limit/);
  });

  it("H11: rejects a session_id that disagrees with the Mcp-Session-Id header", () => {
    const host = memoryHost();
    (host.server.httpHeaders as any)["Mcp-Session-Id"] = "session-a";

    const res = memory.call({ action: "list", session_id: "session-b" });
    expect(res.success).toBe(false);
    expect(res.error).toMatch(/does not match/);
  });

  it("H11: accepts a session_id that matches the header", () => {
    const host = memoryHost();
    (host.server.httpHeaders as any)["Mcp-Session-Id"] = "session-a";
    (host.fs.exists as any).mockReturnValue(false);

    const res = memory.call({ action: "list", session_id: "session-a" });
    expect(res.success).toBe(true);
  });

  it("H11: a traversal session id cannot escape the data directory", () => {
    const host = memoryHost();
    (host.fs.exists as any).mockReturnValue(false);

    memory.call({ action: "list", session_id: "../../etc/passwd" });

    const touched = (host.fs.exists as any).mock.calls.map((c: any[]) => String(c[0]));
    const files = touched.filter((p: string) => p.endsWith("_memory.json"));
    expect(files.length).toBeGreaterThan(0);
    for (const p of files) {
      // The real property: the session id contributes exactly one filename
      // component, so it cannot traverse. The encoded name keeps literal dots
      // but encodes the separators, which is what stops the escape.
      const rel = p.slice("/tmp/mcphe-test/".length);
      expect(rel).not.toContain("/");
      expect(rel).not.toContain("\\");
      expect(p.startsWith("/tmp/mcphe-test/")).toBe(true);
    }
  });
});

// ── M14: index allow-list ──────────────────────────────────────────────────

describe("M14: elastic7 refuses a wildcard index by default", () => {
  function esHost(options: Record<string, unknown> = {}) {
    const host = buildMockHost({
      pluginConfig: { base_url: "https://es.example.com", api_key: "k", ...options },
    });
    (globalThis as any).host = host;
    const requestSpy = host.http.rawPost as any;
    requestSpy.mockReturnValue({ status: 200, headers: {}, body: '{"hits":{"hits":[]}}' });
    return { host, requestSpy };
  }

  it("refuses {index:'*'}", () => {
    const { requestSpy } = esHost();
    expect(() => elastic7.call({ index: "*", query: {} })).toThrow(/expands to every index/);
    expect(requestSpy).not.toHaveBeenCalled();
  });

  it("honours allowed_index_patterns", () => {
    const { requestSpy } = esHost({ allowed_index_patterns: ["logs-*"] });

    expect(() => elastic7.call({ index: "secrets", query: {} })).toThrow(/not permitted/);
    expect(requestSpy).not.toHaveBeenCalled();

    elastic7.call({ index: "logs-2024", query: {} });
    expect(requestSpy).toHaveBeenCalled();
  });

  it("allows a plain index with no configuration", () => {
    const { requestSpy } = esHost();
    elastic7.call({ index: "logs", query: {} });
    // L6: the URL used to be `/<index>_search` -- no separator, so it addressed
    // an index literally named "logs_search". The endpoint is /{index}/_search.
    expect(String(requestSpy.mock.calls[0][0])).toContain("/logs/_search");
  });
});
