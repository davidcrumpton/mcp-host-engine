"""Support code for the mcphe test suite.

``harness`` builds and runs the binary, ``mcp_client`` speaks JSON-RPC to it,
``fixtures`` supplies the JavaScript plugins under test and ``upstream`` is the
local stand-in for a remote host. Nothing here asserts anything: the test
modules own the expectations.
"""
