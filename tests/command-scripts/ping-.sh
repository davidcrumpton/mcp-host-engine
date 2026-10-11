#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

echo -e "\n${CYAN}=== ping- : negative tests ===${NC}"

assert_output_contains "disabled ping tool is unavailable" \
  "unknown tool" \
  mcp_session_call "tools/call" "{\"name\":\"ping\",\"arguments\":{\"target\":\"[IP_ADDRESS]\"}}"

print_assert_summary
