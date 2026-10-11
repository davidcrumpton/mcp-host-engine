#!/usr/bin/env bash
# run-command.sh — Run tests for a single command
# Usage: run-command.sh <command[+|-]>
# Examples:
#   run-command.sh get_ip+        # positive get_ip tests
#   run-command.sh get_ip-        # negative get_ip tests
#   run-command.sh get_ip         # both get_ip+ and get_ip-

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

if [[ $# -eq 0 ]]; then
    echo "Usage: $0 <command[+|-]>"
    echo "Examples:"
    echo "  $0 get_ip        # both positive and negative get_ip tests"
    echo "  $0 get_ip+       # positive get_ip tests only"
    echo "  $0 get_ip-       # negative get_ip tests only"
    exit 1
fi

ARG="$1"
shift

# Determine type filter
if [[ "${ARG}" == *+ ]]; then
    CMD="${ARG%+}"
    exec "${SCRIPT_DIR}/mcptest" --type positive --command "${CMD}" "$@"
elif [[ "${ARG}" == *- ]]; then
    CMD="${ARG%-}"
    exec "${SCRIPT_DIR}/mcptest" --type negative --command "${CMD}" "$@"
else
    exec "${SCRIPT_DIR}/mcptest" --type all --command "${ARG}" "$@"
fi
