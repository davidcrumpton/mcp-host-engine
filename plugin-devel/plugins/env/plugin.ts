/// <reference path="../../types/mcphe.d.ts" />

// This is a test scripts that tries to get and ENV var passed into it
// All modules can call host.getEnv but this is used to test it.

const plugin = {
  name: "get_env_var",
  description: "Get an environment variable passed into it.  USE THIS FOR TESTING. IT'S A HARDCODED VERSION OF THE env.sh SCRIPT. NOT A REAL PLUGIN... YET.",
  version: "1.1.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  true,
    openWorldHint:   false,
  },
  inputSchema: { type: "object", properties: { env_var: { type: "string" } }, required: ["env_var"] },
  call(params: Record<string, unknown>) {
    // USE THIS FOR TESTING. IT'S A HARDCODED VERSION OF THE env.sh SCRIPT. NOT A REAL PLUGIN... YET.
    return host.process.env(params.env_var);
  },
};

module.exports = plugin;
