"""The JavaScript plugins the suite runs.

Each fixture is a plugin file (or a small tree of them) written into a temporary
``plugin_dir`` by ``install()``. Keeping them here rather than in a directory of
.js files means a test declares the plugins it needs:

    plugin_dir = harness.install_plugins(tmp, "ping", "reader")

...and the set under test is visible in the test body instead of inferred from
whatever happens to be on disk. It also lets the suite tamper with a file --
which is the whole point of the checksum cases -- without touching the repo.
"""

from pathlib import Path

# ---------------------------------------------------------------------------
# Trivial plugins: the shape of a tool, with nothing to go wrong
# ---------------------------------------------------------------------------

PING = """\
"use strict";
const plugin = {
  name: "ping",
  description: "A simple ping/pong tool for testing.",
  version: "1.1.0",
  commit: "none",
  tags: ["utility"],
  annotations: {
    readOnlyHint: true,
    destructiveHint: false,
    idempotentHint: true,
    openWorldHint: false
  },
  inputSchema: { type: "object", properties: {}, required: [] },
  call(params) {
    return "pong";
  }
};
module.exports = plugin;
"""

# Returns the arguments it was given, so a test can prove what crossed the wire.
ECHO = """\
"use strict";
const plugin = {
  name: "echo",
  description: "Returns its arguments, the caller's identity and its own config.",
  inputSchema: {
    type: "object",
    properties: { text: { type: "string" } },
    required: []
  },
  call(params) {
    return {
      args: params,
      identity: host.identity,
      options: host.server.config(),
      mcp_version: host.mcp.version()
    };
  }
};
module.exports = plugin;
"""

# A plugin that fails the way plugin code fails: a thrown JS error.
BOOM = """\
"use strict";
module.exports = {
  name: "boom",
  description: "Always throws.",
  call(params) {
    throw new Error("kaboom");
  }
};
"""

# Module-level state plus per-instance config: proves that two entries backed by
# one module get independent VMs and independent options.
COUNTER = """\
"use strict";
var calls = 0;
module.exports = {
  name: "counter",
  description: "Counts its own calls and reports its configured tag.",
  inputSchema: { type: "object", properties: {} },
  call: function (params) {
    calls = calls + 1;
    var tag = host.server.config().tag;
    return "calls=" + calls + " tag=" + tag;
  }
};
"""

# ---------------------------------------------------------------------------
# Host-API plugins: one per sandboxed capability
# ---------------------------------------------------------------------------

READER = """\
"use strict";
module.exports = {
  name: "reader",
  description: "Reads a file through host.fs.readFile.",
  inputSchema: { type: "object", properties: { path: { type: "string" } } },
  call: function (params) {
    return host.fs.readFile(params.path);
  }
};
"""

WRITER = """\
"use strict";
module.exports = {
  name: "writer",
  description: "Writes a file through host.fs.writeFile.",
  inputSchema: {
    type: "object",
    properties: { path: { type: "string" }, content: { type: "string" } }
  },
  call: function (params) {
    host.fs.writeFile(params.path, params.content);
    return "wrote " + params.path;
  }
};
"""

ENVER = """\
"use strict";
module.exports = {
  name: "enver",
  description: "Reads an environment variable through host.process.env.",
  inputSchema: { type: "object", properties: { name: { type: "string" } } },
  call: function (params) {
    return host.process.env(params.name);
  }
};
"""

RUNNER = """\
"use strict";
module.exports = {
  name: "runner",
  description: "Runs a command through host.exec.runCommand.",
  inputSchema: { type: "object", properties: { command: { type: "string" } } },
  call: function (params) {
    return host.exec.runCommand(params.command);
  }
};
"""

# Uses the convenience helpers, whose only restriction is the domain list.
FETCHER = """\
"use strict";
module.exports = {
  name: "fetcher",
  description: "Fetches a URL through host.http.get / host.http.post.",
  inputSchema: {
    type: "object",
    properties: { url: { type: "string" }, method: { type: "string" } }
  },
  call: function (params) {
    if (params.method === "POST") {
      var posted = host.http.post(params.url, {}, "payload");
      return "status=" + posted.status + " body=" + posted.body;
    }
    var fetched = host.http.get(params.url, {});
    return "status=" + fetched.status + " body=" + fetched.body;
  }
};
"""

# Sends a caller-chosen header, so the header gate can be tested on the
# convenience helpers (which used to check the domain list only).
HEADERED = """\
"use strict";
module.exports = {
  name: "headered",
  description: "Sends one caller-chosen header through host.http.get.",
  inputSchema: {
    type: "object",
    properties: { url: { type: "string" }, header: { type: "string" }, value: { type: "string" } }
  },
  call: function (params) {
    var headers = {};
    if (params.header) {
      headers[params.header] = params.value || "x";
    }
    var fetched = host.http.get(params.url, headers);
    return "status=" + fetched.status;
  }
};
"""

# Asks for another plugin's config block by name, which used to be a way to read
# a neighbour's credentials.
SCOPED = """\
"use strict";
module.exports = {
  name: "scoped",
  description: "Reads a plugin config block through host.server.config(name).",
  inputSchema: { type: "object", properties: { name: { type: "string" } } },
  call: function (params) {
    return JSON.stringify(host.server.config(params.name || ""));
  }
};
"""

# Writes through host.fs.writeStream, with a producer that can fail part-way.
STREAMER = """\
"use strict";
module.exports = {
  name: "streamer",
  description: "Writes a file through host.fs.writeStream.",
  inputSchema: {
    type: "object",
    properties: { path: { type: "string" }, fail: { type: "boolean" } }
  },
  call: function (params) {
    var n = 0;
    host.fs.writeStream(params.path, {}, function () {
      n++;
      if (params.fail && n >= 1) {
        throw new Error("producer failed");
      }
      return n === 1 ? "new-content" : "";
    });
    return "written";
  }
};
"""

# Uses host.http.request, which is the one helper that honours allowed_http_methods.
REQUESTER = """\
"use strict";
module.exports = {
  name: "requester",
  description: "Issues a request through host.http.request.",
  inputSchema: {
    type: "object",
    properties: { url: { type: "string" }, method: { type: "string" } }
  },
  call: function (params) {
    var response = host.http.request({
      url: params.url,
      method: params.method || "GET"
    });
    return "status=" + response.status;
  }
};
"""

# require() is resolved against plugin_dir, so a fixture can point at any of the
# module fixtures below and prove the loader's behaviour.
REQUIRER = """\
"use strict";
module.exports = {
  name: "requirer",
  description: "require()s a module and returns one of its exports.",
  inputSchema: { type: "object", properties: { what: { type: "string" } } },
  call: function (params) {
    var loaded = require(params.what);
    return String(loaded.value);
  }
};
"""

# Calls host.crypto.randomBytes with the length it is given, so a tool argument
# reaches make([]byte, n) in the host.
RANDOM_BYTES = """\
"use strict";
module.exports = {
  name: "random_bytes",
  description: "Returns randomBytes(n).",
  inputSchema: {
    type: "object",
    properties: { n: { type: "number" } },
    required: ["n"]
  },
  call: function (params) {
    return host.crypto.randomBytes(params.n);
  }
};
"""

# ---------------------------------------------------------------------------
# Module fixtures for require()
# ---------------------------------------------------------------------------

LIB_HELPER = """\
module.exports = {
  value: "helper-export"
};
"""

# A genuine require() cycle: a.js pulls in b.js, which pulls in a.js again. Both
# use plugin_dir-relative specifiers because that is what the loader resolves.
LIB_CYCLE_A = """\
var b = require("./lib/cycle_b.js");
module.exports = { value: "a" };
"""

LIB_CYCLE_B = """\
var a = require("./lib/cycle_a.js");
module.exports = { value: "b" };
"""

# Not JavaScript, to prove require() refuses it.
LIB_DATA_JSON = """\
{"value": "not-a-module"}
"""

# Requires a file that the fixture does not create: the checksum cases generate a
# manifest first, then add lib/late.js, so the file is genuinely unlisted.
NEEDS_LATE = """\
"use strict";
module.exports = {
  name: "needs_late",
  description: "require()s a file at call time.",
  inputSchema: { type: "object", properties: {} },
  call: function (params) {
    return String(require("./lib/late.js").value);
  }
};
"""

LATE_LIB = """\
module.exports = { value: "late-export" };
"""

# ---------------------------------------------------------------------------
# Plugins whose shape is the thing under test
# ---------------------------------------------------------------------------

# Declares no inputSchema at all.
NO_SCHEMA = """\
"use strict";
module.exports = {
  name: "bare",
  description: "Declares no inputSchema.",
  call: function (params) {
    return "bare";
  }
};
"""

BAD_SCHEMA_TYPE = """\
"use strict";
module.exports = {
  name: "badtype",
  description: "Declares an inputSchema that is not an object schema.",
  inputSchema: { type: "array", items: { type: "string" } },
  call: function (params) {
    return "never";
  }
};
"""

BAD_SCHEMA_VALUE = """\
"use strict";
module.exports = {
  name: "badvalue",
  description: "Declares an inputSchema that is not an object at all.",
  inputSchema: "not-a-schema",
  call: function (params) {
    return "never";
  }
};
"""

NO_NAME = """\
"use strict";
module.exports = {
  description: "Exports no name.",
  call: function (params) {
    return "never";
  }
};
"""

NO_CALL = """\
"use strict";
module.exports = {
  name: "nocall",
  description: "Exports no call function.",
  call: "not-a-function"
};
"""

DUPLICATE_ONE = """\
"use strict";
module.exports = {
  name: "dupe",
  description: "First plugin exporting the name dupe.",
  call: function (params) {
    return "one";
  }
};
"""

DUPLICATE_TWO = """\
"use strict";
module.exports = {
  name: "dupe",
  description: "Second plugin exporting the name dupe.",
  call: function (params) {
    return "two";
  }
};
"""

# Requires an argument and types it, so the SDK's schema validation can be seen
# refusing a call before the plugin is entered.
STRICT = """\
"use strict";
module.exports = {
  name: "strict",
  description: "Requires a string argument called text.",
  inputSchema: {
    type: "object",
    properties: { text: { type: "string" } },
    required: ["text"]
  },
  call: function (params) {
    return "got " + params.text;
  }
};
"""

# Exported name differs from the file name, which is why `digest -format yaml`
# keys on the export rather than the file.
CLOCK = """\
"use strict";
module.exports = {
  name: "get_clock",
  description: "Returns a fixed tick.",
  inputSchema: { type: "object", properties: {} },
  call: function (params) {
    return "tick";
  }
};
"""

# ---------------------------------------------------------------------------
# The catalogue
# ---------------------------------------------------------------------------

# fixture name -> {path relative to plugin_dir: source}
FIXTURES = {
    "ping": {"ping.js": PING},
    "echo": {"echo.js": ECHO},
    "boom": {"boom.js": BOOM},
    "counter": {"counter.js": COUNTER},
    "reader": {"reader.js": READER},
    "writer": {"writer.js": WRITER},
    "enver": {"enver.js": ENVER},
    "runner": {"runner.js": RUNNER},
    "random-bytes": {"random_bytes.js": RANDOM_BYTES},
    "fetcher": {"fetcher.js": FETCHER},
    "headered": {"headered.js": HEADERED},
    "scoped": {"scoped.js": SCOPED},
    "streamer": {"streamer.js": STREAMER},
    "requester": {"requester.js": REQUESTER},
    "requirer": {"requirer.js": REQUIRER},
    "strict": {"strict.js": STRICT},
    "helper": {"lib/helper.js": LIB_HELPER},
    "cycle": {
        "lib/cycle_a.js": LIB_CYCLE_A,
        "lib/cycle_b.js": LIB_CYCLE_B,
    },
    "data": {"lib/data.json": LIB_DATA_JSON},
    "needs-late": {"needs_late.js": NEEDS_LATE},
    "no-schema": {"bare.js": NO_SCHEMA},
    "bad-schema-type": {"badtype.js": BAD_SCHEMA_TYPE},
    "bad-schema-value": {"badvalue.js": BAD_SCHEMA_VALUE},
    "no-name": {"noname.js": NO_NAME},
    "no-call": {"nocall.js": NO_CALL},
    "duplicate": {
        "dupe_one.js": DUPLICATE_ONE,
        "dupe_two.js": DUPLICATE_TWO,
    },
    "clock": {"clock_file.js": CLOCK},
}

# fixture name -> the name the plugin exports, which is the key a config uses.
# Only the fixtures whose export differs from their fixture name need an entry;
# everything else is registered under its own name.
TOOL_NAMES = {
    # Exports no name at all, so it is only addressable by its file name.
    "no-name": "noname",
    "no-call": "nocall",
    "random-bytes": "random_bytes",
    "no-schema": "bare",
    "bad-schema-type": "badtype",
    "bad-schema-value": "badvalue",
    "needs-late": "needs_late",
    "clock": "get_clock",
    "duplicate": "dupe",
}


def tool_name(fixture):
    """Return the tool name a fixture registers as."""
    return TOOL_NAMES.get(fixture, fixture)


def install(plugin_dir, *names):
    """Write the named fixtures into plugin_dir. Returns the directory."""
    plugin_dir = Path(plugin_dir)
    for name in names:
        try:
            files = FIXTURES[name]
        except KeyError:
            raise KeyError(
                f"unknown fixture {name!r}; known fixtures: {sorted(FIXTURES)}"
            )
        for relative, source in files.items():
            target = plugin_dir / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(source)
    return plugin_dir


def write_late_lib(plugin_dir, source=None):
    """Create the file ``needs-late`` require()s, after the manifest was made."""
    target = Path(plugin_dir) / "lib" / "late.js"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(LATE_LIB if source is None else source)
    return target


