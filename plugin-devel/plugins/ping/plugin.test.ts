/**
 * ping/plugin.test.ts
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

describe("ping plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("ping");
    expect(plugin.description).toBeTruthy();
  });

    it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
        expect(plugin.version).match(/\d+\.\d+\.\d+/);
    });
  
});

// Ping returns string 'pong'

const testPing = async () => { 
  const params = {
    CommandEvent: "CommandEvent.ping"
  };

  const response = await plugin.call(params);
 
  // ping returns string 'pong'
  expect(response).toBe("pong");
};
