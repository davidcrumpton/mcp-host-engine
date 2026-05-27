#!/usr/bin/env bash
# run-positive.sh — Run only positive tests
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
exec "${SCRIPT_DIR}/llmtest" --type positive "$@"
