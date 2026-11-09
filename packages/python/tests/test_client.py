import asyncio
import json
from contextlib import closing

import httpx
import pytest

from mindwire import (
    APIError,
    AsyncMindwire,
    ConnectionError,
    Mindwire,
    MindwireError,
    RunFailedError,
    TimeoutError,
)


def record(status="running"):
    return {"id": "run-1", "chatId": "launch", "status": status}


def test_turn_preserves_options_agent_auth_and_request_id():
    seen = []

    def handle(request):
        seen.append(request)
        assert request.url.path == "/proxy/api/turns"
        assert request.url.params["agent"] == "claude-code"
        assert request.headers["Authorization"] == "Bearer secret"
        assert json.loads(request.content) == {
            "chatId": "launch",
            "message": "Build it",
            "mode": "resolve",
            "cwd": "/work",
            "requestId": "req-1",
            "options": {"systemPrompt": "Be precise"},
            "resolve": {"maxIterations": 2},
        }
        return httpx.Response(200, json=record())

    with Mindwire(
        base_url="https://example.com/proxy/api/",
        token="secret",
        agent="codex",
        transport=httpx.MockTransport(handle),
    ) as mw:
        run = mw.turn(
            chat_id="launch",
            message="Build it",
            cwd="/work",
            agent="claude-code",
            options={"systemPrompt": "Be precise"},
            request_id="req-1",
            mode="resolve",
            resolve={"maxIterations": 2},
        )
        assert run.id == "run-1"
        assert run.chat_id == "launch"
        assert run.status == "running"
        assert len(seen) == 1


class ClosingStream(httpx.SyncByteStream):
    def __init__(self, data):
        self.data = data
        self.closed = False

    def __iter__(self):
        # Split unicode and CRLF across network chunks.
        for byte in self.data:
            yield bytes([byte])

    def close(self):
        self.closed = True


def test_stream_frames_unicode_cursor_and_closure():
    source = ClosingStream(
        ': ping\r\n\r\ndata: {"type":"status","meta":{"stream":"open"}}\r\n\r\n'
        'data: {"type":"text",\r\ndata: "text":"أهلاً 🌍","sequence":8}\r\n\r\n'
        'data: {"type":"result","result":{"text":"done"}}'.encode()
    )

    def handle(request):
        if request.url.path == "/runs/run-1":
            return httpx.Response(200, json=record())
        assert request.headers["accept"] == "text/event-stream"
        assert request.url.params["after"] == "7"
        assert request.extensions["timeout"]["read"] is None
        return httpx.Response(
            200, stream=source, headers={"content-type": "text/event-stream"}
        )

    with Mindwire(
        base_url="http://localhost:8790", transport=httpx.MockTransport(handle)
    ) as mw:
        values = list(mw.run("run-1").stream(after=7))
        assert [event["type"] for event in values] == ["text", "result"]
        assert values[0]["text"] == "أهلاً 🌍"
        assert source.closed


def test_early_stream_close_releases_response_without_cancelling_work():
    source = ClosingStream(b'data: {"type":"text","text":"one"}\n\n')
    paths = []

    def handle(request):
        paths.append(request.url.path)
        return httpx.Response(
            200, stream=source, headers={"content-type": "text/event-stream"}
        )

    with Mindwire(
        base_url="http://localhost", transport=httpx.MockTransport(handle)
    ) as mw:
        with closing(mw.stream("/runs/run-1/stream")) as stream:
            assert next(stream)["text"] == "one"
        assert source.closed
        assert paths == ["/runs/run-1/stream"]


@pytest.mark.parametrize("status", ["done", "error", "cancelled", "running"])
def test_wait_checks_actual_completion(status):
    def handle(request):
        if request.url.path.endswith("/stream"):
            return httpx.Response(
                200,
                text='data: {"type":"result","result":{"text":"finished"}}\n\n',
                headers={"content-type": "text/event-stream"},
            )
        return httpx.Response(200, json=record(status))

    with Mindwire(
        base_url="http://localhost", transport=httpx.MockTransport(handle)
    ) as mw:
        run = mw.run("run-1")
        if status == "done":
            assert run.wait()["result"]["text"] == "finished"
        else:
            with pytest.raises(RunFailedError) as error:
                run.wait()
            assert error.value.status == status
            assert run.wait(throw_on_error=False)["run"]["status"] == status


@pytest.mark.parametrize("body", [{"error": "busy"}, "unauthorized"])
def test_http_errors_and_no_implicit_turn_retry(body):
    calls = []

    def handle(request):
        calls.append(request)
        if isinstance(body, dict):
            return httpx.Response(409, json=body)
        return httpx.Response(401, text=body)

    with Mindwire(
        base_url="http://localhost",
        token="private-token",
        transport=httpx.MockTransport(handle),
    ) as mw:
        with pytest.raises(APIError) as error:
            mw.turn(chat_id="launch", message="Build")
        assert error.value.body == body
        assert "private-token" not in str(error.value)
        assert len(calls) == 1


@pytest.mark.parametrize(
    "path",
    ["https://other.com/x", "//other.com/x", "/x?token=x", "/x#y", "/\\other.com"],
)
def test_raw_requests_cannot_redirect_credentials(path):
    with Mindwire(base_url="https://example.com", token="secret") as mw:
        with pytest.raises(ValueError):
            mw.request("GET", path)


@pytest.mark.parametrize(
    "url", ["file:///tmp/x", "https://user:pass@example.com", "https://example.com?x=y"]
)
def test_invalid_base_urls(url):
    with pytest.raises(ValueError):
        Mindwire(base_url=url)


@pytest.mark.parametrize(
    "payload", ["data: not-json\n\n", "data: []\n\n", "data: {}\n\n"]
)
def test_invalid_sse_is_an_sdk_error(payload):
    def handle(request):
        return httpx.Response(
            200, text=payload, headers={"content-type": "text/event-stream"}
        )

    with Mindwire(
        base_url="http://localhost", transport=httpx.MockTransport(handle)
    ) as mw:
        with pytest.raises(MindwireError):
            list(mw.stream("/events"))


@pytest.mark.parametrize(
    "error_type,expected",
    [(httpx.ReadTimeout, TimeoutError), (httpx.ConnectError, ConnectionError)],
)
def test_transport_errors(error_type, expected):
    def handle(request):
        raise error_type("failure", request=request)

    with Mindwire(
        base_url="http://localhost", transport=httpx.MockTransport(handle)
    ) as mw:
        with pytest.raises(expected):
            mw.health()


def test_missing_latest_run_and_no_content_controls():
    seen = []

    def handle(request):
        seen.append(request)
        if request.url.path == "/runs/run-1":
            return httpx.Response(200, json=record())
        if request.url.path.endswith("/run"):
            return httpx.Response(200, text="null")
        return httpx.Response(204)

    with Mindwire(
        base_url="http://localhost", transport=httpx.MockTransport(handle)
    ) as mw:
        assert mw.latest_run("empty") is None
        run = mw.run("run-1")
        run.respond({"interactionId": "ask-1", "decision": "approve"})
        run.cancel()
        assert json.loads(seen[-2].content)["interactionId"] == "ask-1"
        assert seen[-1].url.path == "/runs/run-1/cancel"


def test_handoffs_preserve_native_fields():
    def handle(request):
        assert request.url.path == "/chats/source/handoff"
        if request.method == "GET":
            return httpx.Response(200, json={"digest": "context-digest"})
        assert json.loads(request.content)["requestId"] == "handoff-001"
        return httpx.Response(200, json={"chatId": "review", "run": record()})

    with Mindwire(
        base_url="http://localhost", transport=httpx.MockTransport(handle)
    ) as mw:
        assert mw.preview_handoff("source")["digest"] == "context-digest"
        assert mw.handoff("source", {"requestId": "handoff-001"})["chatId"] == "review"


def test_async_turn_stream_and_wait():
    seen = []

    def handle(request):
        seen.append(request)
        assert request.headers["authorization"] == "Bearer async-secret"
        if request.url.path == "/turns":
            assert request.url.params["agent"] == "codex"
            return httpx.Response(200, json=record())
        if request.url.path.endswith("/stream"):
            assert request.extensions["timeout"]["read"] is None
            return httpx.Response(
                200,
                text='data: {"type":"text","text":"hello"}\n\n'
                'data: {"type":"result","result":{"text":"hello"}}\n\n',
                headers={"content-type": "text/event-stream"},
            )
        return httpx.Response(200, json=record("done"))

    async def check():
        async with AsyncMindwire(
            base_url="http://localhost",
            token="async-secret",
            agent="codex",
            transport=httpx.MockTransport(handle),
        ) as mw:
            run = await mw.turn(chat_id="launch", message="Hello")
            assert [event["type"] async for event in run] == ["text", "result"]
            assert (await run.wait())["result"]["text"] == "hello"
        assert len(seen) == 4

    asyncio.run(check())
