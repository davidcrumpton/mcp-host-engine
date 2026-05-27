#!/usr/bin/env bash


source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-read_file-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== read_file- : negative tests ===${NC}"

assert_output_matches_re "read_file with file in /etc/passwd" "error.*access to file.*not allowed" \
  mcp_request_json "tools/call" "{\"name\":\"read_file\",\"arguments\":{ \"path\":\"/etc/passwd\"}}"

assert_output_matches_re "read_file with file in ./../../../../.././etc/passwd" "error.*access to file.*not allowed" \
  mcp_request_json "tools/call" "{\"name\":\"read_file\",\"arguments\":{ \"path\":\"./../../../../.././etc/passwd\"}}"

assert_output_matches_re "read_file with file in tests/providers/default.yaml" "error.*access to file.*not allowed" \
  mcp_request_json "tools/call" "{\"name\":\"read_file\",\"arguments\":{ \"path\":\"tests/providers/default.yaml\"}}"

print_assert_summary