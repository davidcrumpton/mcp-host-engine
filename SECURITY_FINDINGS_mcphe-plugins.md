# SECURITY_FINDINGS — mcphe plugins

Review of the 25 TypeScript plugins in `plugin-devel/plugins/`, performed against
`CODE_SECURITY_PROMPT_TS.md`. Findings are for the plugin sources; where a bug is also
present in the deployed `plugins/*.js` artifacts I say so, because those are what mcphe
actually loads.

## Method

Four parallel reviews covered the plugin set, then **every High finding was re-verified
against the source by hand**, along with the host facts the review depends on:

- `host/httpclient/httpclient.go` — `request` returns `{status, headers, body}`, never throws on non-2xx
- `host/host.go:196` — `request` takes one options object, not positional args
- `host/env/env.go:11-28` — `process.env` **throws** for a non-allow-listed key
- `host/fs/fs.go:24-42` — paths are symlink-resolved and boundary-checked
- `config/config.go:627-660` — `MaskKeyValue` / `IsSensitiveName` redaction rules
- `config.yaml` — the shipped per-plugin config

Marked **[V]** = verified by me against source. Marked **[U]** = reported by a reviewer,
plausible, but not independently confirmed (usually because it depends on a remote API).

Severity summary: **10 High · 17 Medium · 19 Low**.

---

## Fix status

First remediation tranche applied: **H1–H10** (all High findings), plus M1 and M2 in the files
touched. Each fix has a regression test that was confirmed to **fail against the pre-fix
source** and pass after (see "Verification" below).

Second tranche: **H11–H12 and M1–M17** (all remaining High and all Medium findings). The Low
findings are untouched.

| ID | Status | Notes |
|----|--------|-------|
| H1 | Fixed | Duplicate config block deleted; `baseUrl` hoisted to `let` |
| H2 | Fixed | All 13 write sites flattened to a top-level header map |
| H3 | Fixed | `gmail`, `microsoft_outlook`: token moved to `Authorization: Bearer` |
| H4 | Fixed | `encodeURIComponent` on `label`, `message_id`, `query`; required params validated |
| H5 | Fixed | `proxmox`: `JSON.parse` now applied to `resp.body`, not the response object |
| H6 | Fixed | `microsoft_outlook`: same class of bug as H5 |
| H7 | Fixed | `gitea`: `headers.toString()` → `headers` (7 sites) |
| H8 | Fixed | `gitea`: success now from `response.status`, not `body.includes("ok")` (13 sites) |
| H9 | Fixed | `gitea`: `config.options` guarded |
| H10 | Fixed | `wikipedia_search`: `language` validated against an allow-list; **severity downgraded — was not a live SSRF**, but the same config bug broke 19 of 20 languages |
| H11 | Fixed | `memory`: agent-supplied `session_id` must match the `Mcp-Session-Id` header; traversal ids rejected |
| H12 | Fixed | `mcwsd`: workspace credential is keyed off the session, not an agent-supplied id |
| M1 | Fixed | `envOr` helper applied across every plugin; config-only setups reach the fallback |
| M2 | Fixed | `proxmox`: `mode:"raw"` is opt-in and traversal-sanitised |
| M3 | Fixed | `kibana`, `opensearch`: half-configured credentials throw instead of sending unauthenticated |
| M4 | Fixed | `home_assistant`, `zabbix`: `destructiveHint` corrected to `true` |
| M5 | Fixed | `gitea`: omitted visibility defaults to private; `internal` refused rather than sent as public |
| M6 | Fixed | `zabbix`: `limit` bound re-applied after agent `params` are merged |
| M7 | Fixed | `mcwsd`: workspace tokens pruned on destroy |
| M8 | Fixed | `mcwsd`: token redacted from tool results |
| M9 | Fixed | `memory`: write failures propagate; `set` no longer reports false success |
| M10 | Fixed | `memory`: a corrupt store is refused, not silently replaced with `{}` |
| M11 | Fixed | `memory`: retention and per-call rewrite bounded |
| M12 | Fixed | `memory`: `data_dir` is required; predictable `/tmp` default removed |
| M13 | Fixed | `websearch`: de-redirects via `host.url` instead of `new URL()` |
| M14 | Fixed | `gitea`, `elastic7`, `elastic8`, `kibana`, `opensearch`: agent-chosen index/repo validated against `allowed_index_patterns`; wildcards refused by default |
| M15 | Fixed | `env`: description no longer presents an agent-chosen env var read as inert |
| M16 | Fixed | `run_command`: description matches the host — no shell, the allow-list is the boundary |
| M17 | Fixed | `wikijs`, `gmail`: response bodies parsed; upstream headers no longer returned to the model |

Two corrections to the original write-up, found while writing the tests:

- **M2 was overstated for `gitlab`.** The unguarded `host.config.options` read at the old
  line 173 sat *inside* the try/catch, so an absent `options` block was caught and returned
  an equivalent error. It was not exploitable; H1 (the duplicate log) was the real bug in
  that block. The M2 risk remains genuine for other files where the read is outside a guard
  and will be assessed per-file in the M1/M2 pass.
- **H1 is only reachable when `GITLAB_API_KEY` is allow-listed.** If the env var is not
  allow-listed, `process.env` throws first and the plugin returns early, so the raw-token
  log line was never reached. The leak was real but conditional on config — which is why
  the regression test allow-lists the env vars to exercise the true path.
- **H10 was overstated.** It was rated High as a live SSRF to the cloud metadata endpoint.
  Running the payloads through the real `isDomainAllowed` showed every one was blocked by
  the host allow-list. The plugin flaw was genuine but contained; the *actual* shipped
  defect was the mirror image — `allowed_domains` was en-only, so 19 of 20 languages failed.

**Not yet fixed:** the 19 Low findings.

### Verification

Regression tests were added to `gmail`, `microsoft_outlook`, `proxmox`, `gitea`, `gitlab`
and `wikipedia_search` (51 new cases; suite 100 → 149, all passing), plus 3 Go tests in
`config/wikipedia_langs_test.go`. Each was run against the stashed pre-fix source to confirm
it actually catches the bug:

| Plugin | Failing before fix |
|--------|--------------------|
| `proxmox` | 4 of 6 |
| `gmail` | 5 of 8 |
| `gitea` | 10 of 14 |
| `gitlab` | 2 of 6 |
| `wikipedia_search` | 12 of 19 |
| `config` (Go) | 2 of 3 |

The second tranche adds `plugins/medium_fixes.test.ts` (25 cases) covering M1, M2, M3, M5, M6,
M9–M14, H11 and H12. Suite is now **174 tests across 25 files, all passing**.

Two fixes in this tranche (`elastic7` M1/M14 and `gitea` M5) were written, lost to a bad
backup during the commit, and re-applied afterwards — the committed test file caught them,
which is why they are listed here rather than silently absent.

`tsc --noEmit` error count went **417 → 403** with no new error kinds in plugin source (the
17 additional errors are all inside the two new test files, matching the existing pattern for
plugin tests). Notably the type checker had been flagging H2 all along: 7 × `TS2322: Type
'{ Authorization: ... }' is not assignable to type 'string'`, now gone. `go build ./...` and
`go test ./...` are clean.

One deployment caveat: `plugins/microsoft_outlook.js` is listed in `.gitignore` (as are
`elastic7.js` and `elastic8.js`), so the Outlook fix does not ship via the repo until that
entry is removed. The source fix is correct and tested regardless.

---


# HIGH

## H1. `gitlab` — API token written to the log in clear text
`gitlab/plugin.ts:176` **[V]**

```ts
host.server.logger(1, `GitLab plugin config: apiKey=${apiKey}, baseUrl=${baseUrl}`);
```

Line 164 masks the same value (`apiKey=${apiKey ? '***' : 'MISSING'}`); lines 173-176 are a
duplicate block outside the try/catch that logs it raw. `logger` is `LogfForPlugin` →
`fmt.Fprintf(os.Stderr)`, gated only at level 1, which the shipped `config.yaml:14`
(`verbosity_level: 4`) satisfies.

**Trigger:** any GitLab call, successful or not — the PAT lands in the daemon log.
**Fix:** delete lines 173-176; they duplicate 163-164.

## H2. `gitlab` — nested `{ headers: {...} }` form defeats redaction, putting the bearer token in DEBUG logs
`gitlab/plugin.ts:254, 306, 349, 397, 444, 489, 540, 594, 636, 682, 734, 784, 834` **[V]**

The write handlers pass `host.httpPut(url, { headers: { Authorization: token, ... } }, body)`.
The Go binding tolerates that wrapper (`httpclient.go:153-167`), so the header *is* sent —
but the log line redacts on the **outer key name**:

```go
cfg.LogfWithContext(4, ..., "HTTP request header inbound: %s: %v", k, cfg.MaskKeyValue(k, v))
```

with `k = "headers"`. `IsSensitiveName("headers")` checks for the substrings
`auth/key/token/secret/cookie/password/passwd/credential/signature/session` — `"headers"`
matches none, so `MaskKeyValue` returns the inner map **verbatim**:

```
HTTP request header inbound: headers: map[Authorization:Bearer glpat-… ]
```

Had the flat form been used, `k = "Authorization"` would have matched `auth` and masked to `***`.
The six GET handlers already use the flat form; only the writes regressed.

**Trigger:** any `create_or_update_file` / `create_issue` / `create_repository` at the shipped
verbosity — token to stderr on every request.
**Fix:** pass the flat object (`host.httpPut(url, { "Authorization": token, ... }, body)`).
Secondary **[U]**: if that compatibility branch is ever removed, all writes silently lose auth.

## H3. `gmail` and `microsoft_outlook` — API token placed in the query string
`gmail/plugin.ts:79, 87, 95, 103`; `microsoft_outlook/plugin.ts:84, 94, 104` **[V]**

```ts
const url = `${apiUrl}/users/me/labels?access_token=${apiToken}`;
```

The credential travels as a URL parameter: it lands in proxy/access logs, in `Referer` on
redirect, and in the HTTP error text that the surrounding `catch` returns to the model.
`microsoft_outlook/plugin.ts:75` proves the plugin knows the correct form
(`headers: { Authorization: 'Bearer …' }`), so this is a copied-wrong convention, not a
host limitation.

**Fix:** drop `?access_token=`; send `Authorization: Bearer <token>` on all endpoints.
Preserve the existing logging discipline when fixing — do not "fix" it by logging headers.

## H4. `gmail` and `microsoft_outlook` — agent params concatenated into the URL unencoded
`gmail/plugin.ts:87, 95, 103`; `microsoft_outlook/plugin.ts:84, 94, 104` **[V]**

```ts
const url = `${apiUrl}/users/me/messages/${params.message_id}?access_token=${apiToken}`;
```

Two distinct consequences:
- **Parameter injection** — `{"query":"a&maxResults=1000"}` rewrites the query instead of searching for the literal string; Outlook's `&$top=1000` does the same.
- **Endpoint retargeting** — `{"message_id":"../labels"}` normalises to `/users/me/labels`: the "read one message" branch reaches a different API path with the daemon's token.

Also schema drift: `label`, `message_id`, `query` are not in `required` and are dereferenced
unguarded, so a missing one sends the literal `undefined`.

**Fix:** `encodeURIComponent` on the path segment and on each query value; reject the
branch's required field when absent. The correct pattern already exists in
`wikipedia_search:134-146`.

## H5. `proxmox` — `JSON.parse()` applied to the response *object*; the plugin is dead
`proxmox/plugin.ts:90` **[V]** — present in deployed `plugins/proxmox.js`

```ts
const resp = JSON.parse(host.http.request({ method: "GET", url, headers }));
```

`host.http.request` returns a Go `map[string]interface{}`, not a JSON string. `JSON.parse`
coerces it to `"[object Object]"` and throws. The throw is caught at line 108, so **every**
proxmox call in **every** mode returns `{success:false, error:"<syntax error>"}`. The status
handling at 92-100 and the real parse at 102 are unreachable dead code — an operator reading
this file believes 4xx/5xx is handled.

**Trigger:** `{mode:"list_vms"}` against a healthy Proxmox → parse error, always.
**Fix:** `const resp = host.http.request({...});` then check `resp.status`, then `JSON.parse(resp.body)`.
*Functional break rather than a vulnerability, but certain and total.*

## H6. `microsoft_outlook` — same `JSON.parse` bug on all four commands
`microsoft_outlook/plugin.ts:75, 85, 95, 105` **[V]** — present in deployed `plugins/microsoft_outlook.js`

Same root cause as H5. All four Outlook commands fail on every call, and the resulting
error text is a JSON syntax error that names the wrong cause.

## H7. `gitea` — `headers.toString()` sends `"[object Object]"`, so authentication is silently dropped
`gitea/plugin.ts:211, 288, 354, 384, 544, 571, 598` **[V]** — 7 sites in deployed `plugins/gitea.js`

```ts
headers: headers.toString()      // → "[object Object]"
```

The Go binding does `if h, ok := opts["headers"].(map[string]interface{}); ok` — the assertion
fails, `headersVal` stays nil, `applyHeaders` iterates nothing, and **no error is raised**. The
request goes out unauthenticated. The same file passes `headers: headers` correctly at seven
other sites (227, 260, 323, 417, 453, 484, 526), so it is inconsistent with itself.

**Trigger:** `{CommandEvent:"get_file_contents", project_id:"owner/private-repo", ...}` → 401/404
even with a valid `GITEA_API_KEY`.
**Fix:** `headers: headers` at all seven sites.

## H8. `gitea` — success decided by substring-matching `"ok"` in the body; `status` never checked
`gitea/plugin.ts` — 13 sites, one per handler **[V]** — **FIXED**

```ts
if (!response.body.includes("ok")) {
```

No handler inspected `response.status`. Both directions were wrong, and both are now
confirmed against Gitea's own source rather than inferred:

- **False success (dangerous).** Gitea answers a missing/expired token with 401
  `{"message":"token is required"}` and an under-scoped token with 403
  `{"message":"token does not have at least one of required scope(s), …"}`
  (`routers/api/v1/api.go`). **Both contain `ok` inside `token`**, so every failed write
  returned `{success:true, …}`. Confirmed empirically: a 401 from `create_issue` produced
  `success: true` on the pre-fix code.
- **False failure.** Gitea's write endpoints return the created object with no `ok` field —
  `FileResponse` is `{content, commit}` and `Repository` has no `ok`
  (`modules/structs/repo_file.go`, `repo.go`). So every *successful* `create_or_update_file`,
  `push_files`, `create_repository`, `create_issue`, `create_merge_request`, `create_branch`
  and `fork_repository` was reported as a failure, inviting the caller to retry a write that
  had already landed.

The heuristic happened to be correct only for the search endpoints, which really do return
`{"ok":true,"data":[…],"total":N}`.

**Fix:** a `giteaResult(response)` helper decides from `response.status` (2xx = success),
extracts `data.message` for a readable error, and degrades gracefully when an error body is
not JSON (a proxy HTML page previously escaped as an uncaught `JSON.parse` throw, since the
file has no `catch` blocks). Two adjacent bugs found while fixing this:
- `createOrUpdateFile` parsed the pre-lookup GET body without checking status; a 404 (the
  normal create path) set `sha` from an error body. It now only trusts a 200.
- `pushFiles` reported `` `Pushed ${files.length} files` ``, but `files` is a
  `Record<path, content>` with no `.length`, so it always printed "Pushed undefined files".

## H9. `gitea` — `host.config.options` is dereferenced but absent from the shipped config; every call throws
`gitea/plugin.ts:155-156` **[V]**

```ts
const giteaApiKey = host.process.env("GITEA_API_KEY") || host.config.options.giteaApiKey;
const giteaApiUrl = host.process.env("GITEA_API_URL") || host.config.options.giteaApiUrl || "https://try.gitea.io";
```

`config.yaml:80-82` gives `gitea` only `allowed_env_vars` and `allowed_domains` — **no `options:`
block**. `GITEA_API_URL` is allow-listed but ships commented out in `.env`, so it returns `""`
and the right-hand side evaluates → `TypeError: Cannot read property 'giteaApiUrl' of undefined`.
`gitea/plugin.ts` contains **zero** `catch` blocks, so the exception escapes `call()`.

**Fix:** `const opts = host.config.options || {};` and read from `opts`.

## H10. `wikipedia_search` — agent-controlled `language` becomes the request *host*
`wikipedia_search/plugin.ts:72, 131, 140, 145` **[V]** — **FIXED (severity downgraded: was not a live SSRF)**

```ts
const language = String(params.language || "en").trim().toLowerCase() || "en";
const base = "https://" + lang + ".wikipedia.org/w/api.php";
```

`language` was only trimmed and lowercased — never validated against the `LANG_NAMES` map that
already existed, and never pattern-checked. It was concatenated into the **authority** of the
URL, so `/`, `?` and `#` retargeted the request.

**Correction to the original write-up.** I rated this High and claimed it reached the cloud
metadata endpoint. That was wrong, and I checked it by running the real payloads through the
real `isDomainAllowed` with the shipped allow-list:

| `language` | parsed host | allowed |
|---|---|---|
| `169.254.169.254/latest/meta-data#` | `169.254.169.254` | **false** |
| `en.wikipedia.org@evil.com#` | `evil.com` | **false** |
| `evil.com?x#` | `evil.com` | **false** |
| `../internal` | `..` | **false** |
| `localhost:8080/x#` | `localhost` | **false** |

The Go host checks `allowed_domains` on the *parsed* hostname before issuing the request
(`host/httpclient/httpclient.go:78`), so every injected host was blocked. The plugin-level
flaw was real but was **contained by the host allow-list** — defence in depth held. It is a
latent vulnerability, not an active one: it would become live the moment anyone widened
`allowed_domains` (see below), which is exactly what the natural fix invites.

**The real bug it exposed was the opposite.** `allowed_domains` was `["en.wikipedia.org"]`, and
`isDomainAllowed` matches a host or a subdomain of it — so `fr.wikipedia.org` was **blocked**.
19 of the 20 advertised language editions failed:

```
{"query":"OpenBSD","language":"fr"}
→ {"success":false,"result":"Unexpected error: GoError: access to fr.wikipedia.org is not allowed"}
```

So the plugin advertised 20 languages and shipped exactly one.

**Fix:**
- `resolveLang()` validates against `LANG_NAMES` and **rejects** an unknown code with the list
  of valid ones. Rejecting rather than silently falling back to English: returning English
  text to a caller who asked for French presents the wrong language as the right answer.
- `wikiHost()` is now the single place a language code becomes a hostname; `apiUrl`,
  `restSummaryUrl` and `pageUrl` all go through it and it throws on an unknown code. A future
  call path cannot reintroduce the injection by forgetting to validate first.
- The `language` schema property gained an `enum` matching `LANG_NAMES`, so the agent is
  guided to a valid code instead of learning by error.
- `allowed_domains` (both `config.yaml` and `config/config.go`'s `DefaultConfig`) now lists all
  20 language hosts **explicitly** — no wildcard, so the boundary stays exact and auditable —
  which is what makes the advertised feature actually work.

**Regression guard:** `config/wikipedia_langs_test.go` reads `LANG_NAMES` out of the plugin
source and asserts both configs cover every code, so the two lists cannot drift apart again.
That is the invariant whose violation caused this bug.

## H11. `memory` — agent-supplied `session_id` overrides the session header (cross-tenant read/write)
`memory/plugin.ts:62, 67-73` **[V]**

```ts
const currentSession = session_id
  || (host.server.httpHeaders && host.server.httpHeaders['Mcp-Session-Id'])
  || host.config.options.session_key
  || 'default';
const memoryFile = `${MEMORY_DIR_BASE}/${currentSession}_memory.json`;
```

The `Mcp-Session-Id` header is the only value tied to the caller's identity, and `params.session_id`
overrides it unconditionally, with no check that they agree. Session ids are frequently visible
to the model in earlier tool output, so enumeration is realistic.

**Trigger:** `{"action":"list","session_id":"<victim>"}` returns the victim's whole key/value store
(including any secrets previously stored); `{"action":"set","session_id":"<victim>",…}` overwrites it.
**Fix:** reject `session_id` when it differs from the `Mcp-Session-Id` header, or remove it from the schema.

## H12. `mcwsd` — agent-supplied `workspace_id` selects the credential, with an admin-token fallback
`mcwsd/plugin.ts:342-346` **[V]**

```ts
const token = this._workspaceTokens[id] || adminToken;
```

Two failure modes:
1. **Unknown id → admin token.** An id absent from `_workspaceTokens` falls back to the plugin's control-plane token, which the in-file comment (line 326) says the daemon accepts in `scoped` mode — valid for *every* workspace.
2. **Known id → another caller's token.** The map is instance-level and shared, so supplying an id that some other caller provisioned authenticates as that workspace.

**Trigger:** `exec_command` with `{workspace_id:"ws-of-another-agent", command:"cat ~/.ssh/id_rsa"}`.
**Fix:** don't derive the credential from `params.workspace_id` alone — bind the token map to a
host-provided caller identity and verify ownership; **refuse** rather than fall back to admin.
Impact depends on the daemon's `--workspace-auth-mode` **[U]**; the fallback is documented in-file.

---

# MEDIUM

## M1. Twelve plugins — `host.process.env(x) || cfg.y` is unreachable; config-only setup fails
`gitea:155-156`, `gitlab:160`, `proxmox:61-67`, `elastic7:29-30`, `elastic8:28-29`,
`kibana:34-36`, `opensearch:34-36`, `home_assistant:164`, `zabbix:57`, `wikijs:56-60`,
`gmail:52-62`, `microsoft_outlook:33-43` **[V]**

`host.process.env` **throws** for a key not in `allowed_env_vars` (verified in `host/env/env.go:11-28`);
it does not return `undefined`. So the intended env-over-config precedence can only reach the
`||` right-hand side when the variable is allow-listed *and* empty. When an operator configures
via `options` alone and does not list the env var, the lookup throws, the enclosing `catch`
fires, and the plugin reports a config error naming an env var the operator never used.

**Fix (one shared helper, all files):**
```ts
function envOr(name: string, fallback: string): string {
  try { return host.process.env(name) || fallback; } catch (e) { return fallback; }
}
```
Note `mcwsd` already does this correctly (`envOr`, lines 48-54) — copy that, and keep the
env-over-config precedence it preserves.

## M2. `proxmox` — `mode:"raw"` forwards an agent-supplied path to the authenticated API
`proxmox/plugin.ts:177-180`, `73-78` **[V]**

`buildUrl` only prepends `/api2/json`; nothing validates `path`. The nine declared modes expose a
curated subset, but `raw` exposes the whole API surface with the `PVEAPIToken`.

**Trigger:** `{mode:"raw", path:"/access/users"}` → full user/realm dump; `/access/acl`, `/cluster/backup`,
`/nodes/pve/qemu/100/cloudinit/dump`. The path is also unencoded, so `/../../` escapes the API prefix
(the host's domain allow-list constrains host and port only), and it reaches the log line at 89 verbatim
(log forging).
`readOnlyHint:true` is technically honest (the method is pinned to GET), so this is a
**confidentiality** bypass, not a write primitive.
**Fix:** remove `raw`, or gate it behind `cfg.allow_raw_paths === true` and reject `..`, `#` and control
characters, encoding each segment.

## M3. `kibana` / `opensearch` — proceed **unauthenticated** when credentials are half-configured
`kibana/plugin.ts:47-49`, `opensearch/plugin.ts:48-50` **[V]**

```ts
if (username && password) { headers["Authorization"] = "Basic " + ... }
```

Only `baseUrl` is required. If one of the two is set, the request goes out with no `Authorization`
header at all — no error, no warning — and the model receives whatever anonymous callers get.
Compare proxmox/elastic, which refuse when the credential is missing.
**Fix:** require both or neither.

## M4. `home_assistant` / `zabbix` — `destructiveHint: false` on plugins that delete state
`home_assistant/plugin.ts:31`, `zabbix/plugin.ts:10` **[V]**

Both expose destructive operations (`home_assistant`: `delete_state`, plus `call_service` reaching
`lock.unlock`/`homeassistant.restart`/`shell_command.*`; `zabbix`: `delete_host`, `update_item`).
These hints are permission signals surfaced to the model, so an inaccurate hint is a safety defect,
not metadata.
**Fix:** `destructiveHint: true` on both.

## M5. `gitea` — `visibility:"internal"` silently creates a **public** repository
`gitea/plugin.ts:315` **[V]**

```ts
private: visibility === 'private',
```

The schema offers `enum: ["private","internal","public"]`; both `internal` and `public` map to
`private: false`, which Gitea treats as public. The model asks for the middle setting and gets the
weakest one.
**Fix:** reject `internal` explicitly, or map it to `private: true`.

## M6. `zabbix` — agent `params` overrides the safe `limit` default
`zabbix/plugin.ts:225` (and 218, 233, 244) **[V]**

```ts
const rpcParams = Object.assign({ output: "extend", limit: 100, ... }, p);
```

The bounding default is set and then immediately overridable by the agent: `{limit: 10000000}` or a
negative value (Zabbix treats some negatives as unlimited) defeats it.
**Fix:** re-apply the bound after the merge.

## M7. `mcwsd` — workspace tokens retained unboundedly, never pruned on destroy
`mcwsd/plugin.ts:328, 336-340, 534-547` **[V]**

`_workspaceTokens` only ever grows; `destroyWorkspace` never deletes the entry, so a workspace's
bearer token stays in memory for the process lifetime after the workspace is gone. No cap. Since the
map is shared across differently-configured instances, a colliding `workspace_id` could present
daemon A's token to daemon B **[U]** (depends on loader caching).
**Fix:** `delete` on successful destroy; cap the map.

## M8. `mcwsd` — workspace token echoed to the model in tool results
`mcwsd/plugin.ts:421-427, 645-654` via `_handleResponse:353` **[V]**

`_handleResponse` returns the entire parsed daemon response, and the provisioning responses contain
`workspace_token` — which the plugin itself reads at 425/651. That bearer secret is handed to the
agent, where a prompt-injected instruction can exfiltrate it.
**Fix:** strip `workspace_token` (and nested `workspace.workspace_token`) before returning.

## M9. `memory` — write failure swallowed; `set` reports success
`memory/plugin.ts:29-37, 83-87` **[V]**

`saveAll()` catches `writeFile`/`renameFile` failures, deletes the temp file, and returns nothing —
then `set` returns `{success:true, result:"Memory set: …"}`. A later `get` returns nothing. This is a
silent data-loss path on the plugin's only persistent side effect.
**Fix:** have `saveAll` report failure and propagate it.

## M10. `memory` — a corrupt file silently becomes `{}` and is then overwritten
`memory/plugin.ts:15-22` **[V]**

`loadAll` catches parse errors and returns `{}`; the next `set` writes a one-key map over the
existing file, destroying every prior entry.
**Fix:** distinguish "missing" from "unparseable"; refuse to write on a parse error.

## M11. `memory` — unbounded retention and whole-file rewrite per call
`memory/plugin.ts:82, 76, 83` **[V]**

No cap on key count or value size; a single 10 MB value makes every subsequent call re-read and
re-parse it, and every write re-serialise it (`JSON.stringify(data, null, 2)` inflates it further).
**Fix:** cap value length and entry count; store compact JSON.

## M12. `memory` — predictable `/tmp` default with no permissions
`memory/plugin.ts:64, 128` **[V]**

`host.config.options.data_dir || '/tmp/mcphe.' + host.pid` — world-writable, guessable pid, no mode
set, so another local user can pre-create the directory. `createDataDir()` also runs on every call
outside any try/catch, and `host.config.options` is dereferenced unguarded (a `TypeError` if `options`
is absent).
**Fix:** require an explicit `data_dir`; guard `options`; tolerate "already exists".

## M13. `websearch` — `new URL()` does not exist in goja; de-redirecting never runs
`websearch/plugin.ts:70` **[V]**

```ts
const u = new URL(href, 'https://duckduckgo.com');
```

The host contract states there is no `URL` constructor. The resulting `ReferenceError` is swallowed by
the `catch` at 73-75 on every iteration, so `results[].url` is always the raw
`//duckduckgo.com/l/?uddg=…` wrapper instead of the target page — silent, permanent wrong output.
**Fix:** use the injected `host.url`, or parse `uddg=` with a string operation.

## M14. `gitea` / `elastic7` / `elastic8` / `kibana` / `opensearch` — agent-chosen index/repo with no allow-list
`elastic7:38`, `elastic8:37`, `kibana:56`, `opensearch:56`; `gitea` project_id throughout **[V]**

`encodeURIComponent` correctly blocks `/`, `?`, `#`, `&` (a sound pattern — keep it), but does not
escape `*`. So `{index:"*", query:{size:10000,…}}` reaches every index the credential can see, and
`{indexName:".opendistro_security*"}` reaches the security index. Likewise `fork_repository` with an
agent-chosen `project_id` and `namespace` copies a private repo out under the plugin's own token.
This is agent-chosen target selection under the daemon's credentials; exploitability is entirely a
function of the token's scope, which no plugin checks or documents.
**Fix:** `allowed_index_patterns` / `allowed_projects` config, checked before building the request.

## M15. `env` — agent-chosen env var name is a secret-disclosure primitive presented as inert
`env/plugin.ts:21` **[V]**

```ts
return host.process.env(params.env_var);
```

`allowed_env_vars` is a coarse *name* allow-list: whatever the operator adds so the tool can do its
advertised job becomes readable by the model. `{env_var:"AWS_SECRET_ACCESS_KEY"}` returns the
credential verbatim. `readOnlyHint:true` is literally true (reading env is read-only) but makes a
credential read look inert. It fails closed with no allow-list, which is correct.
**Fix:** don't ship it by default; if kept, constrain `env_var` with an `enum` and state the risk in
the description.

## M16. `run_command` — description says "shell", the host has no shell; allow-list is the whole boundary
`run_command/plugin.ts:5, 18, 23` **[V]**

Only `argv[0]` is checked against `allowed_commands`; every later argument passes through untouched.
The plugin cannot add `--` and does not reject a leading `-`. The annotations are honest — this is the
defect is the misleading description: a model that emits `ps aux | grep nginx` gets `ps` with literal
`|` and `grep` arguments (silently wrong output it may then act on), and one that reads "shell command"
will also try `sh -c …`, which succeeds if `sh` is allow-listed. One allow-listed flag-bearing binary
is enough: `find /tmp -exec sh -c id ;`, `tar --checkpoint-action=exec=…`, `git --upload-pack=…`.
**Fix:** reword both descriptions to state there is no shell; document that `allowed_commands` is the
security boundary and must exclude interpreters, `find`, `tar`, `curl`, `git`.

## M17. `wikijs` / `gmail` — raw response object (with upstream headers) returned to the model
`wikijs/plugin.ts:74-80`, `gmail/plugin.ts:81, 89, 97, 105` **[V]**

The `{status, headers, body}` object is returned as the tool result: transport metadata and every
upstream header (including auth challenges) go into the model's context, the payload stays an
unparsed string, and a 401 from an expired token is shaped like a normal result with no
`success:false`. Only `wikipedia_search` gets status-then-parse right.
**Fix:** check `status`, `JSON.parse(body)`, return data only.

---

# LOW

**L1. `err.message` on a value that may not be an `Error`** — `mcwsd` (15 sites), `home_assistant`,
`zabbix`, `wikijs`, `wikipedia_search`, `websearch`, `gmail`, `outlook`, `proxmox`, `elastic7/8`,
`kibana`, `opensearch` **[U]**. If goja surfaces a Go error as a non-Error, `.message` is `undefined`
and the model gets `{success:false, error:undefined}`. Fix: `(err && err.message) || String(err)`.

**L2. Syntax newer than the build target** — `?.`/`??` in `mcwsd:256,407`, `home_assistant:370`,
`zabbix:139,143`, `get_ip:39-40`; `.flat()` in `zabbix:261`. `tsconfig` targets ES2015 and esbuild
transpiles `?.`/`??`, so those are fine; `.flat()` is ES2019 and is worth checking against the goja
version **[U]**. `response.text?.()` (`zabbix:143`) is dead code — the host returns `body`.

**L3. `websearch` — status never checked** (`:53`): a 429/challenge page yields `count:0`, indistinguishable
from a genuine empty result. Also `max_results` is used unvalidated (`:62`): omitted or non-numeric
makes the bound `NaN` (loop runs to end of page); `0`/negative returns `count:0`.

**L4. `wikipedia_search` — transport failure collapses into "not found"** (`:234,249,276,298,320,343`):
a 429/5xx becomes `"No Wikipedia articles found"`. Return a distinguishable sentinel and surface the status.

**L5. `google_search` / `get_ip` — no status check, no try/catch around `JSON.parse`**
(`google_search:29-30`, `get_ip:35-39`): a non-2xx HTML page or empty body becomes an uncaught
`SyntaxError`. `google_search:34` also uses `payload.items.length` without `Array.isArray`.

**L6. `elastic7` / `elastic8` — a `_search` 404 reported as "Record not found"** (`:51-52`, `:50-51`):
means "index missing", not "record absent". `baseUrl` is also not slash-normalised (`//index_search`),
unlike kibana/opensearch which strip it.

**L7. `kibana` / `opensearch` — `params.size || 10`** (`:52-53`): `{size:0}` silently becomes 10; a huge
`size` is forwarded unclamped.

**L8. Upstream error bodies echoed to the model** — `kibana:67`, `opensearch:67`, `proxmox:98`,
`mcwsd:358`, `home_assistant:386`. ES/daemon error bodies carry index names, mappings, stack traces.
Not credentials, but internal topology on a path the model can retry. Truncate and redact.

**L9. `proxmox` — `requireField` is a truthiness test** (`:114-118`): `vmid:0` is rejected as "missing",
a non-string object passes. `buildUrl` is also called outside the try (`:82`), so `{mode:"raw",path:{}}`
throws a bare `TypeError` instead of a structured failure.

**L10. Schema/runtime drift** — `gitlab:217` has `case "update_file"` but the enum at `:21-35` omits it;
`gitlab` `search_repositories`/`list_project_files` (`:337-343`, `:427-434`) interpolate `page`/`per_page`
without defaults, producing literal `page=undefined`; `gitlab` `pushFiles` (`:297`) and `gitea` `pushFiles`
(`:251`) `.map`/`.length` on a `files` param that is not `required`; `wikijs`/`gmail`/`outlook` dereference
branch-required fields that are optional in schema.

**L11. `gitea` — hardcoded repo template** (`:316-318`): every created repo is stamped MIT-licensed and
Node-gitignored with no way to change it.

**L12. `gitlab` — inconsistent failure shape** (`:179-186`): returns `{success:false, result: …}` while every
other path returns `{success:false, error: …}`.

**L13. `wikijs` — logs every query and page body** (`:88, 111-112`): `create_page`/`update_page` content is
written to the log at the error level, verbatim. No credential, but prompt-influenced content retained.

**L14. `memory` — `get` of an absent key returns the string `"undefined"` as success** (`:91-94`); and
`delete` uses `in` (`:98`), so `{key:"toString"}` reports a deletion that never happened.

**L15. `memory` — unsanitised `session_id` as a path component** (`:72`): traversal out of the allowed root
is blocked by the host, but the caller can create/read files anywhere *inside* a shared `data_dir`.

**L16. Regex cost** — `websearch:55-56` (`[\s\S]*?` rescan per anchor, ~O(n²) over the page);
`wikipedia_search:180` (`[[`-heavy text, quadratic, multiplied by the `{{…}}` loop). Input is
upstream, not agent-supplied. Cap input length before matching.

**L17. `types/mcphe.d.ts:36` declares the wrong `request` signature** (positional). The real binding takes
one options object. `mock-host.ts:128-132` deliberately accepts both, so a plugin written against the
`.d.ts` fails only in production — with a confusing "access to POST is not allowed" rather than a
signature error. The authoring guide repeats the wrong form (`README for agentic AI…/plugin.ts:100`,
and a third variant `host.process.env.get(...)` at 306-307).

**L18. `mock-host.ts` fidelity gaps** — `checkReadPath`/`checkWritePath` use `startsWith` with no
directory boundary (so `/tmp` accepts `/tmpfoo`); no symlink resolution; `runCommand` has no
`allowed_commands` option, so command rejection is untestable; `allowed_headers` is not modelled at all.

**L19. Tests are metadata-only for most plugins** — `gitlab`, `gitea`, `proxmox`, `elastic7`, `elastic8`,
`kibana`, `opensearch`, `google_search` assert only name/description/version. **None invokes `call()`**,
which is why H5/H8 shipped. Several tests define functions they never register (`read_file:32`,
`run_command:32`), send params the schema does not define (`Path` vs `path`, `{Command,Args}`), or are
commented out (`write_file:30-31`). No negative test exists anywhere for a non-2xx response, a non-JSON
body, a blocked env var, a blocked path, a disallowed command, or a wildcard index.

---

# Aggregate patterns (fix once, not per file)

**A1. One wrong assumption about `process.env`, copied into twelve plugins.** `env(x) || cfg.y` appears in
every API plugin. The intent (env overrides config) is right and should be preserved; the implementation
is broken everywhere it appears. Fix as a shared helper.

**A2. Three different treatments of the HTTP response, two of them wrong.** `outlook` parses the response
*object* (always throws); `gmail`/`wikijs` return the object raw (never parse, never check status); only
`wikipedia_search` does status-then-parse-body. Standardise on the third.

**A3. `microsoft_outlook` is a copy of `gmail` with a swapped base URL** — same command names, same
Gmail-shaped paths (`/users/me/messages`, `labelIds`) against an Outlook base. Its one improvement
(`Authorization: Bearer` on `list_labels`) was never back-ported. Fix token handling once in a shared
helper. *(Whether the paths are wrong for the real Outlook API is **[U]** — I could not read it.)*

**A4. URL-encoding discipline is inconsistent.** `wikipedia_search` and `websearch` encode correctly;
`gmail`/`outlook` encode nothing; `gitea` omits it on path/query but `gitlab` gets it right. The correct
pattern already exists in the tree, so these are regressions against it.

**A5. Only `wikipedia_search` distinguishes "transport failed" from "no data".** Everywhere else a 4xx/5xx
or an empty body becomes a plausible-looking empty success.

**A6. No plugin logs a credential except `gitlab`.** gmail/outlook's exposure is in the *request URL*, not
the logger. Preserve the current logging discipline when fixing H3/H4 — do not "fix" it by logging headers.

---

# Sound patterns — do not "fix" these into regressions

- **Flat header objects and credentials in `Authorization`**, never the URL — proxmox (`PVEAPIToken=`),
  elastic (`Bearer`), kibana/opensearch (`Basic`). (gmail/outlook are the exceptions: H3.)
- **`encodeURIComponent` on agent-controlled path segments** — `proxmox:139-175`, `gitlab:240-828`,
  `wikipedia_search:134-146`, `elastic7:38`, `elastic8:37`, `mcwsd:372-644`, and notably
  `kibana:56`/`opensearch:56` encoding the *entire* proxy path including `?size=…`, which is what stops
  `{size:"10&method=DELETE"}` from overriding `method=POST`.
- **Refusing to proceed when the base URL or token is missing** — `proxmox:69-71`, `elastic7:32-34`,
  `kibana:38-40`, `opensearch:38-40`, `mcwsd:267-272`, `home_assistant:175-180`, `wikijs:64-66`.
- **`mcwsd`'s `envOr` (48-54)** — the correct pattern for A1, and what makes its multi-instance
  config-over-env precedence actually work.
- **`mcwsd`'s status checks** on `destroy`/`cancel`/`write` (`:540, 669, 590`) — the correct failure path;
  `_handleResponse` should adopt their discipline, not replace them.
- **`zabbix`'s `rpc`** (`:156`) checks `parsed.error` on a 2xx body — the logical-error detection that
  `mcwsd`'s `_handleResponse` lacks (M8), and it redacts the token as `***` when logging (`:95`).
- **Explicit enum enforcement in code, not just schema** — `proxmox:156-158`, `:182-184`; `wikijs`'s
  `default:` branch rejecting unknown commands (`:257-259`).
- **Atomic temp-then-rename writes** (`memory:29-33`, `host/fs/fs.go:96-125`) and the host's bounded
  reads and symlink-resolving path check — no partial-write or traversal findings exist because the host
  handles them.
- **No `async call()`, no `await`, no `fetch`/`Buffer`/Node builtins** anywhere in the set; `this` is
  never relied on inside `call()`; no module-level mutable state outside `mcwsd`'s token map.
- **Honest annotations where they matter** — `run_command` and `write_file` correctly declare
  `readOnlyHint:false, destructiveHint:true`; `outlook`'s `readOnlyHint:true` is accurate (all four
  commands are GETs).

---

# Suggested fix order

1. **H1, H2** — remove the token log line and flatten the GitLab write headers. Two small edits, live
   credentials in the daemon log.
2. **H3, H4** — move gmail/outlook tokens to a header and encode their params. Credential leakage plus
   endpoint retargeting.
3. **H5, H6, H7** — the three "plugin cannot work" bugs (`proxmox`, `outlook`, `gitea` auth). Cheap, and
   each currently masks everything else in its file.
4. **H8** — replace `includes("ok")` with a status check in all 13 Gitea handlers. Most likely to have
   already caused silent damage: it reports failed writes as successes.
5. **H11, H12** — memory cross-session access and the mcwsd admin-token fallback. Highest blast radius
   of the authorization findings.
6. **H10** — Wikipedia `language` allow-list (SSRF).
7. **H9, M1** — the `env || config` helper across all twelve plugins (correctness; currently breaks
   config-only deployments).
8. **M2** — proxmox `raw` mode. **M3** — kibana/opensearch auth requirement.
9. **M4** — correct the two `destructiveHint` lies.
10. **M9-M12** — memory integrity and retention. **M5-M8** — Gitea visibility, Zabbix limit, mcwsd retention
    and token echo.
11. **M13-M17** — websearch, index allow-lists, env/run_command hardening, response-shape standardisation.
12. **L1-L19** — `err.message` sweep, `.d.ts` and mock fidelity, then the rest.

---

# Regression tests that would catch the High findings

1. **H5/H6** — mock `host.http.request` to return the real shape `{status:200, headers:{}, body:'{"data":[…]}'}`
   and assert `proxmox.call({mode:"list_vms"})` returns the data. Fails today with a `SyntaxError`.
2. **H7** — assert `gitea.call(...)` passes `headers` as an **object containing `Authorization`**, not a string.
   Requires the mock to assert header shape (currently it ignores `headers` entirely).
3. **H8** — `{status:404, body:'{"message":"… [name: okta/infra]"}'}` must yield `success:false`; and
   `{status:200, body:'[{"name":"README.md"}]'}` must yield `success:true`. Both directions fail today.
4. **H9/M1** — with `pluginConfig: {}` (no `options`) and `allowedEnv` excluding the vars, `call()` must return
   a structured `{success:false}`, not throw. Then with `pluginConfig:{options:{…}}` it must **succeed** — the
   mock's `process.env` already throws like the real binding, so this test is writable today.
5. **H10** — `call({query:"x", language:"169.254.169.254/latest/meta-data#"})`: assert no request URL host
   other than `*.wikipedia.org`.
6. **H11** — two calls with different `Mcp-Session-Id` headers, the second passing the first's `session_id`:
   assert `{success:false}` and no cross-session value returned.
7. **H12** — provision ws-A, then `exec_command` with `workspace_id` = ws-A from a different simulated caller:
   assert the request does **not** carry token-A; and with an unknown id, assert it does **not** fall back to
   the admin token.
8. **H1/H2** — assert `host.server.logger` was never called with a string containing the token, and that
   `httpPut`'s headers argument is a flat map whose `Authorization` is the token.
9. **H3/H4** — assert the outgoing request has `headers.Authorization === "Bearer …"`, that `url` contains no
   `access_token`, and that `message_id:"../labels"` stays inside a single path segment.
