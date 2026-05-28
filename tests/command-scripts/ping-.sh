#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-ping-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== ping- : negative tests ===${NC}"

assert_output_contains "ping with wrong target name" \
  "unknown tool" \
  mcp_session_call "tools/call" "{\"name\":\"ping\",\"arguments\":{\"target\":\"[IP_ADDRESS]\"}}"

print_assert_summary
