#!/usr/bin/env bash
# run-command.sh — Run tests for a single command
# Usage: run-command.sh <command[+|-]>
# Examples:
#   run-command.sh chat+          # positive chat tests
#   run-command.sh chat-          # negative chat tests
#   run-command.sh chat           # both chat+ and chat-
#   run-command.sh chat-lmstudio+ # LMStudio chat tests

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

if [[ $# -eq 0 ]]; then
    echo "Usage: $0 <command[+|-]>"
    echo "Examples:"
    echo "  $0 chat          # both positive and negative chat tests"
    echo "  $0 chat+         # positive chat tests only"
    echo "  $0 chat-         # negative chat tests only"
    echo "  $0 version       # version tests"
    exit 1
fi

ARG="$1"
shift

# Determine type filter
if [[ "${ARG}" == *+ ]]; then
    CMD="${ARG%+}"
    exec "${SCRIPT_DIR}/llmtest" --type positive --command "${CMD}" "$@"
elif [[ "${ARG}" == *- ]]; then
    CMD="${ARG%-}"
    exec "${SCRIPT_DIR}/llmtest" --type negative --command "${CMD}" "$@"
else
    exec "${SCRIPT_DIR}/llmtest" --type all --command "${ARG}" "$@"
fi
