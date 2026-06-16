/**
 * date_time/plugin.test.ts
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

describe("date_time plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("get_datetime");
    expect(plugin.description).toBeTruthy();
  });

  it("returns ISO String", () => {
    expect(plugin.call({})).match(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/);
  });

  // version is 1.1.1 major.minor.patch format
  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });

// commit is optional for plugin authors
//   it("commit is a SHA-256 hash", () => {
//     expect(plugin.commit).match(/[0-9a-f]{64}/);
//   });
});

const testDateTime = async () => { 
  const params = {
    CommandEvent: "CommandEvent.date_time"
  };

  const response = await plugin.call(params);
 
  // check for ISO String regex /\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/
  expect(response).match(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/);
};
