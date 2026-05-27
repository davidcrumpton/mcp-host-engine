#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-get_env_var-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== get_env_var- : negative tests ===${NC}"


#OUT: {"jsonrpc":"2.0","result":{"content":[{"text":"","type":"text"}],"isError":false},"id":1}
assert_output_contains "get_env_var with not allowed env var" "GoError" \
  mcp_request_json "tools/call" "{\"name\":\"get_env_var\",\"arguments\":{\"env_var\":\"SHELL\"}}"

print_assert_summary