/**
 * memory/plugin.test.ts
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

describe("memory plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("memory");
    expect(plugin.description).toBeTruthy();
  });

    it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
      expect(plugin.version).match(/\d+\.\d+\.\d+/);
    }); 
});

// memory actions to test    action: { type: "string", enum: ["set", "get", "delete", "list"] },

const testMemorySet = async () => { 
  const params = {
    CommandEvent: "CommandEvent.memory",
    action: "set",
    key: "test",
    value: "test"
  };

  const response = await plugin.call(params);
 
  // check for 
  expect(response).toBe("Success");
};

const testMemoryGet = async () => { 
  const params = {
    CommandEvent: "CommandEvent.memory",
    action: "get",
    key: "test"
  };

  const response = await plugin.call(params);

  // check for 
  expect(response).toBe("Success");
};

const testMemoryDelete = async () => { 
  const params = {
    CommandEvent: "CommandEvent.memory",
    action: "delete",
    key: "test"
  };

  const response = await plugin.call(params);

  // check for 
  expect(response).toBe("Success");
};

const testMemoryList = async () => { 
  const params = {
    CommandEvent: "CommandEvent.memory",
    action: "list"
  };

  const response = await plugin.call(params);

  // check for 
  expect(response).toBe("Success");
};