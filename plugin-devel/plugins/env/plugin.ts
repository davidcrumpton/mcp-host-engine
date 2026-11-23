/// <reference path="../../types/mcphe.d.ts" />

// Reads an environment variable that the operator has explicitly allow-listed
// for this plugin via allowed_env_vars.
//
// M15: this is a secret-disclosure primitive dressed as an inert getter.
// allowed_env_vars is a coarse *name* allow-list -- whatever the operator adds
// so the tool can do its advertised job becomes readable by the model, so
// {"env_var":"AWS_SECRET_ACCESS_KEY"} returns the credential verbatim if it was
// allow-listed. readOnlyHint:true is literally true (reading env is read-only)
// but makes a credential read look harmless. It fails closed with no allow-list,
// which is correct; the residual risk is the breadth of the allow-list, so the
// description now says so plainly and the tool is disabled by default in the
// shipped config.

const plugin = {
  name: "get_env_var",
  description:
    "Read an environment variable by name, restricted to the names listed in this " +
    "plugin's allowed_env_vars. WARNING: this exposes secrets. Anything in " +
    "allowed_env_vars can be read by the model, so the allow-list must contain only " +
    "variables you are willing to hand to an agent -- never credentials. Prefer " +
    "configuring the specific plugin that needs a value instead of enabling this one.",
  version: "1.2.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  true,
    openWorldHint:   false,
  },
  inputSchema: {
    type: "object",
    properties: {
      env_var: {
        type: "string",
        description:
          "Name of the environment variable to read. Must appear in this plugin's " +
          "allowed_env_vars, otherwise the read fails closed."
      }
    },
    required: ["env_var"]
  },
  call(params: Record<string, unknown>) {
    const name = params.env_var;
    if (typeof name !== "string" || name === "") {
      return { success: false, error: "env_var is required" };
    }

    // host.process.env throws when the name is not allow-listed, which is the
    // fail-closed behaviour we want. Turn it into a normal tool error rather
    // than an uncaught exception.
    try {
      return host.process.env(name);
    } catch (err) {
      return {
        success: false,
        error: (err as Error)?.message || `access to environment variable ${name} is not allowed`
      };
    }
  },
};

module.exports = plugin;
