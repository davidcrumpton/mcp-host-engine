"use strict";
const plugin = {
  name: "get_env_var",
  description: "Read an environment variable by name, restricted to the names listed in this plugin's allowed_env_vars. WARNING: this exposes secrets. Anything in allowed_env_vars can be read by the model, so the allow-list must contain only variables you are willing to hand to an agent -- never credentials. Prefer configuring the specific plugin that needs a value instead of enabling this one.",
  version: "1.2.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint: true,
    destructiveHint: false,
    idempotentHint: true,
    openWorldHint: false
  },
  inputSchema: {
    type: "object",
    properties: {
      env_var: {
        type: "string",
        description: "Name of the environment variable to read. Must appear in this plugin's allowed_env_vars, otherwise the read fails closed."
      }
    },
    required: ["env_var"]
  },
  call(params) {
    const name = params.env_var;
    if (typeof name !== "string" || name === "") {
      return { success: false, error: "env_var is required" };
    }
    try {
      return host.process.env(name);
    } catch (err) {
      return {
        success: false,
        error: (err == null ? void 0 : err.message) || `access to environment variable ${name} is not allowed`
      };
    }
  }
};
module.exports = plugin;
