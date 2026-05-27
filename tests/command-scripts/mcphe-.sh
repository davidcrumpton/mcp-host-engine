#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/funcs.sh"

SESSION_BASE="test-mcphe-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -c1-8)"

echo -e "\n${CYAN}=== mcphe- : negative tests ===${NC}"

assert_fails "config file not found: $SESSION_BASE" \
  ${SCRIPT_PATH}/../mcphe $SESSION_BASE

print_assert_summary