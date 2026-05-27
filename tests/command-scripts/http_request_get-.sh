#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-http_request_get-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== http_request_get- : negative tests ===${NC}"

assert_output_matches_re "http_request_get with url https://www.eeeeexample.com" "GoError.*access.*www\.eeeeexample\.com.*not allowed" \
  mcp_request_json "tools/call" "{\"name\":\"http_request_get\",\"arguments\":{ \"url\" : \"https://www.eeeeexample.com\" }}"

print_assert_summary