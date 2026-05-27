#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-get_ip-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== get_ip- : negative tests ===${NC}"

assert_output_contains "get_ip with wrong target name" \
  "not found" \
  mcp_request_json "tools/call" "{\"name\":\"ip.get_ip\",\"arguments\":{\"target\":\"[IP_ADDRESS]\"}}"

print_assert_summary
