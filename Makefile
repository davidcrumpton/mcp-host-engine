# Variables
BINARY_NAME=mcphe

.PHONY: all build run clean test help image up down logs stdio env release snapshot check-inputs

all: build

## build: Build the binary with version and commit info
build:
	VERSION=$$(cat VERSION); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo none); \
	go build -ldflags "-X mcphe/config.Version=$$VERSION -X mcphe/config.Commit=$$COMMIT"

## run: Build and run the server
run: build
	./$(BINARY_NAME)

## clean: Remove the binary
clean:
	rm -f $(BINARY_NAME)

## test: Run Go tests
test:
	go test ./...

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## ";} {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

## release: Build and publish release packages (CI only — needs a token)
release:
	goreleaser release --clean

## snapshot: Build every artifact locally, publishing nothing
## (docker/sbom/sign are skipped: they need a registry, syft and an OIDC identity)
snapshot:
	MCPHE_RELEASE_DRY_RUN=1 goreleaser release --snapshot --clean --skip=publish,sbom,sign,docker

## check-inputs: Verify nothing gitignored under plugins/ would be packaged
check-inputs:
	./scripts/check-release-inputs.sh

# ---------------------------------------------------------------------------
# Containers. One image, two modes (see deploy/README.md). These wrap
# deploy/run.sh, which uses Apple's `container` CLI; with docker or podman
# installed, use `docker compose up -d --build` / `docker compose run --rm -T
# mcphe-stdio` instead — same files, same variables.
# ---------------------------------------------------------------------------

## image: Build the container image
image:
	./deploy/run.sh build

## env: Create deploy/.env from deploy/.env.example (never overwrites)
env:
	@test -f deploy/.env || cp deploy/.env.example deploy/.env
	@echo "deploy/.env ready — add your plugin credentials"

## up: Start the HTTP daemon container (detached)
up:
	./deploy/run.sh http

## down: Stop and remove the daemon container
down:
	./deploy/run.sh stop

## logs: Follow daemon container logs
logs:
	./deploy/run.sh logs

## stdio: Run stdio mode in a container (writes protocol to this terminal)
stdio:
	./deploy/run.sh stdio


## Build TypeScript plugins and run tests
plugin-tests:
	cd plugin-devel && \
	npm run test


## Build TypeScript plugins to dist/ only
plugin-build:
	cd plugin-devel && \
	npm run build

## Build TypeScript plugins and Deploy
plugin-deploy:
	cd plugin-devel && \
	npm run build && \
	npm run deploy
	

