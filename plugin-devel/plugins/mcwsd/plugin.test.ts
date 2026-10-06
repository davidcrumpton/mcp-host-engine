/**
 * mcwsd/plugin.test.ts
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import { installMockHost, mockHTTPResponse } from "../mock-host";

let mockHost: ReturnType<typeof installMockHost>;

const BASE = "https://docker-vm.crumpton.org:9080";

beforeEach(() => {
  vi.clearAllMocks();
  mockHost = installMockHost({
    allowedDomains: ["docker-vm.crumpton.org"],
    allowedEnv: ["MCWSD_BASE_URL", "MCWSD_TOKEN"],
    env: { MCWSD_BASE_URL: BASE, MCWSD_TOKEN: "test-token" },
  });
});

import * as pluginModule from "./plugin";
const plugin = pluginModule as unknown as {
  name: string;
  description: string;
  version: string;
  call: (p: Record<string, unknown>) => any;
};

describe("mcwsd plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("mcwsd");
    expect(plugin.description).toBeTruthy();
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });

  it("errors when no token is configured", () => {
    installMockHost({
      allowedDomains: ["docker-vm.crumpton.org"],
      allowedEnv: ["MCWSD_BASE_URL", "MCWSD_TOKEN"],
      env: { MCWSD_BASE_URL: BASE, MCWSD_TOKEN: "" },
    });
    const res = plugin.call({ CommandEvent: "workspace_status", workspace_id: "w1" });
    expect(res.success).toBe(false);
    expect(res.error).toMatch(/token/i);
  });

  it("rejects an unknown CommandEvent", () => {
    const res = plugin.call({ CommandEvent: "nope" });
    expect(res).toEqual({ success: false, error: "Unknown CommandEvent: nope" });
  });

  // ── prepare_workspace ───────────────────────────────────────────────────

  it("prepares a workspace", () => {
    vi.mocked(mockHost.http.post).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ workspace_id: "12345", os: "linux" }) })
    );
    const res = plugin.call({
      CommandEvent: "prepare_workspace",
      repo_url: "https://github.com/org/repo.git",
      ref: "main",
    });
    expect(res).toEqual({ success: true, result: { workspace_id: "12345", os: "linux" } });
    expect(mockHost.http.post).toHaveBeenCalledWith(
      `${BASE}/workspaces`,
      expect.objectContaining({ Authorization: "Bearer test-token" }),
      JSON.stringify({ repo_url: "https://github.com/org/repo.git", ref: "main" })
    );
  });

  it("prepares a blank workspace without a repo URL", () => {
    vi.mocked(mockHost.http.post).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ workspace_id: "blank-1", os: "linux" }) })
    );
    const res = plugin.call({ CommandEvent: "prepare_workspace" });
    expect(res).toEqual({ success: true, result: { workspace_id: "blank-1", os: "linux" } });
    expect(mockHost.http.post).toHaveBeenCalledWith(
      `${BASE}/workspaces`,
      expect.objectContaining({ Authorization: "Bearer test-token" }),
      JSON.stringify({ ref: "main" })
    );
  });

  it("forwards env_overrides when provided", () => {
    vi.mocked(mockHost.http.post).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ workspace_id: "1" }) })
    );
    plugin.call({
      CommandEvent: "prepare_workspace",
      repo_url: "https://github.com/org/repo.git",
      env_overrides: { language: "node", install_command: "npm ci" },
    });
    const body = JSON.parse(vi.mocked(mockHost.http.post).mock.calls[0][2] as string);
    expect(body.env_overrides).toEqual({ language: "node", install_command: "npm ci" });
  });

  // ── exec_command ────────────────────────────────────────────────────────

  it("executes a command", () => {
    vi.mocked(mockHost.http.post).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ stdout: "ok", exit_code: 0 }) })
    );
    const res = plugin.call({
      CommandEvent: "exec_command",
      workspace_id: "w1",
      command: "go test ./...",
      allow_network: false,
      timeout_seconds: 30,
      secrets: ["TOKEN"],
    });
    expect(res).toEqual({ success: true, result: { stdout: "ok", exit_code: 0 } });
    const [url, , body] = vi.mocked(mockHost.http.post).mock.calls[0];
    expect(url).toBe(`${BASE}/workspaces/w1/exec`);
    expect(JSON.parse(body as string)).toEqual({
      command: "go test ./...",
      timeout_seconds: 30,
      secrets: ["TOKEN"],
      allow_network: false,
    });
  });

  it("requires allow_network for exec_command", () => {
    const res = plugin.call({ CommandEvent: "exec_command", workspace_id: "w1", command: "ls" });
    expect(res).toEqual({
      success: false,
      error: "allow_network (boolean) is mandatory for exec_command.",
    });
  });

  it("uses the configured default timeout when timeout_seconds is omitted", () => {
    vi.mocked(mockHost.http.post).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ stdout: "ok" }) })
    );
    plugin.call({ CommandEvent: "exec_command", workspace_id: "w1", command: "make", allow_network: false });
    const body = JSON.parse(vi.mocked(mockHost.http.post).mock.calls[0][2] as string);
    expect(body.timeout_seconds).toBe(180);
  });

  it("honors options.timeout_seconds for the default timeout", () => {
    const h = installMockHost({
      allowedDomains: ["docker-vm.crumpton.org"],
      allowedEnv: ["MCWSD_BASE_URL", "MCWSD_TOKEN"],
      env: { MCWSD_BASE_URL: BASE, MCWSD_TOKEN: "test-token" },
      pluginConfig: { options: { timeout_seconds: 240 } },
    });
    vi.mocked(h.http.post).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ stdout: "ok" }) })
    );
    plugin.call({ CommandEvent: "exec_command", workspace_id: "w1", command: "make", allow_network: false });
    const body = JSON.parse(vi.mocked(h.http.post).mock.calls[0][2] as string);
    expect(body.timeout_seconds).toBe(240);
  });

  // ── get_artifacts ───────────────────────────────────────────────────────

  it("fetches artifacts scoped to paths", () => {
    vi.mocked(mockHost.http.get).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ patch: "diff" }) })
    );
    const res = plugin.call({ CommandEvent: "get_artifacts", workspace_id: "w1", paths: "src,go.mod" });
    expect(res).toEqual({ success: true, result: { patch: "diff" } });
    expect(mockHost.http.get).toHaveBeenCalledWith(
      `${BASE}/workspaces/w1/artifacts?paths=src%2Cgo.mod`,
      expect.any(Object)
    );
  });

  // ── safe_edit ───────────────────────────────────────────────────────────

  it("performs a safe edit", () => {
    vi.mocked(mockHost.http.post).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ replacements: 1 }) })
    );
    const res = plugin.call({
      CommandEvent: "safe_edit",
      workspace_id: "w1",
      file_path: "/workspace/repo/main.go",
      pattern: "foo",
      replacement: "bar",
    });
    expect(res).toEqual({ success: true, result: { replacements: 1 } });
    expect(mockHost.http.post).toHaveBeenCalledWith(
      `${BASE}/workspaces/w1/safe-edit`,
      expect.any(Object),
      JSON.stringify({ file_path: "/workspace/repo/main.go", pattern: "foo", replacement: "bar" })
    );
  });

  // ── commit_and_pr ───────────────────────────────────────────────────────

  it("commits and opens a PR without a forge type", () => {
    vi.mocked(mockHost.http.post).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ branch: "wsd/abc", mr_url: "https://gitlab/mr/1" }) })
    );
    const res = plugin.call({
      CommandEvent: "commit_and_pr",
      workspace_id: "w1",
      commit_message: "fix bug",
      pr_title: "Fix bug",
      project_path: "group/project",
    });
    expect(res.success).toBe(true);
    const body = JSON.parse(vi.mocked(mockHost.http.post).mock.calls[0][2] as string);
    expect(body).toMatchObject({
      commit_message: "fix bug",
      pr_title: "Fix bug",
      project_path: "group/project",
    });
    expect(body).not.toHaveProperty("forge_type");
  });

  it("requires commit_message and pr_title for commit_and_pr", () => {
    const res = plugin.call({ CommandEvent: "commit_and_pr", workspace_id: "w1" });
    expect(res).toEqual({ success: false, error: "commit_message is required for commit_and_pr." });
  });

  // ── workspace_status / destroy_workspace ────────────────────────────────

  it("fetches workspace status", () => {
    vi.mocked(mockHost.http.get).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ status: "running" }) })
    );
    const res = plugin.call({ CommandEvent: "workspace_status", workspace_id: "w1" });
    expect(res).toEqual({ success: true, result: { status: "running" } });
    expect(mockHost.http.get).toHaveBeenCalledWith(`${BASE}/workspaces/w1/status`, expect.any(Object));
  });

  it("destroys a workspace", () => {
    vi.mocked(mockHost.http.delete).mockReturnValueOnce(mockHTTPResponse({ status: 204 }));
    const res = plugin.call({ CommandEvent: "destroy_workspace", workspace_id: "w1" });
    expect(res).toEqual({ success: true, result: "Workspace w1 destroyed." });
  });

  // ── file system ─────────────────────────────────────────────────────────

  it("lists files", () => {
    vi.mocked(mockHost.http.get).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ entries: [] }) })
    );
    const res = plugin.call({ CommandEvent: "list_files", workspace_id: "w1", path: "/workspace/repo" });
    expect(res).toEqual({ success: true, result: { entries: [] } });
    expect(mockHost.http.get).toHaveBeenCalledWith(
      `${BASE}/workspaces/w1/files?path=%2Fworkspace%2Frepo`,
      expect.any(Object)
    );
  });

  it("reads a file", () => {
    vi.mocked(mockHost.http.get).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ content: "hello" }) })
    );
    const res = plugin.call({ CommandEvent: "read_file", workspace_id: "w1", path: "/workspace/repo/main.go" });
    expect(res).toEqual({ success: true, result: { content: "hello" } });
  });

  it("writes a file", () => {
    vi.mocked(mockHost.http.put).mockReturnValueOnce(mockHTTPResponse({ status: 200 }));
    const res = plugin.call({
      CommandEvent: "write_file",
      workspace_id: "w1",
      path: "/workspace/repo/a.txt",
      content: "data",
    });
    expect(res).toEqual({ success: true, result: "File /workspace/repo/a.txt written successfully." });
    expect(mockHost.http.put).toHaveBeenCalledWith(
      `${BASE}/workspaces/w1/file`,
      expect.any(Object),
      JSON.stringify({ path: "/workspace/repo/a.txt", content: "data" })
    );
  });

  // ── background processes ────────────────────────────────────────────────

  it("starts a background process", () => {
    vi.mocked(mockHost.http.post).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ id: "p1", status: "running" }) })
    );
    const res = plugin.call({
      CommandEvent: "start_process",
      workspace_id: "w1",
      command: "npm run dev",
      allow_network: true,
    });
    expect(res).toEqual({ success: true, result: { id: "p1", status: "running" } });
    const body = JSON.parse(vi.mocked(mockHost.http.post).mock.calls[0][2] as string);
    expect(body).toEqual({ command: "npm run dev", allow_network: true });
  });

  it("lists processes", () => {
    vi.mocked(mockHost.http.get).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ processes: [] }) })
    );
    const res = plugin.call({ CommandEvent: "list_processes", workspace_id: "w1" });
    expect(res).toEqual({ success: true, result: { processes: [] } });
  });

  it("gets process status", () => {
    vi.mocked(mockHost.http.get).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ id: "p1", status: "exited" }) })
    );
    const res = plugin.call({ CommandEvent: "get_process_status", workspace_id: "w1", process_id: "p1" });
    expect(res).toEqual({ success: true, result: { id: "p1", status: "exited" } });
    expect(mockHost.http.get).toHaveBeenCalledWith(`${BASE}/workspaces/w1/processes/p1`, expect.any(Object));
  });

  it("sends process input", () => {
    vi.mocked(mockHost.http.post).mockReturnValueOnce(mockHTTPResponse({ status: 200 }));
    const res = plugin.call({
      CommandEvent: "send_process_input",
      workspace_id: "w1",
      process_id: "p1",
      input_text: "yes\n",
    });
    expect(res).toEqual({ success: true, result: "Input sent to process p1." });
    expect(mockHost.http.post).toHaveBeenCalledWith(
      `${BASE}/workspaces/w1/processes/p1/input`,
      expect.any(Object),
      JSON.stringify({ input: "yes\n" })
    );
  });

  it("kills a process", () => {
    vi.mocked(mockHost.http.delete).mockReturnValueOnce(mockHTTPResponse({ status: 200 }));
    const res = plugin.call({ CommandEvent: "kill_process", workspace_id: "w1", process_id: "p1" });
    expect(res).toEqual({ success: true, result: "Process p1 killed." });
    expect(mockHost.http.delete).toHaveBeenCalledWith(`${BASE}/workspaces/w1/processes/p1`, expect.any(Object));
  });

  // ── error handling ──────────────────────────────────────────────────────

  it("surfaces non-2xx responses", () => {
    vi.mocked(mockHost.http.get).mockReturnValueOnce(
      mockHTTPResponse({ status: 404, body: "not found" })
    );
    const res = plugin.call({ CommandEvent: "workspace_status", workspace_id: "w1" });
    expect(res).toEqual({ success: false, error: "HTTP 404", detail: "not found" });
  });

  it("reports an error when the domain is blocked", () => {
    installMockHost({
      allowedDomains: ["example.com"],
      allowedEnv: ["MCWSD_BASE_URL", "MCWSD_TOKEN"],
      env: { MCWSD_BASE_URL: BASE, MCWSD_TOKEN: "test-token" },
    });
    const res = plugin.call({ CommandEvent: "workspace_status", workspace_id: "w1" });
    expect(res.success).toBe(false);
    expect(res.error).toMatch(/not allowed/);
  });
});
