/**
 * wikipedia_search/plugin.test.ts
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
const plugin = pluginModule as unknown as {
  version: string;
  name: string;
  description: string;
  inputSchema: any;
  call: (p: Record<string, unknown>) => any;
};

function requestedUrls(host: ReturnType<typeof installMockHost>): string[] {
  return host.http.request.mock.calls.map((c: any[]) => {
    const arg = c[0];
    return typeof arg === "string" ? arg : arg.url;
  });
}

describe("wikipedia_search plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("wikipedia_search");
    expect(plugin.description).toBeTruthy();
  });

  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });
});

// ---------------------------------------------------------------------------
// H10 — `language` was concatenated into the URL *authority*, so `/`, `?` and
// `#` could retarget the request to an arbitrary host (cloud metadata, an
// internal service, anything the daemon can reach). The host's allowed_domains
// check happened to contain it in the shipped config, but the plugin must not
// depend on that: it now validates against a fixed allow-list before use.
// ---------------------------------------------------------------------------

const HOST_INJECTION_PAYLOADS = [
  "169.254.169.254/latest/meta-data#",
  "169.254.169.254",
  "localhost:8080/x#",
  "en.wikipedia.org@evil.com#",
  "evil.com?x#",
  "../internal",
  "evil.com",
  "a.b/c",
  "x#",
  "x?",
];

describe("H10: language cannot retarget the request host", () => {
  for (const payload of HOST_INJECTION_PAYLOADS) {
    it(`rejects ${JSON.stringify(payload)} without making any request`, () => {
      const res = plugin.call({ query: "OpenBSD", language: payload });

      expect(res.success).toBe(false);
      expect(mockHost.http.request).not.toHaveBeenCalled();
    });
  }

  it("never builds a URL whose host is outside <lang>.wikipedia.org", () => {
    // Let every accepted call run; the default mock returns an empty 200, so
    // the handlers bail out early but the URLs are still recorded.
    for (const lang of ["en", "fr", "de", "ja", "zh"]) {
      mockHost = installMockHost({
        allowedDomains: [`${lang}.wikipedia.org`],
      });
      mockHost.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

      plugin.call({ query: "OpenBSD", language: lang, mode: "lookup" });

      for (const url of requestedUrls(mockHost)) {
        const host = new URL(url).hostname;
        expect(host).toBe(`${lang}.wikipedia.org`);
      }
    }
  });

  it("surfaces a clear error listing the supported codes", () => {
    const res = plugin.call({ query: "OpenBSD", language: "klingon" });

    expect(res.success).toBe(false);
    expect(res.result).toContain("Unsupported language");
    expect(res.result).toContain("en");
    expect(res.result).toContain("fr");
  });

  it("advertises the same language set in the schema as it enforces", () => {
    const declared = plugin.inputSchema.properties.language.enum;
    expect(Array.isArray(declared)).toBe(true);

    for (const code of declared) {
      const res = plugin.call({ query: "OpenBSD", language: code });
      // Rejection would mean the schema offers a code the plugin refuses.
      expect(String(res.result || "")).not.toContain("Unsupported language");
    }
  });
});

// ---------------------------------------------------------------------------
// Normal behaviour that the H10 fix must not break.
// ---------------------------------------------------------------------------

describe("language handling", () => {
  it("defaults to en when language is omitted", () => {
    mockHost.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    plugin.call({ query: "OpenBSD", mode: "search" });

    const urls = requestedUrls(mockHost);
    expect(urls.length).toBeGreaterThan(0);
    for (const url of urls) {
      expect(new URL(url).hostname).toBe("en.wikipedia.org");
    }
  });

  it("treats an empty language as the default", () => {
    mockHost.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    plugin.call({ query: "OpenBSD", language: "   ", mode: "search" });

    for (const url of requestedUrls(mockHost)) {
      expect(new URL(url).hostname).toBe("en.wikipedia.org");
    }
  });

  it("is case-insensitive", () => {
    mockHost = installMockHost({ allowedDomains: ["fr.wikipedia.org"] });
    mockHost.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    const res = plugin.call({ query: "OpenBSD", language: "FR", mode: "search" });

    expect(String(res.result || "")).not.toContain("Unsupported language");
    for (const url of requestedUrls(mockHost)) {
      expect(new URL(url).hostname).toBe("fr.wikipedia.org");
    }
  });

  it("still reports a useful message when a language edition returns nothing", () => {
    mockHost = installMockHost({ allowedDomains: ["fr.wikipedia.org"] });
    mockHost.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "{}" }));

    const res = plugin.call({ query: "zzzznotarealarticle", language: "fr", mode: "search" });

    expect(res.success).toBe(false);
    expect(res.result).toContain("French");
  });
});
