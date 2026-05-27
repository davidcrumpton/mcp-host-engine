#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-write_file-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== write_file+ : positive tests ===${NC}"

TEST_FILE="$(mktemp /tmp/mcphe_test_XXXXXX)"

assert_output_contains "write_file with file in /tmp" "\"isError\":false" \
  mcp_request_json "tools/call" "{\"name\":\"write_file\",\"arguments\":{ \"path\":\"${TEST_FILE}\",\"content\":\"hello world\"}}"

# Try writing to the same file with different content
assert_output_contains "write_file with file in /tmp" "\"isError\":false" \
  mcp_request_json "tools/call" "{\"name\":\"write_file\",\"arguments\":{ \"path\":\"${TEST_FILE}\",\"content\":\"hello world new content\"}}"

# Clean up
rm "${TEST_FILE}"

print_assert_summary