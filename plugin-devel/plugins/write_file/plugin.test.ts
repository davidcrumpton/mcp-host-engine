/**
 * write_file/plugin.test.ts
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

describe("write_file plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("write_file");
    expect(plugin.description).toBeTruthy();
  });

  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  }); 

  // The following need fixing
//   it("testTmpFileWrite", testTmpFileWrite);
//   it("testPathNotAllowed", testPathNotAllowed);
});

// Test Search with 'OpenBSD'

const testTmpFileWrite = async () => { 
  const params = {
    path: "/tmp/test.txt",
    content: "Hello, world!"
  };

  const response = await plugin.call(params);
  // check for undefined
  expect(response)
  
  // check for File written successfully.
  expect(response).toBe("File written successfully.");
};

const testPathNotAllowed = async () => { 
  const params = {
    path: "/usr/bin/test.txt",
    content: "Hello, world!"
  };

  const response = await plugin.call(params);
  // check for error
  expect(response).toBeInstanceOf(Error);
  
  // check for File written successfully.
  expect(response).toBe("error: writing to file \"/usr/bin/test.txt\" is not allowed");
};
