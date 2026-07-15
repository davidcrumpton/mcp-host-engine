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

  it("throws when domain is blocked", () => {
    // Reinstall host with ifconfig.io blocked
    installMockHost({ allowedDomains: ["example.com"] });
    expect(() => plugin.call({})).toThrow(/not allowed/);
  });

  it("throws on invalid JSON response", () => {
    vi.mocked(mockHost.http.request).mockReturnValueOnce(
      mockHTTPResponse({ body: "not json" })
    );
    expect(() => plugin.call({})).toThrow();
  });
});
