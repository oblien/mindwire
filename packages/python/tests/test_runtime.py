"""Opt-in checks against the real Go runtime, with no agent login or paid request."""

import asyncio
import os
import secrets
import socket
import subprocess
import time
from pathlib import Path

import pytest

from mindwire import APIError, AsyncMindwire, ConnectionError, Mindwire


@pytest.fixture(scope="module")
def runtime(tmp_path_factory):
    binary = os.environ.get("MINDWIRE_TEST_DAEMON")
    if not binary:
        pytest.skip(
            "set MINDWIRE_TEST_DAEMON to a built runtime for integration checks"
        )
    directory = tmp_path_factory.mktemp("runtime")
    token = secrets.token_hex(32)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    url = f"http://127.0.0.1:{port}"
    env = dict(
        os.environ,
        DAEMON_TOKEN=token,
        ADDR=f"127.0.0.1:{port}",
        STATE_PATH=str(directory / "state.json"),
        AGENT_CWD=str(directory),
    )
    with (directory / "daemon.log").open("w") as log:
        process = subprocess.Popen(
            [str(Path(binary).resolve())],
            cwd=directory,
            env=env,
            stdout=log,
            stderr=log,
        )
        try:
            deadline = time.monotonic() + 20
            with Mindwire(base_url=url, token=token, timeout=1) as mw:
                while True:
                    if process.poll() is not None:
                        pytest.fail("Runtime exited during startup")
                    try:
                        assert mw.health()["ok"] is True
                        break
                    except ConnectionError:
                        if time.monotonic() >= deadline:
                            raise
                        time.sleep(0.05)
            yield url, token
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


def test_sync_real_runtime(runtime):
    url, token = runtime
    with Mindwire(base_url=url, token=token) as mw:
        assert mw.health()["version"]
        assert mw.catalog()["agents"]
        assert mw.request("GET", "/bots") == []
        assert mw.latest_run("not-created") is None
        with pytest.raises(APIError) as error:
            mw.run("missing-run")
        assert error.value.status == 404
    with Mindwire(base_url=url, token="incorrect") as mw:
        with pytest.raises(APIError) as error:
            mw.health()
        assert error.value.status == 401


def test_async_real_runtime(runtime):
    url, token = runtime

    async def check():
        async with AsyncMindwire(base_url=url, token=token) as mw:
            assert (await mw.health())["ok"] is True
            assert (await mw.catalog())["agents"]
            assert await mw.latest_run("not-created") is None

    asyncio.run(check())
