/**
 * gitlab/plugin.test.ts
 * Metadata checks plus regressions for the token-logging and header-shape bugs.
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

const TOKEN = "glpat-secret-do-not-log";

function gitlabHost(overrides: Record<string, unknown> = {}) {
  return installMockHost({
    allowedDomains: ["gitlab.example.com"],
    // Allow-list the env vars (unset, so they resolve to "") so the plugin
    // proceeds down the normal path instead of bailing out of its try/catch.
    // Without this the buggy lines below are unreachable and the tests are
    // vacuous.
    allowedEnv: ["GITLAB_API_KEY", "GITLAB_API_URL"],
    env: {},
    pluginConfig: {
      options: {
        gitlabApiKey: TOKEN,
        gitlabBaseUrl: "https://gitlab.example.com",
        ...overrides,
      },
    },
  });
}

describe("gitlab plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("gitlab");
    expect(plugin.description).toBeTruthy();
  });

  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });

  // Regression: H1 — a duplicate config block logged the raw API token.
  it("never writes the API token to the log", () => {
    const host = gitlabHost();
    host.httpPost.mockReturnValue(mockHTTPResponse({ status: 201, body: "{}" }));

    plugin.call({ CommandEvent: "create_issue", project_id: "a/b", title: "t" });

    const logged = host.server.logger.mock.calls.map((c: unknown[]) => String(c[1]));
    expect(logged.length).toBeGreaterThan(0);
    for (const line of logged) {
      expect(line).not.toContain(TOKEN);
    }
  });

  // Regression: H2 — writes nested the headers under a "headers" key, which
  // defeated the host's name-based redaction and logged the token verbatim.
  it("passes a flat header map to write calls, with no nested headers key", () => {
    const host = gitlabHost();
    host.httpPost.mockReturnValue(mockHTTPResponse({ status: 201, body: "{}" }));

    plugin.call({ CommandEvent: "create_issue", project_id: "a/b", title: "t" });

    const headers = host.httpPost.mock.calls[0][1] as any;
    expect(headers.headers).toBeUndefined();
    expect(headers.Authorization).toBe(`Bearer ${TOKEN}`);
  });

  it("returns a structured failure on a non-2xx status", () => {
    const host = gitlabHost();
    host.httpPost.mockReturnValue(mockHTTPResponse({ status: 403, body: "denied" }));

    const res = plugin.call({ CommandEvent: "create_issue", project_id: "a/b", title: "t" });
    expect(res.success).toBe(false);
  });

  // Guard, not a proven regression: the original unguarded `config.options`
  // read sat inside the try/catch, so an absent options block was caught and
  // returned an equivalent error. This test pins the behaviour so a future
  // refactor that moves the read outside the guard is caught.
  it("does not throw when the options block is absent", () => {
    installMockHost({
      allowedDomains: ["gitlab.example.com"],
      allowedEnv: ["GITLAB_API_KEY", "GITLAB_API_URL"],
      env: {},
      pluginConfig: {},
    });

    const res = plugin.call({ CommandEvent: "search_projects", search: "x" });
    expect(res.success).toBe(false);
    expect(res.error).toBeTruthy();
  });
});
