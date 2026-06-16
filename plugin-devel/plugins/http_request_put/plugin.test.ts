/**
 * http_request_put/plugin.test.ts
 * Complex plugin only tests plugin description for now.
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

describe("http_request_put plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("http_request_put");
    expect(plugin.description).toBeTruthy();
  });

    it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
      expect(plugin.version).match(/\d+\.\d+\.\d+/);
    }); 
});

