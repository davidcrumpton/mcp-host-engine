#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

echo -e "\n${CYAN}=== get_ip+ : positive tests ===${NC}"

assert_contains_ip_address "get_ip with no arguments"  \
  mcp_session_call "tools/call" "{\"name\":\"get_ip\"}"

print_assert_summary