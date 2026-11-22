"""End-to-end tests for mcphe: the Go binary, run as an operator would run it.

Every case here drives the real ``mcphe`` binary -- over HTTP, over stdio, or
through its sub-commands -- against a throwaway config and a throwaway plugin
directory. Nothing is mocked and nothing is stubbed except the remote host a
plugin fetches from, which is a local HTTP server in ``support/upstream.py``.

The suite is split the way the rest of the repo splits tests: every case is
either *positive* (the documented behaviour happens) or *negative* (the failure
is reported, not swallowed). ``test_inventory.py`` is the coverage gate -- a
case listed in ``case_inventory.CASES`` without a test, or a test without a
case, fails the run.

Run it from the repository root::

    python3 -m tests

Nothing here needs a daemon, a container runtime, or network access. It does
need the Go toolchain, because the subject is the binary it builds; see
``README.md``.
"""

__all__ = ["case_inventory"]
