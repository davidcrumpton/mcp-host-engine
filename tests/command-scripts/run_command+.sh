#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-run_command-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== run_command+ : positive tests ===${NC}"

assert_output_contains "run_command env" 'USER' \
  mcp_session_call "tools/call" '{"name":"run_command","arguments":{"command":"env"}}'

print_assert_summary