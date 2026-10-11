#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

echo -e "\n${CYAN}=== get_ip- : negative tests ===${NC}"

assert_output_contains "unknown get_ip alias is rejected" \
  "unknown tool" \
  mcp_session_call "tools/call" "{\"name\":\"ip.get_ip\",\"arguments\":{\"target\":\"[IP_ADDRESS]\"}}"

print_assert_summary
