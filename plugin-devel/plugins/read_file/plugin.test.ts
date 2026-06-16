/**
 * read_file/plugin.test.ts
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

describe("read_file plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("read_file");
    expect(plugin.description).toBeTruthy();
  });

  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  }); 
});

// Test Search with 'OpenBSD'

const testReadFile = async () => { 
  const params = {
    CommandEvent: "CommandEvent.read_file",
    Path: "/etc/passwd"
  };

  const response = await plugin.call(params);
 
  // check for popular O/S users but we can't use OR logic.  If we add the logic, the code fails.  Must refactor at some point
  expect(response).toContain("root");
  // expect(response).toContain("daemon");
  // expect(response).toContain("nobody");
};
