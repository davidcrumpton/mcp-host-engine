#!/usr/bin/env bash
# =============================================================================
# funcs.sh — Shared helpers for mcphe test suite
# =============================================================================
# Source this file from test scripts. Do not execute directly.
# =============================================================================

SCRIPT_PATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
COMMAND_SCRIPTS_PATH="${SCRIPT_PATH}/command-scripts"

# ---- Colors ----
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ---- Default test config ----
# set from mcptest via OPT_PROVIDER if you want to use a different config

TEST_CONFIG_PATH="${SCRIPT_PATH}/providers"
DEFAULT_TEST="${TEST_CONFIG_PATH}/${OPT_PROVIDER}"

# print_safe_env
# prints environment variables that start with MCPHE or __MCPHE
# and masks the value for any key containing _API_KEY
print_safe_env() {
    for key in $(env | grep MCPHE | cut -d= -f1); do
        if [[ "$key" =~ ^__MCPHE.*$ ]]; then
            echo "       ${key}: [MASKED_VALUE]"
        elif [[ "$key" =~ .*_API_KEY.* ]]; then
            echo "       ${key}: [MASKED_VALUE]"
        else
            echo "       ${key}: ${!key}"
        fi
    done
}

# =============================================================================
# ASSERTION HELPERS
# Each assertion runs a command, checks exit code / output, and reports.
# =============================================================================

_ASSERT_PASS=0
_ASSERT_FAIL=0

# assert_succeeds LABEL CMD [ARGS...]
# Passes when the command exits 0.
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

# assert_fails LABEL CMD [ARGS...]
# Passes when the command exits non-zero (error expected).
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

# assert_output_contains LABEL PATTERN CMD [ARGS...]
# Passes when stdout/stderr contains PATTERN.
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

assert_output_matches_re() {
    local label="$1"; local pattern="$2"; shift 2
    local output
    output=$("$@" 2>&1)
    # Use BSD and GNU grep compatibility for -P (Perl regex)
    if echo "${output}" | grep -Eq "${pattern}"; then
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

assert_output_not_matches_re() {
    local label="$1"; local pattern="$2"; shift 2
    local output
    output=$("$@" 2>&1)
    if ! echo "${output}" | grep -Eq "${pattern}"; then
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
# assert_exit_code LABEL EXPECTED CMD [ARGS...]
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

# print_assert_summary — call at end of each test script
print_assert_summary() {
    local total=$((_ASSERT_PASS + _ASSERT_FAIL))
    echo ""
    echo -e "${BOLD}  Script summary: ${_ASSERT_PASS}/${total} passed${NC}"
    if [[ $_ASSERT_FAIL -gt 0 ]]; then
        echo -e "${RED}  ${_ASSERT_FAIL} assertion(s) failed${NC}"
        return 1
    fi
}


assert_contains_ip_address() {
    local label="$1"; 
    shift
    local output exit_code
    output=$("$@" 2>&1)
    exit_code=$?

    ip6_pattern=''
    for h in {1..7}; do ip6_pattern="${ip6_pattern}[0-9a-fA-F]{1,4}:"; done
    ip6_pattern="${ip6_pattern}[0-9a-fA-F]{1,4}"   # 8 groups, 7 colons

    ip4_pattern='[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}'

    if [[ $exit_code -eq 0 ]]; then
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
    else
        echo -e "${RED}  ✗ FAIL${NC}  ${label}  (exit ${exit_code})"
        echo "       CMD: $*"
        echo "       OUT: ${output}"
        print_safe_env
        _ASSERT_FAIL=$((_ASSERT_FAIL + 1))
    fi
}

# =============================================================================
# MCPHE COMMAND WRAPPERS
# =============================================================================

# Wrap cURL command to de-uglify calls to MCPHE

# skip_if_provider_unavailable — use at top of remote/lmstudio test scripts
skip_if_provider_unavailable() {
    if ! port_available; then
        echo -e "${YELLOW}  ⊘ SKIP${NC}  Provider '${MCPHE_PROVIDER}' at '${MCPHE_API_URL}' is not reachable — skipping script"
        exit 0
    fi
}



# ### Fetch Key from YAML file with $1 as filename and $2 as key
# ---
# version: "1.0"
# port: "7999"
# host: "0.0.0.0"
# use_https: false
#bearer_token: "ooghoh9ob4eenohtoF2miMeixayeefosh5UNgae9Oopahsh9sho5zoh0wadeFaoZ"

fetch_key_from_yaml() {
  local filename="$1"
  local key="$2"
  local value
  value=$(yq e "${key}" "$filename")
  echo "$value"
}

export BEARER="Bearer $(fetch_key_from_yaml "$DEFAULT_TEST" ".bearer_token")"
export PORT="$(fetch_key_from_yaml "$DEFAULT_TEST" ".port")"
export HOST="$(fetch_key_from_yaml "$DEFAULT_TEST" ".host")"
export USE_HTTPS="$(fetch_key_from_yaml "$DEFAULT_TEST" ".use_https")"

if [[ "$USE_HTTPS" == "true" ]]; then
    PROTOCOL="https"
else
    PROTOCOL="http"
fi

# =============================================================================
# PORT AVAILABILITY CHECK
# =============================================================================

# port_available
#
# Returns 0 if port 8000 is open and responding (TCP), 1 otherwise.
port_available() {
    local port=${1:-$PORT}
    if nc -z "$HOST" "$port" >/dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# JSON MCP command wrappers
mcp_request_json() {
    local method="$1"
    shift

    local params="{}"
    if [[ "$#" -gt 0 ]]; then
        params="$*"
    fi

    # Build JSON payload
    local payload
    payload="{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$method\",\"params\":$params}"

    # Make cURL call
    local response
    if [[ -n "$BEARER" ]]; then
        response=$(curl -s -X POST ${PROTOCOL}://${HOST}:${PORT}/rpc \
            -H "Content-Type: application/json" \
            -d "$payload" \
            -H "Authorization: $BEARER"
        )
    else
        response=$(curl -s -X POST ${PROTOCOL}://${HOST}:${PORT}/rpc \
            -H "Content-Type: application/json" \
            -d "$payload"
        )
    fi
    local exit_val=$?
    echo "$response"
    return $exit_val
}

