# Running mcphe in containers

One image, two modes. The mode is **not** baked into the image — `transport` is
a config-file field, so the same image runs as an HTTP daemon or as a stdio
server depending on which config file it is handed at start:

| mode  | how it is selected                                   | client sees            |
|-------|------------------------------------------------------|------------------------|
| http  | config with `transport: http` mounted at `/etc/mcphe/config.yaml` | `http://host:8001/rpc` (streamable HTTP + SSE) |
| stdio | config with `transport: stdio` mounted at `/etc/mcphe/config.yaml` | JSON-RPC on stdin/stdout |

Everything else (binary, plugins, user, paths) is identical, so you build once
and switch modes at run time. That also means a rebuild is never needed when
your plugin set changes — plugins come from the config file and the env file.

```
Dockerfile              multi-stage build, non-root, one runtime image
compose.yaml            http service + stdio service, shared image/env/mounts
deploy/config.http.yaml container profile for HTTP mode (edit your plugin set here)
deploy/config.stdio.yaml container profile for stdio mode
deploy/.env.example     every per-plugin credential, in one place
deploy/run.sh           same two modes with Apple's `container` CLI (no compose)
```

## Quick start

### With compose (docker compose v2, podman compose, container-compose)

```bash
cp deploy/.env.example deploy/.env && $EDITOR deploy/.env   # secrets + plugin creds

docker compose up -d --build                # HTTP daemon on 127.0.0.1:8001
docker compose logs -f mcphe
docker compose down

docker compose run --rm -T mcphe-stdio      # stdio, protocol on your terminal
```

`mcphe-stdio` lives behind the `stdio` profile so it never starts with
`up` — a stdio server is only meaningful when a client owns its stdin/stdout.

### With Apple's `container` CLI (this Mac: no compose installed)

```bash
cp deploy/.env.example deploy/.env && $EDITOR deploy/.env

./deploy/run.sh build      # or: make image
./deploy/run.sh http       # or: make up      -> http://127.0.0.1:8001/rpc
./deploy/run.sh logs       # or: make logs
./deploy/run.sh stop       # or: make down
./deploy/run.sh stdio      # or: make stdio   -> JSON-RPC on this terminal
```

`run.sh` is deliberately a thin translation of `compose.yaml`: same env file,
same mounts, same config files. When you later install a compose
implementation, nothing has to be rewritten.

> Apple's `container` has no built-in compose. Community plugins exist
> (`container-compose`) and are looked up in
> `/usr/local/libexec/container-plugins/`; they are not installed here, which
> is why `run.sh` exists.

## Environment variables: why they are not in the image

`config.yaml` cannot know which plugins a deployment needs, and plugin
credentials differ per deployment, so the image ships **no** environment at
all. Everything arrives at run time from `deploy/.env` (`env_file:` in compose,
`--env-file` in `run.sh`).

Two separate gates keep that safe:

1. **Config-time:** `env://NAME` in a config file is expanded from the process
   environment when the config loads. This is how secrets get in without ever
   touching a YAML file that might be committed.
2. **Call-time:** a plugin can only read variables named in *its own*
   `allowed_env_vars:` list. Everything else in `.env` is invisible to it.

So adding a plugin is a two-file edit, never a rebuild:

```
deploy/config.http.yaml   add the plugin block (+ allowed_env_vars)
deploy/.env               add the matching credentials
                          restart the container
```

Three traps worth knowing:

* **`env://` is fatal when unset.** mcphe exits with
  `environment variable "X" is not set` rather than silently using an empty
  value. If you comment a plugin out of the config, comment its variables out
  of `.env` too.
* **`env://` only resolves at the top level of a plugin block**, not inside
  `options:`. `home_assistant` reads `HASS_URL`/`HASS_TOKEN` itself and falls
  back to `options.ApiUrl`, which is why that value is a literal URL.
* **`file://` refs point inside the container.** A host path like
  `file:///Users/you/.../config.yaml` does not exist in the image; use
  `file:///etc/mcphe/config.yaml`.

## Paths and mounts

| container path        | purpose                                          | mount |
|-----------------------|--------------------------------------------------|-------|
| `/etc/mcphe/config.yaml` | the active config — this is the mode switch    | bind, `:ro` |
| `/app/plugins`        | JS plugins (baked in from `plugins/`)            | optional bind, `:ro`, for live plugin edits |
| `/var/lib/mcphe`      | revocation list, `memory` plugin data, pid file  | bind to `deploy/state` |

State is a **bind mount**, not a named volume, on purpose: the container runs
as a non-root user and a fresh named volume is root-owned, which would make the
revocation list unwritable. `run.sh` builds with your host UID/GID so the
directory just works; for compose set `MCPHE_UID`/`MCPHE_GID` if your state
directory belongs to someone else.

## Variables

All optional, all with defaults — set them in the environment or in `deploy/.env`.

| variable | default | meaning |
|---|---|---|
| `MCPHE_IMAGE` | `mcphe:local` | image tag |
| `MCPHE_ENV_FILE` | `deploy/.env` | env file passed to the container |
| `MCPHE_CONFIG_HTTP` | `deploy/config.http.yaml` | config mounted in HTTP mode |
| `MCPHE_CONFIG_STDIO` | `deploy/config.stdio.yaml` | config mounted in stdio mode |
| `MCPHE_STATE_DIR` | `deploy/state` | host dir for `/var/lib/mcphe` |
| `MCPHE_BIND_IP` | `127.0.0.1` | host address the port is published on |
| `MCPHE_PORT` | `8001` | published/container port (must match the config's `port`) |
| `MCPHE_CONTAINER_NAME` | `mcphe` | container name |
| `MCPHE_UID` / `MCPHE_GID` | `10001` (compose), host uid/gid (`run.sh`) | container user |
| `MCPHE_ARCH` | host arch | build/run architecture for `container` |

## Gotchas

* **`host: 0.0.0.0`, not `127.0.0.1`.** A container listening on loopback is
  unreachable through the published port. `deploy/config.http.yaml` sets this.
* **stdio needs stdin and no TTY.** `run.sh stdio` uses `-i` without `-t`;
  compose uses `stdin_open: true` / `tty: false`, so run it with
  `docker compose run --rm -T`. A pty line-buffers and rewrites the stream,
  which corrupts MCP framing. `--progress none` keeps the CLI's own output off
  the pipe — stdout must carry protocol frames only. mcphe already logs to
  stderr, which is why `logs_as_json: true` is set in both profiles.
* **Keep stdin open for the session.** A client that writes its requests and
  immediately closes stdin can lose the responses: the transport sees EOF and
  shuts down. Normal MCP clients hold the pipe open; a `printf | container run`
  smoke test will not.
* **Services on the Mac.** Apple containers are on the `default` network
  (`192.168.64.0/24`); the Mac is the gateway at `192.168.64.1`. `localhost`
  inside the container is the container itself, and `host.docker.internal` does
  not exist. Add the gateway IP to a plugin's `allowed_domains` when it calls
  back to the host. Docker/podman users get `host.docker.internal` instead.
* **Vault.** `vault_addr: http://localhost:8200` will not resolve; use the
  host's address (`http://192.168.64.1:8200`) and set `VAULT_ROLE_ID` /
  `VAULT_SECRET_ID` (or `VAULT_TOKEN`) in `.env`.
* **HTTPS.** For `use_https: true`, mount `cert_file`/`key_file` into the
  container and point the config at those paths; `tls/` is excluded from the
  build context on purpose.
* **Auth is on by default.** Both profiles set `token_secret: env://MCPHE_TOKEN_SECRET`,
  so the HTTP endpoint returns 401 without a token. Mint one on the host
  against the same secret:
  ```bash
  MCPHE_TOKEN_SECRET=... MCPHE_REVOKE_FILE=... \
    ./mcphe token create -config deploy/config.http.yaml alice 30d
  ```
  Then send `Authorization: Bearer <token>`. The healthcheck deliberately uses
  `/rpc/openapi.json`, which is served outside the auth middleware.
* **Changing plugins in the image.** Plugins are baked in at build time
  (`COPY plugins/`). To iterate without rebuilding, bind-mount
  `./plugins:/app/plugins:ro` (commented out in `compose.yaml`); plugins load
  at startup only, so restart the container afterwards.
* **`container build` is `linux/arm64` here.** `run.sh` passes the host arch;
  on Apple silicon that means arm64 only, no emulation.
