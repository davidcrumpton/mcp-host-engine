import { defineConfig } from "vitest/config";
import path from "path";

export default defineConfig({
  test: {
    // Each test file gets its own isolated module registry
    isolate: true,

    // Run tests in Node (not jsdom) — plugins are server-side
    environment: "node",

    // Global test helpers available without import
    globals: false,

    // Pattern to find test files
    include: ["plugins/**/*.test.ts"],

    // Coverage (optional, run with --coverage)
    coverage: {
      provider: "v8",
      include: ["plugins/**/plugin.ts"],
      exclude: ["plugins/mock-host.ts"],
    },
  },
  resolve: {
    alias: {
      // Allow `import type { Host } from "mcphe"` in plugin source
      "mcphe": path.resolve(__dirname, "types/mcphe.d.ts"),
    },
  },
});
