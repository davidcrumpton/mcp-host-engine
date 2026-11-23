/**
 * gmail/plugin.test.ts
 * Complex plugin only tests plugin description for now.
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

const GMAIL_CFG = {
  ApiUrl: "https://gmail.example.com/gmail/v1",
  ApiToken: "tok-secret",
};

function gmailHost(overrides: Record<string, unknown> = {}) {
  return installMockHost({
    allowedDomains: ["gmail.example.com"],
    allowedEnv: [],
    // host.config.options is the plugin's options block, mirroring the real host.
    pluginConfig: { options: { ...GMAIL_CFG, ...overrides } },
  });
}

describe("gmail plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("gmail");
    expect(plugin.description).toBeTruthy();
  });

  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });

  // Regression: H3 — the token was in the query string, so it leaked into
  // proxy logs and into the error text returned to the model.
  it("sends the token in an Authorization header, never the query string", () => {
    const host = gmailHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: '{"labels":[]}' }));

    plugin.call({ CommandEvent: "list_labels" });

    const arg = host.http.request.mock.calls[0][0] as any;
    expect(arg.url).not.toContain("access_token");
    expect(arg.url).not.toContain("tok-secret");
    expect(arg.headers.Authorization).toBe("Bearer tok-secret");
  });

  // Regression: H4 — unencoded query let the agent inject API parameters.
  it("encodes the search query so it cannot inject parameters", () => {
    const host = gmailHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    plugin.call({ CommandEvent: "search_messages", query: "a&maxResults=999" });

    const arg = host.http.request.mock.calls[0][0] as any;
    expect(arg.url).toContain("q=a%26maxResults%3D999");
    expect(arg.url).not.toContain("&maxResults=999");
  });

  // Regression: H4 — a traversal message_id used to retarget the endpoint.
  it("keeps a traversal message_id inside a single path segment", () => {
    const host = gmailHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    plugin.call({ CommandEvent: "read_message", message_id: "../labels" });

    const arg = host.http.request.mock.calls[0][0] as any;
    expect(arg.url).toContain("/messages/..%2Flabels");
    expect(arg.url).not.toContain("/messages/../labels");
  });

  it("returns a structured failure on a non-2xx status", () => {
    const host = gmailHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 401, body: '{"error":"unauthorized"}' }));

    const res = plugin.call({ CommandEvent: "list_labels" });
    expect(res.success).toBe(false);
    expect(res.status).toBe(401);
  });

  it("rejects a missing required param instead of sending 'undefined'", () => {
    const host = gmailHost();
    const res = plugin.call({ CommandEvent: "search_messages" });
    expect(res.success).toBe(false);
    expect(host.http.request).not.toHaveBeenCalled();
  });

  // Regression: M1 — host.process.env throws for a non-allow-listed key.
  it("works from config alone when no env vars are allow-listed", () => {
    const host = gmailHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    const res = plugin.call({ CommandEvent: "list_labels" });
    expect(res.success).toBe(true);
  });
});

