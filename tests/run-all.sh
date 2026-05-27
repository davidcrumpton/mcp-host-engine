#!/usr/bin/env bash
# run-all.sh — Run the full test suite (positive + negative, all providers)
# This is the "run before bed" script. Results are logged to tests/scripts/results/
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
exec "${SCRIPT_DIR}/llmtest" --type all --provider any "$@"
