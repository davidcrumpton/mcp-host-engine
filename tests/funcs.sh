#!/usr/bin/env bash
# ======================================================================================
# funcs.sh — Shared helpers for mcphe test suite
# ======================================================================================
# Description:
# This file provides shared utility functions, assertion helpers, and API wrappers
# for the MCPHE test suite.
#
# WARNING: Source this file from test scripts. DO NOT execute directly.
# ======================================================================================

# -------------------------------------------------------------------------------------
# 1. GLOBAL CONSTANTS AND PATHS
# -------------------------------------------------------------------------------------

# Determine script location paths
SCRIPT_PATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
COMMAND_SCRIPTS_PATH="${SCRIPT_PATH}/command-scripts"

# Color definitions for terminal output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# Default test configuration paths
TEST_CONFIG_PATH="${SCRIPT_PATH}/providers"
# Default provider name is expected to be set via OPT_PROVIDER environment variable
DEFAULT_TEST="${TEST_CONFIG_PATH}/${OPT_PROVIDER}"

# -------------------------------------------------------------------------------------
# 2. UTILITY AND ENVIRONMENT FUNCTIONS
# -------------------------------------------------------------------------------------

# print_safe_env
# Prints environment variables starting with MCPHE or __MCPHE, masking values
# if the key contains _API_KEY or if the key starts with __MCPHE.
print_safe_env() {
    for key in $(env | grep MCPHE | cut -d= -f1); do
        local value
        # Check for masking conditions
        if [[ "$key" =~ ^__MCPHE.*$ ]] || [[ "$key" =~ .*_API_KEY.* ]]; then
            value="[MASKED_VALUE]"
        else
            # Use indirect expansion to get the value of the variable
            if declare -p "$key" > /dev/null 2>&1; then
                value="${!key}"
            else
                value="(N/A - Variable not set)"
            fi
        fi
        echo "       ${key}: ${value}"
    done
}

# -------------------------------------------------------------------------------------
# 3. CONFIGURATION SETUP
# -------------------------------------------------------------------------------------

# fetch_key_from_yaml
# Extracts a specified key value from a YAML file using yq.
# Usage: fetch_key_from_yaml <filename> <key>
fetch_key_from_yaml() {
  local filename="$1"
  local key="$2"
  local value
  # Use yq to safely extract the value
  value=$(yq e "${key}" "$filename")
  echo "$value"
}

# Export required variables by reading the default test configuration file
export BEARER="Bearer $(fetch_key_from_yaml "$DEFAULT_TEST" ".bearer_token")"
export PORT="$(fetch_key_from_yaml "$DEFAULT_TEST" ".port")"
export HOST="$(fetch_key_from_yaml "$DEFAULT_TEST" ".host")"
export USE_HTTPS="$(fetch_key_from_yaml "$DEFAULT_TEST" ".use_https")"

# Determine protocol based on the configuration
if [[ "$USE_HTTPS" == "true" ]]; then
    PROTOCOL="https"
else
    PROTOCOL="http"
fi

# -------------------------------------------------------------------------------------
# 4. NETWORK AND API COMMUNICATION
# -------------------------------------------------------------------------------------

# port_available
# Checks if the configured host and port are reachable using netcat (nc -z).
# Returns 0 if the port is open and responding, 1 otherwise.
# Usage: port_available [port_number]
port_available() {
    local port=${1:-$PORT}
    if command -v nc >/dev/null 2>&1 && nc -z "$HOST" "$port" >/dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# mcp_session_call
# Constructs and sends a JSON-RPC request to the MCPHE endpoint.
# Usage: mcp_session_call <method> [params...]
# Returns the JSON response body.
mcp_session_call() {
    local method="$1"
    shift

    local params="{}"
    if [[ "$#" -gt 0 ]]; then
        # Concatenate remaining arguments into the JSON params object
        params="$*"
    fi

    # Build the JSON payload for the RPC call
    local payload
    payload="{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$method\",\"params\":$params}"

    # Construct the full URL
    local rpc_url="${PROTOCOL}://${HOST}:${PORT}/rpc"

    # Make cURL call, handling authorization header conditionally
    if [[ -n "$BEARER" ]]; then
        local response
        response=$(curl -s -X POST "$rpc_url" \
            -H "Content-Type: application/json" \
            -d "$payload" \
            -H "Authorization: $BEARER"
        )
    else
        local response
        response=$(curl -s -X POST "$rpc_url" \
            -H "Content-Type: application/json" \
            -d "$payload"
        )
    fi
    local exit_val=$?
    echo "$response"
    return $exit_val
}

# mcp_session_call
# Performs a full MCP session: initialize → initialized → your method → returns your method's response JSON
# Usage: mcp_session_call <method> [params]
mcp_session_call() {
    local method="$1"
    shift
    local params="{}"
    if [[ "$#" -gt 0 ]]; then
        params="$*"
    fi

    local rpc_url="${PROTOCOL}://${HOST}:${PORT}/rpc"
    local auth_header=()
    if [[ -n "$BEARER" ]]; then
        auth_header=(-H "Authorization: $BEARER")
    fi

    # --- 1. initialize (get session ID) ---
    local init_payload='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test-client","version":"0.1"}}}'

    local init_raw
    init_raw=$(curl -s -D - -X POST "$rpc_url" \
        -H "Content-Type: application/json" \
        -H "Accept: application/json, text/event-stream" \
        "${auth_header[@]}" \
        -d "$init_payload")

    # Extract session ID from response headers
    local session_id
    session_id=$(echo "$init_raw" | grep -i '^mcp-session-id:' | tr -d '\r' | awk '{print $2}')

    if [[ -z "$session_id" ]]; then
        echo '{"error":"no session ID returned from initialize"}' >&2
        return 1
    fi

    # --- 2. initialized notification (no response expected) ---
    local notif_payload='{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
    curl -s -X POST "$rpc_url" \
        -H "Content-Type: application/json" \
        -H "Accept: application/json, text/event-stream" \
        -H "Mcp-Session-Id: $session_id" \
        "${auth_header[@]}" \
        -d "$notif_payload" > /dev/null

    # --- 3. Actual call ---
    local payload
    payload="{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"$method\",\"params\":$params}"

    local raw
    raw=$(curl -s -X POST "$rpc_url" \
        -H "Content-Type: application/json" \
        -H "Accept: application/json, text/event-stream" \
        -H "Mcp-Session-Id: $session_id" \
        "${auth_header[@]}" \
        -d "$payload")

    # Strip SSE envelope → extract data: line
    local response
    response=$(echo "$raw" | grep '^data:' | sed 's/^data: //')
    if [[ -z "$response" ]]; then
        response="$raw"
    fi

    echo "$response"
}

# -------------------------------------------------------------------------------------
# 5. ASSERTION AND TESTING FRAMEWORK
# -------------------------------------------------------------------------------------

# Global counters for assertion summary
_ASSERT_PASS=0
_ASSERT_FAIL=0

# assert_succeeds
# Asserts that a command succeeds (exits with code 0).
# Usage: assert_succeeds <label> <command> [args...]
assert_succeeds() {
    local label="$1"; shift
    local output exit_code
    output=$("$@" 2>&1)
    exit_code=$?
    if [[ $exit_code -eq 0 ]]; then
        echo -e "${GREEN}  ✓ PASS${NC}  ${label}"
        _ASSERT_PASS=$((_ASSERT_PASS + 1))
    else
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (exit ${exit_code})"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
    fi
}

# assert_fails
# Asserts that a command fails (exits with non-zero code).
# Usage: assert_fails <label> <command> [args...]
assert_fails() {
    local label="$1"; shift
    local output exit_code
    output="$("$@" 2>&1)"
    exit_code=$?
    if [[ $exit_code -ne 0 ]]; then
        echo -e "${GREEN}  ✓ PASS${NC}  ${label}  (correctly errored, exit ${exit_code})"
        _ASSERT_PASS=$((_ASSERT_PASS + 1))
    else
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (should have failed but exited 0)"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
    fi
}

# assert_output_contains
# Asserts that the command output contains a specific literal pattern.
# Usage: assert_output_contains <label> <pattern> <command> [args...]
assert_output_contains() {
    local label="$1"; local pattern="$2"; shift 2
    local output
    output=$("$@" 2>&1)
    if echo "${output}" | grep -q "${pattern}"; then
        echo -e "${GREEN}  ✓ PASS${NC}  ${label}"
        _ASSERT_PASS=$((_ASSERT_PASS + 1))
    else
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (pattern '${pattern}' not found)"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
    fi
}

# assert_output_matches_re
# Asserts that the command output matches a specific extended regex pattern.
# Usage: assert_output_matches_re <label> <regex_pattern> <command> [args...]
assert_output_matches_re() {
    local label="$1"; local pattern="$2"; shift 2
    local output
    output=$("$@" 2>&1)
    # Use grep -E for extended regex matching
    if echo "${output}" | grep -E "${pattern}"; then
        echo -e "${GREEN}  ✓ PASS${NC}  ${label}"
        _ASSERT_PASS=$((_ASSERT_PASS + 1))
    else
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (regex pattern '${pattern}' not found)"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
    fi
}

# assert_output_not_matches_re
# Asserts that the command output does NOT match a specific extended regex pattern.
# Usage: assert_output_not_matches_re <label> <regex_pattern> <command> [args...]
assert_output_not_matches_re() {
    local label="$1"; local pattern="$2"; shift 2
    local output
    output=$("$@" 2>&1)
    # Use grep -qE for quiet extended regex matching; exit status 1 means no match
    if ! echo "${output}" | grep -qE "${pattern}"; then
        echo -e "${GREEN}  ✓ PASS${NC}  ${label}"
        _ASSERT_PASS=$((_ASSERT_PASS + 1))
    else
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (regex pattern '${pattern}' found)"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
    fi
}

# assert_exit_code
# Asserts that a command exits with an expected specific exit code.
# Usage: assert_exit_code <label> <expected_code> <command> [args...]
assert_exit_code() {
    local label="$1"; local expected="$2"; shift 2
    local output exit_code
    output=$("$@" 2>&1)
    exit_code=$?
    if [[ $exit_code -eq $expected ]]; then
        echo -e "${GREEN}  ✓ PASS${NC}  ${label}  (exit ${exit_code})"
        _ASSERT_PASS=$((_ASSERT_PASS + 1))
    else
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (expected exit ${expected}, got ${exit_code})"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
    fi
}

# assert_contains_ip_address
# Asserts that the command output contains at least one IPv4 or IPv6 address.
# Usage: assert_contains_ip_address <label> <command> [args...]
assert_contains_ip_address() {
    local label="$1"; 
    shift
    local output exit_code
    output=$("$@" 2>&1)
    exit_code=$?

    # Regex pattern for IPv6 (8 groups, 7 colons, allowing for compression)
    # This simplified pattern covers standard full/compressed IPv6.
    ip6_pattern='([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|::(ffff(:0{1,4})?|([0-9]{1,3}\.){3}[0-9]{1,3})'
    # Regex pattern for IPv4
    ip4_pattern='[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}'

    if [[ $exit_code -ne 0 ]]; then
        # Fail if the command itself failed
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (command failed, exit ${exit_code})"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
        return 1
    fi

    if echo "${output}" | grep -Eq "${ip6_pattern}" || echo "${output}" | grep -Eq "${ip4_pattern}"; then
        echo -e "${GREEN}  ✓ PASS${NC}  ${label}"
        _ASSERT_PASS=$((_ASSERT_PASS + 1))
    else
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (no IPv4 or IPv6 address found)"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
    fi
}

# assert_file_contains
# Asserts that a file contains a specific literal pattern.
# Usage: assert_file_contains <label> <pattern> <file_path> <command>
# File is created after command runs
#
assert_file_contains() {
    local label="$1"; local pattern="$2"; local file_path="$3"
    shift 3
    local output exit_code
    output=$("$@" 2>&1)
    exit_code=$?

    if [[ $exit_code -ne 0 ]]; then
        # Fail if the command itself failed
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (command failed, exit ${exit_code})"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
        return 1
    fi
    
    if [[ ! -f "$file_path" ]]; then
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (file '${file_path}' does not exist)"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
        return 1
    fi
    if grep -q "${pattern}" "$file_path"; then
        echo -e "${GREEN}  ✓ PASS${NC}  ${label}"
        _ASSERT_PASS=$((_ASSERT_PASS + 1))
    else
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (pattern '${pattern}' not found in file)"
        echo "       FILE: ${file_path}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
    fi
}

# print_assert_summary
# Summary function to be called at the end of a test script.
# Returns 1 if any assertion failed.
print_assert_summary() {
    local total=$((_ASSERT_PASS + _ASSERT_FAIL))
    echo ""
    echo -e "${BOLD}  ==============================================================${NC}"
    echo -e "${BOLD}  Script summary: ${_ASSERT_PASS}/${total} tests passed${NC}"
    if [[ $_ASSERT_FAIL -gt 0 ]]; then
        echo -e "${RED}  ${_ASSERT_FAIL} assertion(s) failed${NC}"
        return 1
    else
        echo -e "${GREEN}  All tests passed successfully.${NC}"
        return 0
    fi
}

# skip_if_provider_unavailable
# Checks if the API endpoint is reachable and reports a skip if not.
# This function should be called at the start of a test suite.
skip_if_provider_unavailable() {
    if ! port_available; then
        echo -e "${YELLOW}  ⊘ SKIP${NC}  Provider '${MCPHE_PROVIDER}' at '${MCPHE_API_URL}' is not reachable — skipping script"
        return 0 # Exit status 0 to signal graceful skip
    fi
    return 1 # API is available, continue execution
}

# -------------------------------------------------------------------------------------
# Example Execution Block (Optional)
# -------------------------------------------------------------------------------------
# To ensure the function is ready for external use, no execution logic is included here.
