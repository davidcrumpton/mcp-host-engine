/**
 * run_command/plugin.test.ts
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

describe("run_command plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("run_command");
    expect(plugin.description).toBeTruthy();
  });

    it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
        expect(plugin.version).match(/\d+\.\d+\.\d+/);
    });
});

// Test Search with 'OpenBSD'

const testRunCommandDate = async () => { 
  const params = {
    CommandEvent: "CommandEvent.run_command",
    Command: "file",
    Args: ["/bin/sh"]
  };

  const response = await plugin.call(params);
 
  // bin/shell exists on everything but Windows -- to bad MS Fanboys!
  expect(response).toContain("Mach-O") || expect(response).toContain("ELF") || expect(response).toContain("PE");
};
