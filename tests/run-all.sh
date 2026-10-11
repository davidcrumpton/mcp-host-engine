#!/usr/bin/env bash
# run-all.sh — Run the full test suite (positive + negative, default provider)
# Results are logged under tests/results/.
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
exec "${SCRIPT_DIR}/mcptest" --type all "$@"
