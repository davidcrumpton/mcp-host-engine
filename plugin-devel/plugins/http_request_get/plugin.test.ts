/**
 * http_request_get/plugin.test.ts
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
// import { installMockHost } from "../mock-host";

beforeEach(() => {
  vi.clearAllMocks();
});

// Vitest transforms .ts natively — import the plugin directly
import * as pluginModule from "./plugin";
const plugin = pluginModule as unknown as { version: string, name: string; description: string; call: (p: Record<string, unknown>) => string };

describe("http_request_get plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("http_request_get");
    expect(plugin.description).toBeTruthy();
  });

    it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
        expect(plugin.version).match(/\d+\.\d+\.\d+/);
    }); 
  
});

const testHttpRequestGet = async () => { 
  const params = {
    CommandEvent: "CommandEvent.http_request_get",
    URL: "https://www.example.com/"
  };

  const response = await plugin.call(params);
 
  // check for 
  expect(response).toContain("HTTP/1.1");
};
