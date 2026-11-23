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
});
