#!/usr/bin/env bash
# run-negative.sh — Run only negative tests
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
exec "${SCRIPT_DIR}/llmtest" --type negative "$@"
