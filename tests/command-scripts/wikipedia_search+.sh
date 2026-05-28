#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-wikipedia_search-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== wikipedia_search+ : positive tests ===${NC}"

assert_output_contains "wikipedia_search with topic OpenBSD" OpenBSD\
  mcp_session_call "tools/call" "{\"name\":\"wikipedia_search\",\"arguments\":{\"query\":\"OpenBSD\"}}"

print_assert_summary
