#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# deploy/run.sh — run mcphe in Apple's `container` CLI.
#
# This is the same two modes as compose.yaml, expressed in plain `container`
# commands, for machines that have Apple's CLI but no compose implementation
# (no `docker compose`, no `podman compose`, no container-compose plugin).
# Same env file, same mounts, same config files — so you can switch to
# `docker compose` later without changing anything else.
#
#   ./deploy/run.sh build              # build the image
#   ./deploy/run.sh http               # start the HTTP daemon (detached)
#   ./deploy/run.sh stdio              # run stdio mode on this terminal's pipes
#   ./deploy/run.sh logs               # follow daemon logs
#   ./deploy/run.sh shell              # debug shell inside the image
#   ./deploy/run.sh stop               # stop/remove the daemon
#   ./deploy/run.sh status             # what's running
# ---------------------------------------------------------------------------
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)

IMAGE=${MCPHE_IMAGE:-mcphe:local}
ENV_FILE=${MCPHE_ENV_FILE:-$ROOT/deploy/.env}
STATE_DIR=${MCPHE_STATE_DIR:-$ROOT/deploy/state}
CONFIG_HTTP=${MCPHE_CONFIG_HTTP:-$ROOT/deploy/config.http.yaml}
CONFIG_STDIO=${MCPHE_CONFIG_STDIO:-$ROOT/deploy/config.stdio.yaml}
CONTAINER_NAME=${MCPHE_CONTAINER_NAME:-mcphe}
PORT=${MCPHE_PORT:-8001}
BIND_IP=${MCPHE_BIND_IP:-127.0.0.1}
# Apple container builds/runs linux/arm64 on Apple silicon by default.
ARCH=${MCPHE_ARCH:-$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')}

log()  { printf '==> %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

need_env_file() {
  [[ -f $ENV_FILE ]] || die "missing $ENV_FILE — run: cp deploy/.env.example deploy/.env && \$EDITOR deploy/.env"
}

# Common mounts: writable state dir (revocation list, memory plugin data).
prepare_state() {
  mkdir -p "$STATE_DIR"
  # Make sure the container user can write, whatever UID the image was built
  # with (run.sh builds with the host UID by default, so this is usually a
  # no-op).
  chmod u+rwx "$STATE_DIR" 2>/dev/null || true
}

cmd_build() {
  local version commit uid gid
  version=$(cat "$ROOT/VERSION" 2>/dev/null || echo dev)
  commit=$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo none)
  uid=$(id -u)
  gid=$(id -g)

  log "building $IMAGE for linux/$ARCH (VERSION=$version COMMIT=$commit UID=$uid GID=$gid)"
  container build \
    --arch "$ARCH" \
    --tag "$IMAGE" \
    --build-arg "VERSION=$version" \
    --build-arg "COMMIT=$commit" \
    --build-arg "UID=$uid" \
    --build-arg "GID=$gid" \
    "$ROOT"
  log "built $IMAGE"
}

cmd_http() {
  need_env_file
  prepare_state
  container delete --force "$CONTAINER_NAME" >/dev/null 2>&1 || true

  log "starting $CONTAINER_NAME on $BIND_IP:$PORT (config: $CONFIG_HTTP)"
  container run --detach --rm \
    --name "$CONTAINER_NAME" \
    --env-file "$ENV_FILE" \
    --publish "$BIND_IP:$PORT:$PORT" \
    --volume "$CONFIG_HTTP:/etc/mcphe/config.yaml:ro" \
    --volume "$STATE_DIR:/var/lib/mcphe" \
    "$IMAGE" /etc/mcphe/config.yaml

  log "waiting for /rpc/openapi.json"
  for _ in $(seq 1 30); do
    if curl -fsS -o /dev/null "http://$BIND_IP:$PORT/rpc/openapi.json" 2>/dev/null; then
      log "up: http://$BIND_IP:$PORT/rpc"
      return 0
    fi
    sleep 1
  done
  log "did not answer in 30s — check: ./deploy/run.sh logs"
  return 1
}

# stdio: stdin/stdout belong to the MCP client. `-i` keeps stdin open, no `-t`
# (a pty would line-buffer and corrupt the JSON-RPC framing), and
# `--progress none` keeps the CLI's own output off the pipe.
cmd_stdio() {
  need_env_file
  prepare_state
  exec container run --interactive --rm \
    --progress none \
    --name "${CONTAINER_NAME}-stdio" \
    --env-file "$ENV_FILE" \
    --volume "$CONFIG_STDIO:/etc/mcphe/config.yaml:ro" \
    --volume "$STATE_DIR:/var/lib/mcphe" \
    "$IMAGE" /etc/mcphe/config.yaml
}

cmd_logs() {
  container logs --follow "$CONTAINER_NAME"
}

cmd_stop() {
  container stop "$CONTAINER_NAME" 2>/dev/null || true
  container delete --force "$CONTAINER_NAME" >/dev/null 2>&1 || true
  log "stopped $CONTAINER_NAME"
}

cmd_status() {
  container ls --all | sed -n '1p;/mcphe/p'
  printf '\nimage:\n'
  container image ls | sed -n '1p;/mcphe/p'
}

cmd_shell() {
  need_env_file
  container run --interactive --tty --rm \
    --env-file "$ENV_FILE" \
    --volume "$STATE_DIR:/var/lib/mcphe" \
    --entrypoint /bin/sh \
    "$IMAGE"
}

case "${1:-}" in
  build)  cmd_build ;;
  http|up) cmd_http ;;
  stdio)  cmd_stdio ;;
  logs)   cmd_logs ;;
  stop|down) cmd_stop ;;
  status) cmd_status ;;
  shell)  cmd_shell ;;
  *)
    sed -n '2,25p' "${BASH_SOURCE[0]}" | sed -e 's/^# \{0,1\}//'
    exit 1
    ;;
esac
