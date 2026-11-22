"""Run the whole mcphe test suite.

    cd <repo root>
    python3 -m tests

Or, from any working directory::

    python3 /path/to/repo/tests/__main__.py

Both are equivalent to ``python3 -m unittest discover -s tests -t .`` run from
the repo root.

The subject is the Go binary, so the run starts by building it once. Doing that
here rather than in the first test means a missing toolchain or a compile error
is reported as one clear message instead of eighty test failures.
"""

import sys
import unittest
from pathlib import Path

TESTS_DIR = Path(__file__).resolve().parent
REPO_ROOT = TESTS_DIR.parent

# Running this file directly puts tests/ on sys.path; discovery imports the
# modules as `tests.*`, so the repo root has to be importable instead.
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

MINIMUM_PYTHON = (3, 8)


def main():
    if sys.version_info < MINIMUM_PYTHON:
        print(
            f"error: this suite needs Python {MINIMUM_PYTHON[0]}.{MINIMUM_PYTHON[1]} "
            f"or newer, but {sys.executable} is {sys.version.split()[0]}",
            file=sys.stderr,
        )
        return 2

    # Import late: support.harness is what needs the repo root on sys.path.
    from tests.support import harness

    try:
        built = harness.binary()
    except RuntimeError as error:
        print(f"error: {error}", file=sys.stderr)
        return 2

    suite = unittest.TestLoader().discover(
        start_dir=str(TESTS_DIR),
        pattern="test_*.py",
        top_level_dir=str(REPO_ROOT),
    )
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print(f"\nbinary under test: {built}")
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    sys.exit(main())
