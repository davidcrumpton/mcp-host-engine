#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-http_request_get-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== http_request_get+ : positive tests ===${NC}"

assert_output_contains "http_request_get with url https://www.example.com" "Example Domain"\
  mcp_request_json "tools/call" "{\"name\":\"http_request_get\",\"arguments\":{ \"url\" : \"https://www.example.com\" }}"

print_assert_summary