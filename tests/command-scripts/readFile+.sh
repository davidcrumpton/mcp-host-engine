#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-write_file-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== read_file+ : positive tests ===${NC}"

TEST_FILE="$(mktemp /tmp/mcphe_test_XXXXXX)"

echo "Hello from read_file" > "${TEST_FILE}"

assert_output_contains "read_file with file in /tmp" "Hello from read_file" \
  mcp_session_call "tools/call" "{\"name\":\"read_file\",\"arguments\":{ \"path\":\"${TEST_FILE}\",\"content\":\"hello world\"}}"

# Clean up
rm "${TEST_FILE}"

print_assert_summary