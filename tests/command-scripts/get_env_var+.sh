#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

echo -e "\n${CYAN}=== get_env_var+ : positive tests ===${NC}"

assert_output_contains "get_env_var with PATH" "/bin" \
  mcp_session_call "tools/call" "{\"name\":\"get_env_var\",\"arguments\":{\"env_var\":\"PATH\"}}"

print_assert_summary