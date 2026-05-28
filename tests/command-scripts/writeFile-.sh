#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-write_file-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== write_file- : negative tests ===${NC}"

assert_output_matches_re "write_file with file in /var/tmp/" "writing to file.* not allowed" \
  mcp_session_call "tools/call" "{\"name\":\"write_file\",\"arguments\":{ \"path\":\"/var/tmp/mcphe_test.txt\",\"content\":\"hello world\"}}"

print_assert_summary
