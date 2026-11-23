/**
 * proxmox/plugin.test.ts
 * Complex plugin only tests plugin description for now.
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import { installMockHost, mockHTTPResponse } from "../mock-host";

let mockHost: ReturnType<typeof installMockHost>;

beforeEach(() => {
  vi.clearAllMocks();
  mockHost = installMockHost();
});

// Vitest transforms .ts natively — import the plugin directly
import * as pluginModule from "./plugin";
const plugin = pluginModule as unknown as { version: string, name: string; description: string; call: (p: Record<string, unknown>) => any };

const PROXMOX_CFG = {
  base_url: "https://pve.example.com:8006",
  api_token: "root@pam!mcphe=00000000-0000-0000-0000-000000000000",
};

describe("proxmox plugin", () => {
  it("has the correct metadata", () => {
    expect(plugin.name).toBe("proxmox_api");
    expect(plugin.description).toBeTruthy();
  });
  it("version is \\d+\\.\\d+\\.\\d+ major.minor.patch format", () => {
    expect(plugin.version).match(/\d+\.\d+\.\d+/);
  });

  // Regression: H5 — JSON.parse was applied to the HTTPResponse object, so
  // every call threw and the plugin was entirely non-functional.
  it("parses response.body rather than the response object", () => {
    const host = installMockHost({
      allowedDomains: ["pve.example.com"],
      pluginConfig: PROXMOX_CFG,
    });
    host.http.request.mockReturnValue(
      mockHTTPResponse({ status: 200, body: JSON.stringify({ data: [{ vmid: 100 }] }) })
    );

    const res = plugin.call({ mode: "list_vms" });
    expect(res.success).toBe(true);
    expect(res.data).toEqual([{ vmid: 100 }]);
  });

  it("returns a structured failure on a non-2xx status", () => {
    const host = installMockHost({
      allowedDomains: ["pve.example.com"],
      pluginConfig: PROXMOX_CFG,
    });
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 500, body: "boom" }));

    const res = plugin.call({ mode: "list_vms" });
    expect(res.success).toBe(false);
    expect(res.status).toBe(500);
  });

  it("returns a structured failure when the body is not JSON", () => {
    const host = installMockHost({
      allowedDomains: ["pve.example.com"],
      pluginConfig: PROXMOX_CFG,
    });
    host.http.request.mockReturnValue(mockHTTPResponse({ status: 200, body: "<html>proxy error</html>" }));

    const res = plugin.call({ mode: "list_vms" });
    expect(res.success).toBe(false);
  });

  // Regression: M1 — host.process.env throws for a non-allow-listed key, which
  // used to make a config-only deployment fail before reaching cfg.base_url.
  it("works from config alone when no env vars are allow-listed", () => {
    const host = installMockHost({
      allowedDomains: ["pve.example.com"],
      allowedEnv: [],
      pluginConfig: PROXMOX_CFG,
    });
    host.http.request.mockReturnValue(
      mockHTTPResponse({ status: 200, body: JSON.stringify({ data: [] }) })
    );

    const res = plugin.call({ mode: "list_vms" });
    expect(res.success).toBe(true);
  });
});

