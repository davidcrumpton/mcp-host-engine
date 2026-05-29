module.exports = {
  name: "read_file",
  description: "Read a file from the host filesystem.",
  version: "1.0.0",
  commit: "none",
  Tags: ["utility"],
  inputSchema: {
    type: "object",
    properties: {
      path: { type: "string", description: "Path to read." },
    },
    required: ["path"],
  },
  call(params) {
    return host.readFile(params.path);
  },
};
