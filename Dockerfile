# syntax note: intentionally plain Dockerfile (no BuildKit frontend directive) so
# it also builds under Apple's `container build`, podman, and older builders.

# ---------------------------------------------------------------------------
# Build stage
# ---------------------------------------------------------------------------
ARG GO_VERSION=1.25
ARG ALPINE_VERSION=3.24

FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src

# Dependencies first: editing Go code then won't re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Injected by deploy/run.sh, compose.yaml, or `--build-arg`; defaults keep a
# bare `container build` working.
ARG VERSION=dev
ARG COMMIT=none

ENV CGO_ENABLED=0 \
    GOOS=linux

RUN go build \
      -trimpath \
      -ldflags "-s -w -X mcphe/config.Version=${VERSION} -X mcphe/config.Commit=${COMMIT}" \
      -o /out/mcphe .

# ---------------------------------------------------------------------------
# Runtime stage
# ---------------------------------------------------------------------------
FROM alpine:${ALPINE_VERSION}

# The process must not run as root (mcphe refuses to start unless
# run_as_root: true), and a UID that matches the host user makes bind-mounted
# state/plugins writable without chmod games. Override with --build-arg.
ARG UID=10001
ARG GID=10001

RUN set -eux; \
    apk add --no-cache ca-certificates tzdata; \
    # GID may already exist in Alpine (e.g. 20 = dialout on macOS hosts), so the
    # group/user creation is best-effort; USER below is numeric anyway.
    addgroup -g "${GID}" mcphe 2>/dev/null || addgroup mcphe; \
    adduser -D -H -u "${UID}" -G mcphe mcphe 2>/dev/null || true; \
    mkdir -p /app/plugins /etc/mcphe /var/lib/mcphe; \
    chown -R "${UID}:${GID}" /app /var/lib/mcphe

COPY --from=build /out/mcphe /usr/local/bin/mcphe

# Plugins are baked in so the image is self-contained and reproducible.
# Bind-mount over /app/plugins:ro when iterating on a plugin (see compose.yaml).
COPY plugins/ /app/plugins/

# Both modes ship as example configs, plus config.yaml as the default so a bare
# `container run <image>` does something sensible (HTTP on 0.0.0.0:8001).
COPY deploy/config.http.yaml deploy/config.stdio.yaml /etc/mcphe/
RUN cp /etc/mcphe/config.http.yaml /etc/mcphe/config.yaml \
 && chown -R "${UID}:${GID}" /app/plugins

USER ${UID}:${GID}
WORKDIR /app

ENV TZ=UTC

# Mode is a *runtime* choice, not a build-time one:
#   /etc/mcphe/config.yaml          -> default (copy of config.http.yaml)
#   /etc/mcphe/config.http.yaml     -> HTTP daemon on :8001
#   /etc/mcphe/config.stdio.yaml    -> MCP stdio server on stdin/stdout
# Mount your own config over /etc/mcphe/config.yaml to supply your plugin set.
ENTRYPOINT ["/usr/local/bin/mcphe"]
CMD ["/etc/mcphe/config.yaml"]
