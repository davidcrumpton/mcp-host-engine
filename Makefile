# Variables
BINARY_NAME=mcp-server
VERSION ?= 0.0.10
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT)

.PHONY: all build run clean test help

all: build

## build: Build the binary with version and commit info
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) main.go

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