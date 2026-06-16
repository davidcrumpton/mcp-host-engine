/**
 * env/plugin.test.ts
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import { buildMockHost, installMockHost } from "../mock-host";

let mockHost: ReturnType<typeof installMockHost>;

beforeEach(() => {
  vi.clearAllMocks();
  mockHost = installMockHost({
    allowedDomains: ["ifconfig.io"],
    env: { HOME: "/Users/mcphe" },
  });
});

// Vitest transforms .ts natively — import the plugin directly
import * as pluginModule from "./plugin";
const plugin = pluginModule as unknown as { version: string, name: string; description: string; call: (p: Record<string, unknown>) => string };

describe("get_env_var plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("get_env_var");
    expect(plugin.description).toBeTruthy();
  });

  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });

  it("fetches the HOME environment variable", async () => {
    const params = {
      CommandEvent: "CommandEvent.env",
      env_var: "HOME"
    };

    const response = await plugin.call(params);

    expect(response).toContain("/Users/mcphe"); // As set in mockHost.ts
  });
});

