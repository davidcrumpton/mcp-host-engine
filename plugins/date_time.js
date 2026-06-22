"use strict";
const plugin = {
  name: "get_datetime",
  description: "Get the current date and time of the host.",
  version: "1.1.0",
  commit: "none",
  Tags: ["utility"],
  annotations: {
    readOnlyHint: true,
    destructiveHint: false,
    idempotentHint: false,
    openWorldHint: false
  },
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    host.console.log("Called")
    return (/* @__PURE__ */ new Date()).toISOString();
  }
};
module.exports = plugin;
