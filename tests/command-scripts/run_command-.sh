#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

echo -e "\n${CYAN}=== run_command- : negative tests ===${NC}"

assert_output_contains 'run_command su' 'not allowed' \
  mcp_session_call "tools/call" '{"name":"run_command","arguments":{"command":"su"}}'

print_assert_summary