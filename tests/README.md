# mcphe test suite

![mcphe robot](./images/mcphe-192x192.png)

## What's new

### `mcptest` — the new central runner

```bash
# Run all tests for all providers
./mcptest --type all --provider any

# Quick focused tests
./mcptest --type positive              # only passing-expected tests
./mcptest --type negative              # only failure-expected tests
./mcptest --command "get_ip http_request_get"      # get_ip+ and get_ip- only
./mcptest --command "readFile"           # readFile+ and readFile- only
./mcptest --command "get_ip" --type positive
./mcptest --dry-run                    # see what would run without executing
./mcptest --stop                       # bail on first failure

```

Results are timestamped and logged to `tests/scripts/results/llmtest-YYYYMMDD-HHMMSS.log` automatically, so you can check them in the morning.

### `funcs.sh` — big upgrade

Three new things it provides:

**Assertion helpers** — every test now uses `assert_succeeds`, `assert_fails`, `assert_output_contains`, or `assert_exit_code`. Each prints a clear ✓/✗ line and counts pass/fail. `print_assert_summary` at the end of each script gives a per-script tally and exits non-zero if anything failed, so `llmtest` can detect it.

**`skip_if_provider_unavailable`** — provider-specific scripts call this at the top. If the remote server isn't running or the remote server is down, the script exits 0 with a clear SKIP message instead of failing the whole suite.

### Provider-specific test scripts

The providers folder contains `default.yaml` and it is the default.  Add other mcp server configurations and use the --provider argument to select the provider.  The provider name must match the filename of the provider configuration file.

## Caveats

This was designed and tested only on LMStudio at this time.
