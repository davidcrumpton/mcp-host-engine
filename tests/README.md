# mcphe shell test suite

## Running tests

The `mcptest` runner selects positive (`+.sh`) and negative (`-.sh`) test scripts
from `command-scripts/`. Negative tests assert that invalid or disallowed requests
are rejected; they are successful tests when their assertions pass.

```bash
# Run the full suite against the default config (providers/default.yaml)
./tests/mcptest

# Run only positive or negative assertions
./tests/mcptest --type positive
./tests/mcptest --type negative

# Run both get_ip scripts, or only its positive script
./tests/mcptest --command get_ip
./tests/mcptest --command get_ip --type positive

# Multiple command names are space-separated within one argument
./tests/mcptest --command "get_ip http_request_get"

# Preview the selected tests without requiring a running server
./tests/mcptest --dry-run

# Stop after the first failed test script
./tests/mcptest --stop
```

Use `--provider <file>` to select a YAML file in `tests/providers/`, for example
`--provider default.yaml`. The configured server must be running for a real test
run. Timestamped logs are written to `tests/results/mcphe-YYYYMMDD-HHMMSS.log`.

## Test scripts

Shared HTTP/RPC and assertion helpers are in `funcs.sh`. Add provider configs to
`providers/`; the provider argument is the config filename. Every test script
must finish with `print_assert_summary`, so an assertion failure causes the
runner to report that script as failed.
