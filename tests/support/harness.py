"""Shared helpers for the mcphe end-to-end suite.

Everything the tests need in order to run the real binary lives here, so the
test modules stay about behaviour rather than plumbing:

* ``binary()`` builds ``mcphe`` once per run (or reuses ``MCPHE_TEST_BINARY``);
* ``write_config()`` / ``install_plugins()`` build a throwaway config + plugin
  directory for one test;
* ``Server`` starts the binary as a child process and waits until it is
  listening;
* ``run_cli()`` runs the binary to completion, for the sub-commands and for the
  startup failures that never get as far as listening.

The suite has no third-party dependencies on purpose: ``mcphe`` is a Go binary,
so the only thing Python has to provide is a subprocess, a socket and ``json``.
That keeps ``python3 -m tests`` working on a bare interpreter.
"""

import atexit
import os
import shutil
import socket
import subprocess
import tempfile
import time
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]

# Reuse a build if the caller already made one (CI, or a developer iterating
# without re-running the suite). Otherwise the binary is built once per process.
BINARY_ENV = "MCPHE_TEST_BINARY"

_BINARY = None

# Any MCPHE_* variable in the developer's shell could change what a plugin can
# see (host.process.env is allow-listed per plugin) or where the server looks
# for things, so the suite always starts from a clean slate and adds back only
# the variables a test asks for.
_MCPHE_ENV_PREFIX = "MCPHE_"


# ---------------------------------------------------------------------------
# The binary under test
# ---------------------------------------------------------------------------


def _go_available():
    return shutil.which("go") is not None


def binary():
    """Return the path to an ``mcphe`` binary, building it if necessary.

    Building once per run keeps the whole suite at a couple of seconds of
    overhead; building per test would dominate it.
    """
    global _BINARY
    if _BINARY is not None:
        return _BINARY

    override = os.environ.get(BINARY_ENV)
    if override:
        path = Path(override).expanduser().resolve()
        if not path.is_file():
            raise RuntimeError(f"{BINARY_ENV}={override} does not point at a file")
        _BINARY = path
        return _BINARY

    if not _go_available():
        raise RuntimeError(
            "the mcphe test suite needs the Go toolchain to build the binary, but "
            "'go' is not on PATH. Install Go, or set "
            f"{BINARY_ENV}=/path/to/an/existing/mcphe build"
        )

    target = Path(tempfile.mkdtemp(prefix="mcphe-bin-")) / "mcphe"
    completed = subprocess.run(
        ["go", "build", "-trimpath", "-o", str(target), "."],
        cwd=str(REPO_ROOT),
        capture_output=True,
        text=True,
        timeout=600,
    )
    if completed.returncode != 0:
        raise RuntimeError(
            "go build failed, so the suite cannot run:\n"
            f"--- stdout ---\n{completed.stdout}\n--- stderr ---\n{completed.stderr}"
        )
    # The build directory is a per-run temporary, so it goes away with the run.
    atexit.register(shutil.rmtree, str(target.parent), True)
    _BINARY = target
    return _BINARY


# ---------------------------------------------------------------------------
# Environment and process plumbing
# ---------------------------------------------------------------------------


def clean_env(**extra):
    """Return os.environ without MCPHE_* variables, plus ``extra``."""
    env = {
        key: value
        for key, value in os.environ.items()
        if not key.startswith(_MCPHE_ENV_PREFIX)
    }
    env["PYTHONUNBUFFERED"] = "1"
    for key, value in extra.items():
        env[key] = str(value)
    return env


def free_port():
    """Return a port that nothing is listening on right now."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def port_is_open(port, host="127.0.0.1", timeout=0.2):
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.settimeout(timeout)
        return sock.connect_ex((host, int(port))) == 0


def run_cli(*args, **kwargs):
    """Run the binary to completion and return the CompletedProcess.

    Arguments are passed through verbatim, so flags come before positional
    arguments exactly as the CLI requires: ``run_cli("-config-checksum", sum,
    config_path)``.
    """
    cwd = kwargs.pop("cwd", None)
    extra_env = kwargs.pop("env", None) or {}
    timeout = kwargs.pop("timeout", 120)
    stdin = kwargs.pop("stdin", None)
    if kwargs:
        raise TypeError(f"unexpected keyword arguments: {sorted(kwargs)}")

    return subprocess.run(
        [str(binary())] + [str(a) for a in args],
        cwd=None if cwd is None else str(cwd),
        env=clean_env(**extra_env),
        input=stdin,
        capture_output=True,
        text=True,
        timeout=timeout,
    )


def output_of(result):
    """Combined stdout+stderr, for assertion messages that need context."""
    return f"--- stdout ---\n{result.stdout}\n--- stderr ---\n{result.stderr}"


def temp_dir(testcase, prefix="mcphe-test-"):
    """Create a temporary directory that is removed when the test finishes."""
    path = Path(tempfile.mkdtemp(prefix=prefix))
    testcase.addCleanup(shutil.rmtree, str(path), True)
    return path


# ---------------------------------------------------------------------------
# Config files
# ---------------------------------------------------------------------------

# Everything a serve config needs that a test rarely cares about. verbosity 0
# keeps the server quiet; a test that asserts on logs turns it up itself.
DEFAULT_CONFIG = {
    "transport": "http",
    "host": "127.0.0.1",
    "verbosity_level": 0,
}


def _yaml_scalar(value):
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return repr(value)
    if value is None:
        return "null"
    text = str(value)
    return '"' + text.replace("\\", "\\\\").replace('"', '\\"') + '"'


def _yaml_lines(data, indent):
    """Render the subset of YAML an mcphe config needs: mappings and lists."""
    lines = []
    pad = "  " * indent
    for key, value in data.items():
        if value is None:
            # None means "leave the key out", which is how a config expresses
            # "use the built-in default" for every field.
            continue
        if isinstance(value, dict):
            if not value:
                lines.append(f"{pad}{key}: {{}}")
                continue
            lines.append(f"{pad}{key}:")
            lines.extend(_yaml_lines(value, indent + 1))
        elif isinstance(value, (list, tuple)):
            if not value:
                lines.append(f"{pad}{key}: []")
                continue
            lines.append(f"{pad}{key}:")
            for item in value:
                if isinstance(item, dict):
                    # Only needed for a list of mappings, which no config here
                    # uses; keep it explicit rather than silently wrong.
                    raise TypeError("list of mappings is not supported")
                lines.append(f"{pad}  - {_yaml_scalar(item)}")
        else:
            lines.append(f"{pad}{key}: {_yaml_scalar(value)}")
    return lines


class Config:
    """A written config file, plus the values it was written with."""

    def __init__(self, path, data):
        self.path = path
        self.data = data

    @property
    def port(self):
        port = self.data.get("port")
        return "" if port is None else str(port)

    def text(self):
        return self.path.read_text()

    def rewrite(self, **fields):
        """Merge ``fields`` into the config and write it back."""
        merged = dict(self.data)
        merged.update(fields)
        self.path.write_text(render_config(merged))
        self.data = merged
        return self

    def __str__(self):
        return str(self.path)


def render_config(data):
    return "\n".join(_yaml_lines(data, 0)) + "\n"


def write_config(directory, **fields):
    """Write a config file into ``directory`` and return it.

    A ``port`` is allocated unless the caller supplies one, because a config
    without a port would make the server listen on the built-in 8001 and the
    suite would fight the developer's own daemon.
    """
    data = dict(DEFAULT_CONFIG)
    data["port"] = str(free_port())
    data.setdefault("plugin_dir", str(Path(directory) / "plugins"))
    data.update(fields)
    path = Path(directory) / "config.yaml"
    path.write_text(render_config(data))
    return Config(path, data)


def install_plugins(directory, *names):
    """Write the named fixture plugins into ``<directory>/plugins``.

    Returns the plugin directory, so a test can add, replace or tamper with a
    file before the server starts.
    """
    from tests.support import fixtures

    plugin_dir = Path(directory) / "plugins"
    plugin_dir.mkdir(parents=True, exist_ok=True)
    fixtures.install(plugin_dir, *names)
    return plugin_dir


# ---------------------------------------------------------------------------
# The server process
# ---------------------------------------------------------------------------


class Server:
    """The real binary, serving HTTP, for the duration of one test.

    Use it as a context manager or call ``start()``/``stop()``. ``start()``
    waits until the port is listening and raises with the process output if the
    server died instead -- so a positive test fails with the server's own error
    message rather than a connection refused three steps later.
    """

    def __init__(self, config, *, cwd=None, env=None, args=(), startup_timeout=20.0):
        self.config = config
        self.cwd = None if cwd is None else str(cwd)
        self.env = env or {}
        self.args = tuple(str(a) for a in args)
        self.startup_timeout = startup_timeout
        self._process = None
        self._stdout = None
        self._stderr = None
        self._stdout_path = None
        self._stderr_path = None

    # -- lifecycle ---------------------------------------------------------

    def start(self):
        if self._process is not None:
            raise RuntimeError("server already started")
        log_dir = Path(tempfile.mkdtemp(prefix="mcphe-log-"))
        self._stdout_path = log_dir / "stdout.log"
        self._stderr_path = log_dir / "stderr.log"
        self._stdout = open(self._stdout_path, "w+")
        self._stderr = open(self._stderr_path, "w+")
        self._process = subprocess.Popen(
            [str(binary())] + list(self.args) + [str(self.config.path)],
            cwd=self.cwd,
            env=clean_env(**self.env),
            stdin=subprocess.DEVNULL,
            stdout=self._stdout,
            stderr=self._stderr,
            text=True,
        )
        try:
            self._wait_until_listening()
        except Exception:
            self.stop()
            raise
        return self

    def _wait_until_listening(self):
        deadline = time.time() + self.startup_timeout
        while time.time() < deadline:
            if self._process.poll() is not None:
                raise AssertionError(
                    f"server exited during startup with status {self._process.returncode}\n"
                    f"{self.output()}"
                )
            if port_is_open(self.config.port):
                return
            time.sleep(0.02)
        raise AssertionError(
            f"server did not start listening on port {self.config.port} within "
            f"{self.startup_timeout:.0f}s\n{self.output()}"
        )

    def stop(self):
        if self._process is not None:
            if self._process.poll() is None:
                self._process.terminate()
                try:
                    self._process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    self._process.kill()
                    self._process.wait(timeout=10)
            self._process = None
        for handle in (self._stdout, self._stderr):
            if handle is not None:
                handle.flush()
                handle.close()
        self._stdout = self._stderr = None
        # The log directory is per-server and only ever read while it runs.
        if self._stdout_path is not None:
            shutil.rmtree(str(self._stdout_path.parent), True)
            self._stdout_path = self._stderr_path = None

    def __enter__(self):
        return self.start()

    def __exit__(self, *exc):
        self.stop()
        return False

    # -- accessors ---------------------------------------------------------

    @property
    def started(self):
        return self._process is not None

    @property
    def base_url(self):
        return f"http://127.0.0.1:{self.config.port}"

    @property
    def rpc_url(self):
        return f"{self.base_url}/rpc"

    @property
    def pid(self):
        return self._process.pid

    @property
    def returncode(self):
        return None if self._process is None else self._process.poll()

    def stdout(self):
        return self._read(self._stdout, self._stdout_path)

    def stderr(self):
        return self._read(self._stderr, self._stderr_path)

    def output(self):
        return f"--- stdout ---\n{self.stdout()}\n--- stderr ---\n{self.stderr()}"

    @staticmethod
    def _read(handle, path):
        if path is None:
            return ""
        if handle is not None:
            handle.flush()
        try:
            return path.read_text()
        except FileNotFoundError:  # pragma: no cover - defensive
            return ""

    def wait(self, timeout=30):
        """Wait for the process to exit and return its status."""
        return self._process.wait(timeout=timeout)

    def signal(self, sig):
        """Send a signal to the running server (for shutdown cases)."""
        self._process.send_signal(sig)


def run_server(config, *args, **kwargs):
    """Run the binary against ``config`` and wait for it to exit.

    This is how the startup-failure cases are expressed: a config that must be
    refused never listens, so there is nothing to poll -- only an exit status
    and a message to assert on.
    """
    timeout = kwargs.pop("timeout", 60)
    return run_cli(*(list(args) + [config.path]), timeout=timeout, **kwargs)


