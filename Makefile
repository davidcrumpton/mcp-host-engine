# Variables
BINARY_NAME=mcphe

.PHONY: all build run clean test help

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

## release: Build and publish release packages
release:
	goreleaser release --clean

## Build TypeScript plugins and Deploy
plugin-deploy:
	cd plugin-devel && \
	npm run build && \
	npm run deploy
	

plugin-tests:
	cd plugin-devel && \
	npm run test
