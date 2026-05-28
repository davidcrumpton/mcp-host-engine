#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-wikipedia_search-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== wikipedia_search- : negative tests ===${NC}"

assert_output_contains "wikipedia_search wrong input" "missing properties" \
  mcp_session_call "tools/call" "{\"name\":\"wikipedia_search\",\"arguments\":{\"quer\":\"OpenBSD\"}}"

print_assert_summary