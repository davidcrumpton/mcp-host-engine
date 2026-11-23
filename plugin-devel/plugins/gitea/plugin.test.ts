/**
 * gitea/plugin.test.ts
 * Metadata checks plus regressions for the header-shape and config-loading bugs.
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import { installMockHost, mockHTTPResponse } from "../mock-host";

let mockHost: ReturnType<typeof installMockHost>;

beforeEach(() => {
  vi.clearAllMocks();
  mockHost = installMockHost();
});

// Vitest transforms .ts natively — import the plugin directly
import * as pluginModule from "./plugin";
const plugin = pluginModule as unknown as { version: string, name: string; description: string; call: (p: Record<string, unknown>) => any };

function giteaHost(overrides: Record<string, unknown> = {}) {
  return installMockHost({
    allowedDomains: ["gitea.example.com"],
    allowedEnv: [],
    pluginConfig: {
      options: {
        giteaApiKey: "key-secret",
        giteaApiUrl: "https://gitea.example.com",
        ...overrides,
      },
    },
  });
}

describe("gitea plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("gitea");
    expect(plugin.description).toBeTruthy();
  });

  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });

  // Regression: H7 — headers were passed as headers.toString(), i.e. the string
  // "[object Object]". The host's type assertion then failed silently and the
  // request went out unauthenticated with no error raised.
  it("passes headers as an object, not a string", () => {
    const host = giteaHost();
    host.http.request.mockReturnValue(
      mockHTTPResponse({ status: 200, body: '{"ok":true,"data":[]}' })
    );

    plugin.call({ CommandEvent: "search_projects", search: "x" });

    const arg = host.http.request.mock.calls[0][0] as any;
    expect(typeof arg.headers).not.toBe("string");
    expect(arg.headers).toEqual(expect.objectContaining({
      Authorization: "Bearer key-secret",
    }));
  });

  // Regression: H9 — host.config.options was dereferenced unguarded while the
  // shipped config gives gitea no options block, so every call threw.
  it("works from config alone when no env vars are allow-listed", () => {
    const host = giteaHost();
    host.http.request.mockReturnValue(
      mockHTTPResponse({ status: 200, body: '{"ok":true,"data":[]}' })
    );

    expect(() => plugin.call({ CommandEvent: "search_projects", search: "x" })).not.toThrow();
    expect(host.http.request).toHaveBeenCalled();
  });

  it("returns a structured error, and does not throw, when no key is configured", () => {
    const host = installMockHost({
      allowedDomains: ["gitea.example.com"],
      allowedEnv: [],
      pluginConfig: {},
    });

    const res = plugin.call({ CommandEvent: "search_projects", search: "x" });
    expect(res.success).toBe(false);
    expect(host.http.request).not.toHaveBeenCalled();
  });

  // ---------------------------------------------------------------------
  // Regression: H8 -- success was decided by response.body.includes("ok"),
  // which was wrong in both directions. Status is the real signal.
  // ---------------------------------------------------------------------

  // The dangerous direction: an auth failure reported as success. Gitea's 401
  // body is {"message":"token is required"} and its 403 body is
  // {"message":"token does not have at least one of required scope(s)..."}.
  // Both contain "ok" inside "token", so the old check passed and the plugin
  // returned success:true for a write that never happened.
  it("reports an expired-token 401 as failure, despite 'ok' inside 'token'", () => {
    const host = giteaHost();
    host.http.request.mockReturnValue(
      mockHTTPResponse({ status: 401, body: '{"message":"token is required"}' })
    );

    const res = plugin.call({ CommandEvent: "create_issue", project_id: "o/r", title: "t" });

    expect(res.success).toBe(false);
    expect(res.error).toContain("401");
    expect(res.error).toContain("token is required");
  });

  it("reports an under-scoped 403 as failure, despite 'ok' inside 'token'", () => {
    const host = giteaHost();
    host.http.request.mockReturnValue(
      mockHTTPResponse({
        status: 403,
        body: '{"message":"token does not have at least one of required scope(s), required=[write:issue]"}',
      })
    );

    const res = plugin.call({ CommandEvent: "create_issue", project_id: "o/r", title: "t" });
    expect(res.success).toBe(false);
    expect(res.error).toContain("403");
  });

  // The other direction: Gitea's write endpoints return the created object
  // with no "ok" field at all (FileResponse = {content, commit}), so a
  // successful write used to be reported as a failure.
  it("reports a successful file write as success even though the body has no 'ok'", () => {
    const host = giteaHost();
    host.http.request.mockReturnValue(
      mockHTTPResponse({
        status: 201,
        body: '{"content":{"name":"a.txt","sha":"abc"},"commit":{"sha":"def"}}',
      })
    );

    const res = plugin.call({
      CommandEvent: "create_or_update_file",
      project_id: "o/r",
      file_path: "a.txt",
      content: "aGk=",
    });

    expect(res.success).toBe(true);
    expect(res.data).toEqual({ content: { name: "a.txt", sha: "abc" }, commit: { sha: "def" } });
  });

  it("treats a 200 as success for a search endpoint that does carry ok:true", () => {
    const host = giteaHost();
    host.http.request.mockReturnValue(
      mockHTTPResponse({ status: 200, body: '{"ok":true,"data":[{"id":1}],"total":1}' })
    );

    const res = plugin.call({ CommandEvent: "search_projects", search: "x" });
    expect(res.success).toBe(true);
  });

  it("does not throw when an error body is not JSON (e.g. a proxy HTML page)", () => {
    const host = giteaHost();
    host.http.request.mockReturnValue(
      mockHTTPResponse({ status: 502, body: "<html><body>502 Bad Gateway</body></html>" })
    );

    const res = plugin.call({ CommandEvent: "create_issue", project_id: "o/r", title: "t" });
    expect(res.success).toBe(false);
    expect(res.error).toContain("502");
  });

  it("reports a 2xx with an empty body as a failure rather than success", () => {
    const host = giteaHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 204, body: "" }));

    const res = plugin.call({ CommandEvent: "create_issue", project_id: "o/r", title: "t" });
    expect(res.success).toBe(false);
  });

  // createOrUpdateFile does a GET first to find an existing sha. A 404 there is
  // the normal create path, not an error worth parsing.
  it("creates a file when the pre-lookup returns 404", () => {
    const host = giteaHost();
    host.http.request
      .mockReturnValueOnce(mockHTTPResponse({ status: 404, body: '{"message":"not found"}' }))
      .mockReturnValueOnce(mockHTTPResponse({ status: 201, body: '{"content":{},"commit":{}}' }));

    const res = plugin.call({
      CommandEvent: "create_or_update_file",
      project_id: "o/r",
      file_path: "new.txt",
      content: "aGk=",
    });

    expect(res.success).toBe(true);
    expect(res.result).toContain("created");
    const putBody = JSON.parse((host.http.request.mock.calls[1][0] as any).body);
    expect(putBody.sha).toBeNull();
  });

  it("updates a file and carries the existing sha when the pre-lookup returns 200", () => {
    const host = giteaHost();
    host.http.request
      .mockReturnValueOnce(mockHTTPResponse({ status: 200, body: '{"sha":"existing-sha"}' }))
      .mockReturnValueOnce(mockHTTPResponse({ status: 200, body: '{"content":{},"commit":{}}' }));

    const res = plugin.call({
      CommandEvent: "create_or_update_file",
      project_id: "o/r",
      file_path: "a.txt",
      content: "aGk=",
    });

    expect(res.success).toBe(true);
    expect(res.result).toContain("updated");
    const putBody = JSON.parse((host.http.request.mock.calls[1][0] as any).body);
    expect(putBody.sha).toBe("existing-sha");
  });

  it("counts keys, not .length, when reporting how many files were pushed", () => {
    const host = giteaHost();
    host.http.request.mockReturnValue(
      mockHTTPResponse({ status: 201, body: '{"content":{},"commit":{}}' })
    );

    const res = plugin.call({
      CommandEvent: "push_files",
      project_id: "o/r",
      files: { "a.txt": "aGk=", "b.txt": "aGk=" },
    });

    expect(res.success).toBe(true);
    expect(res.result).toContain("Pushed 2 files");
  });
});
