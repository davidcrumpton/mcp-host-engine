#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

echo -e "\n${CYAN}=== http_request_get+ : positive tests ===${NC}"

assert_output_contains "http_request_get with url https://www.example.com" "Example Domain"\
  mcp_session_call "tools/call" "{\"name\":\"http_request_get\",\"arguments\":{ \"url\" : \"https://www.example.com\" }}"

print_assert_summary