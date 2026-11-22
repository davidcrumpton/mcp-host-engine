# tests/

End-to-end tests for mcphe. Every case drives the **real binary**: over HTTP, over
stdio, or through its sub-commands. Nothing is mocked, and the only thing that is
faked is the remote host a plugin fetches from, which is a local HTTP server.

```sh
cd <repo root>
python3 -m tests        # ~9 s, offline, no daemon, no network
```

`python3 -m unittest discover -s tests -t .` runs the same thing, as does
`python3 tests/__main__.py` from any directory. The interpreter needs nothing
installed — no `requests`, no SDK, just the standard library — but it does need
**Go on `PATH`**, because the subject is the binary it builds. The binary is
built once per run; set `MCPHE_TEST_BINARY=/path/to/mcphe` to reuse an existing
build instead.

`go test ./...` is unaffected: `tests/` holds no Go files, so the Go tool skips
it.

## Coverage

162 recorded cases, each mapped to a test. `case_inventory.py` is the inventory
of record; `test_inventory.py` is the gate that keeps it honest.

| Section | Positive | Negative | Cases |
|---------|---------:|---------:|------:|
| 1. Startup & config loading | 6 | 8 | **14** |
| 2. Tool discovery & registration | 7 | 10 | **17** |
| 3. Tool invocation & host sandbox | 11 | 26 | **37** |
| 4. HTTP transport & auth | 10 | 14 | **24** |
| 5. stdio transport | 3 | 3 | **6** |
| 6. Plugin integrity | 12 | 16 | **28** |
| 7. Config integrity | 9 | 8 | **17** |
| 8. Token CLI | 7 | 5 | **12** |
| 9. CLI usage & exit codes | 4 | 3 | **7** |
| **Total** | **69** | **93** | **162** |

## Layout

```
tests/
  case_inventory.py            inventory of record: (case id, section, kind, description)
  test_inventory.py            the coverage gate
  test_server_positive.py      startup, tool discovery, host API   (sections 1-3)
  test_server_negative.py      startup refusals, sandbox denials   (sections 1-3)
  test_transport_positive.py   SSE, sessions, auth, CORS, stdio    (sections 4-5)
  test_transport_negative.py   401/404/415, protocol errors, stdio  (sections 4-5)
  test_integrity_positive.py   digest output, checksums, config pin (sections 6-7)
  test_integrity_negative.py   mismatches, malformed pins          (sections 6-7)
  test_cli_positive.py         token create/revoke, usage          (sections 8-9)
  test_cli_negative.py         token and flag errors               (sections 8-9)
  test_live.py                 opt-in, real deployment
  support/
    harness.py                 builds/runs the binary, writes configs, temp dirs
    cases.py                   the base classes the modules build on
    mcp_client.py              JSON-RPC over streamable HTTP and over stdio
    fixtures.py                the JavaScript plugins under test
    upstream.py                local stand-in for a remote host
```

## Why the real binary, and why no network

The subject is a *server*, and most of what can go wrong lives at the edges: the
status code for a bad `Accept` header, whether a checksum mismatch happens before
or after the config is parsed, what `tools/call` returns when plugin code throws.
A mock proves none of that. So the suite runs the binary it just built, with a
config and a plugin directory it wrote, and asserts on what a client sees.

Plugins are written into a temporary directory by `support/fixtures.py` rather
than committed as a directory of `.js` files. That is what makes the integrity
cases possible — tampering with a plugin, adding one after the manifest was
generated, corrupting a `require()`d module — and it makes each test declare the
plugins it needs:

```python
class RequireTests(ServerCase):
    plugins = ("requirer", "helper")
    enabled = ("requirer",)      # helper.js is a library, not a tool
```

The one thing that would otherwise need the internet is `host.http.*`, so
`support/upstream.py` serves those requests on `127.0.0.1`. The domain allow-list
is compared against the URL's hostname, so a config listing `127.0.0.1` is
exactly as restricted as one listing `example.com`; no rule is relaxed for the
test.

## Rules for tests in this directory

> The suite must run on a random Tuesday, offline, in one command.

- **No network, no container runtime, no pre-existing daemon.** Only
  `test_live.py` reaches outside, and it skips itself when it cannot.
- **Assert on exit codes, on the wire and on disk** — not on prose the binary
  prints. Prose is for humans; the exit status and the JSON-RPC error are the
  contract. Where a message *is* the contract (a refusal that a human has to act
  on), assert the fragment that carries the meaning.
- **A negative case asserts the failure mode**, not just that something failed:
  401 vs 404 vs 415, a JSON-RPC error vs a plain-text body, "not allowed" vs
  "not listed".
- **Every write is verified by reading it back**, in the test, from the
  filesystem — a plugin returning "wrote" only means the host call returned.
- **Every test maps to a case, and every case maps to a test.** The gate fails
  otherwise — see below.

## The coverage gate

`test_inventory.py` cross-checks `case_inventory.CASES` against the `CASES`
mapping each test module declares. It fails when:

- a case in the inventory has no test,
- a mapped test method no longer exists,
- a mapped case id is not in the inventory,
- the inventory contains a duplicate case id,
- a test method exists that is mapped to no case,
- a case names an unknown section or kind, or has no description,
- a section is declared but unused, or lacks a positive or a negative case.

Two consequences worth remembering:

1. **Renaming a case to match a test defeats the gate.** Add or retire cases
   deliberately, in the inventory, in the same commit.
2. **The gate only scans `PORTED_MODULES`.** A new test module is unchecked
   until it is listed there — add it with the file.

## Adding a case

1. Add `("my.case", section, POSITIVE|NEGATIVE, "what it asserts")` to
   `case_inventory.CASES`.
2. Write the test in the matching positive/negative module, on `ServerCase`,
   `StdioCase`, `StartupCase` or `ConfigCase` from `support/cases.py`.
3. Map it: `"MyTests.test_something": "my.case"` in that module's `CASES`.
4. Run `python3 -m tests` from the repo root.

## Former gaps, now pinned as fixes

Three behaviours were once pinned as they *were*, not as they should be. The
fixes landed with the medium-severity review items, and each case now asserts
the fixed behaviour under the same id (so the history stays readable):

| Case | What it asserts now |
|------|---------------------|
| `transport.openapi-auth` | `GET /rpc/openapi.json` without a token is 401. It used to be served straight off the mux, outside the bearer-token middleware, so the tool inventory was readable by anyone who could reach the port. |
| `transport.preflight-allowed` | An unauthenticated `OPTIONS` preflight is answered 200 with the CORS headers. It used to be 401, because `ValidateToken` wrapped `CORSMiddleware` and the preflight a browser sends carries no `Authorization` header by design. |
| `sandbox.http-method-convenience` | `host.http.post` is refused when only `GET` is in `allowed_http_methods`, and the request is never sent. All eight helpers now share one gate. |

## Limits worth knowing

- **`require()` at load time is verified once.** A plugin that calls `require()`
  at the top level is checked when the server starts; only a `require()` *inside*
  `call()` is re-read and re-verified per call. The manifest cases cover the
  call-time path, which is the one that can change under a running server.
- **Revocation is asynchronous.** The server reloads the revocation file on a 5 s
  timer, so `auth.revoked-token` is the one case that waits rather than polls a
  sub-second event. It allows up to 12 s and usually finishes in ~5 s.
- **Ports are allocated, not fixed.** Each server gets a free port picked at
  config-write time; a collision in the microseconds between closing the probe
  socket and the server binding would fail that test with "address already in
  use", which is also what `startup.port-in-use` asserts deliberately.
- **`digest` needs at least one argument.** `mcphe digest` with no arguments
  prints usage and exits 2, so "scan the configured plugin_dir" means
  `mcphe digest -format text`. `startup.defaults` relies on that.

## Live tests

```sh
cd <repo root>
export MCPHE_LIVE_BASE_URL=http://127.0.0.1:8001
export MCPHE_LIVE_TOKEN=<token>            # if the server authenticates
export MCPHE_LIVE_TOOL=ping                # optional: a tool to call
export MCPHE_LIVE_ARGS='{}'                # optional: its arguments
python3 -m unittest tests.test_live -v
```

Without `MCPHE_LIVE_BASE_URL` the module skips itself. Use it to check a
deployment — a container image, a package install, a service unit — where what
you want to prove is that *this* server, with *this* config, serves the tools it
should.

## Running the server locally

For manual work, build and run it the way an operator would:

```sh
make build
./mcphe config.yaml                    # HTTP on http://127.0.0.1:8001/rpc
./mcphe token create alice 30d         # a token for that config's token_secret
```

`config.example.yaml` documents every key. For a throwaway environment, point
`plugin_dir` at `plugins/` (the JavaScript plugins in the repo) or at
`plugin-devel/dist/` (the TypeScript build) and list the tools you want:

```yaml
transport: http
host: "127.0.0.1"
port: "8001"
plugin_dir: "plugins"
plugins:
  ping:
  get_datetime:
```
