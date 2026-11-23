/**
 * get_ip/plugin.test.ts
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import { installMockHost, mockHTTPResponse } from "../mock-host";

let mockHost: ReturnType<typeof installMockHost>;

beforeEach(() => {
  mockHost = installMockHost({ allowedDomains: ["ifconfig.io"] });
});

// Vitest transforms .ts natively — import the plugin directly
import * as pluginModule from "./plugin";
const plugin = pluginModule as unknown as { version: string, name: string; description: string; call: (p: Record<string, unknown>) => string };

describe("get_ip plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("get_ip");
    expect(plugin.description).toBeTruthy();
  });

  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });

  it("returns country code and IP from ifconfig.io", () => {
    vi.mocked(mockHost.http.request).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ ip: "1.2.3.4", country_code: "US" }) })
    );
    const res = plugin.call({}) as unknown as string;
    expect(res).toBe("US: 1.2.3.4");
    expect(mockHost.http.request).toHaveBeenCalledWith({url: "https://ifconfig.io/all.json", method: "GET"});
  });

  it("handles missing country_code gracefully", () => {
    vi.mocked(mockHost.http.request).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ ip: "1.2.3.4" }) })
    );
    expect(plugin.call({})).toBe("unknown: 1.2.3.4");
  });

  it("handles missing ip gracefully", () => {
    vi.mocked(mockHost.http.request).mockReturnValueOnce(
      mockHTTPResponse({ body: JSON.stringify({ country_code: "US" }) })
    );
    expect(plugin.call({})).toBe("US: unknown");
  });

  // L5: these used to throw out of call(). A transport or parse failure is now
  // a structured failure, so the model gets a reason instead of an exception
  // (or, worse, an uncaught SyntaxError from JSON.parse).
  it("returns a structured error when the domain is blocked", () => {
    // Reinstall host with ifconfig.io blocked
    installMockHost({ allowedDomains: ["example.com"] });
    const res = plugin.call({}) as unknown as Record<string, unknown>;
    expect(res.success).toBe(false);
    expect(String(res.error)).toMatch(/not allowed/);
  });

  it("returns a structured error on invalid JSON response", () => {
    vi.mocked(mockHost.http.request).mockReturnValueOnce(
      mockHTTPResponse({ body: "not json" })
    );
    const res = plugin.call({}) as unknown as Record<string, unknown>;
    expect(res.success).toBe(false);
    expect(String(res.error)).toMatch(/non-JSON/);
  });
});
