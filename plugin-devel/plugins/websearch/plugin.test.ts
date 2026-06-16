/**
 * websearch/plugin.test.ts
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import { installMockHost } from "../mock-host";

let mockHost: ReturnType<typeof installMockHost>;

beforeEach(() => {
  vi.clearAllMocks();
  mockHost = installMockHost();
});

// Vitest transforms .ts natively — import the plugin directly
import * as pluginModule from "./plugin";
const plugin = pluginModule as unknown as { version: string, name: string; description: string; call: (p: Record<string, unknown>) => string };

describe("websearch plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("websearch");
    expect(plugin.description).toBeTruthy();
  });

    it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
        expect(plugin.version).match(/\d+\.\d+\.\d+/);
    });
});

// Test Search with 'OpenBSD'

const testSearchOpenBSD = async () => { 
  const params = {
    query: "OpenBSD"
  };

  const response = await plugin.call(params);

  // Text can change but the web site should always be in the description
  expect(response.includes("https://www.openbsd.org")).toBe(true);
  expect(response.includes("OpenBSD")).toBe(true);
};
