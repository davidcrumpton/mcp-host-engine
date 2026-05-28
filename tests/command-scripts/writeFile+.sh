#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-write_file-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== write_file+ : positive tests ===${NC}"

TEST_FILE="$(mktemp /tmp/mcphe_test_XXXXXX)"

# Usage: assert_file_contains <label> <pattern> <file_path>
assert_file_contains "write_file with text in /tmp" "hello world" "${TEST_FILE}" \
  mcp_session_call "tools/call" "{\"name\":\"write_file\",\"arguments\":{ \"path\":\"${TEST_FILE}\",\"content\":\"hello world\"}}"

# Try writing to the same file with different content
assert_file_contains "write_file with file in /tmp" "hello world new content" "${TEST_FILE}" \
  mcp_session_call "tools/call" "{\"name\":\"write_file\",\"arguments\":{ \"path\":\"${TEST_FILE}\",\"content\":\"hello world new content\"}}"

cat ${TEST_FILE}
# Clean up
rm "${TEST_FILE}"

print_assert_summary