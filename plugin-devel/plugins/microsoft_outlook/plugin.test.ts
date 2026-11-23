/**
 * microsoft_outlook/plugin.test.ts
 * Metadata checks plus regressions for the response-shape and token-handling bugs.
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

const OUTLOOK_CFG = {
  ApiUrl: "https://outlook.example.com/api/v2.0",
  ApiToken: "tok-secret",
};

function outlookHost(overrides: Record<string, unknown> = {}) {
  return installMockHost({
    allowedDomains: ["outlook.example.com"],
    allowedEnv: [],
    pluginConfig: { options: { ...OUTLOOK_CFG, ...overrides } },
  });
}

describe("microsoft_outlook plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("outlook_365");
    expect(plugin.description).toBeTruthy();
  });

  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });

  // Regression: H6 — JSON.parse was applied to the HTTPResponse object, so all
  // four commands threw on every call regardless of credentials or network.
  it("parses response.body rather than the response object", () => {
    const host = outlookHost();
    host.http.request.mockReturnValue(
      mockHTTPResponse({ status: 200, body: JSON.stringify({ value: [{ id: "inbox" }] }) })
    );

    const res = plugin.call({ CommandEvent: "list_labels" });
    expect(res.success).toBe(true);
    expect(res.data).toEqual({ value: [{ id: "inbox" }] });
  });

  // Regression: H3 — the token was in the query string on 3 of 4 endpoints.
  it("sends the token in an Authorization header, never the query string", () => {
    const host = outlookHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    plugin.call({ CommandEvent: "read_message", message_id: "abc" });

    const arg = host.http.request.mock.calls[0][0] as any;
    expect(arg.url).not.toContain("access_token");
    expect(arg.url).not.toContain("tok-secret");
    expect(arg.headers.Authorization).toBe("Bearer tok-secret");
  });

  // Regression: H4 — unencoded params let the agent inject OData parameters.
  it("encodes the search query so it cannot inject parameters", () => {
    const host = outlookHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    plugin.call({ CommandEvent: "search_messages", query: "x&$top=1000" });

    const arg = host.http.request.mock.calls[0][0] as any;
    expect(arg.url).toContain("search=x%26%24top%3D1000");
    expect(arg.url).not.toContain("&$top=1000");
  });

  it("keeps a traversal message_id inside a single path segment", () => {
    const host = outlookHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    plugin.call({ CommandEvent: "read_message", message_id: "../labels" });

    const arg = host.http.request.mock.calls[0][0] as any;
    expect(arg.url).toContain("/messages/..%2Flabels");
    expect(arg.url).not.toContain("/messages/../labels");
  });

  it("returns a structured failure on a non-2xx status", () => {
    const host = outlookHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 403, body: "denied" }));

    const res = plugin.call({ CommandEvent: "list_labels" });
    expect(res.success).toBe(false);
    expect(res.status).toBe(403);
  });

  // Regression: M1 — host.process.env throws for a non-allow-listed key.
  it("works from config alone when no env vars are allow-listed", () => {
    const host = outlookHost();
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    const res = plugin.call({ CommandEvent: "list_labels" });
    expect(res.success).toBe(true);
  });
});
