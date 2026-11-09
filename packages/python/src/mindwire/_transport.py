import json
from collections.abc import AsyncIterator, Iterator, Mapping
from contextlib import contextmanager
from typing import Any, cast
from urllib.parse import quote, urlsplit

import httpx

from ._errors import APIError, ConnectionError, MindwireError, TimeoutError
from ._types import Event, RunRecord
from ._version import __version__


def stream_timeout(timeout: httpx.Timeout) -> httpx.Timeout:
    value = httpx.Timeout(timeout)
    value.read = None
    return value


def segment(value: str) -> str:
    if not value or value in (".", ".."):
        raise ValueError("An identifier must be nonempty and cannot be '.' or '..'")
    return quote(value, safe="")


def run_record(value: Any) -> RunRecord:
    if not isinstance(value, dict) or any(
        not isinstance(value.get(key), str) for key in ("id", "chatId", "status")
    ):
        raise MindwireError("The runtime returned an invalid run record")
    return cast(RunRecord, value)


def decode_response(response: httpx.Response) -> Any:
    if not response.is_success:
        try:
            body = response.json()
        except ValueError:
            body = response.text
        raise APIError(
            response.status_code,
            response.request.method,
            response.request.url.path,
            body,
        )
    if response.status_code == 204 or not response.content:
        return None
    try:
        return response.json()
    except ValueError as error:
        raise MindwireError("The runtime returned a non-JSON response") from error


@contextmanager
def network_errors() -> Iterator[None]:
    try:
        yield
    except httpx.TimeoutException as error:
        raise TimeoutError("The MindWire runtime request timed out") from error
    except httpx.HTTPError as error:
        raise ConnectionError("The MindWire runtime connection failed") from error


class Connection:
    def __init__(
        self,
        base_url: str,
        token: str | None,
        agent: str | None,
        headers: Mapping[str, str] | None,
    ) -> None:
        parsed = urlsplit(base_url)
        if (
            parsed.scheme not in ("http", "https")
            or not parsed.netloc
            or parsed.username is not None
            or parsed.password is not None
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError(
                "base_url must be an HTTP(S) URL without credentials or query"
            )
        self.base_url = base_url.rstrip("/")
        self.agent = agent
        self.headers = httpx.Headers(headers or {})
        self.headers.setdefault("Accept", "application/json")
        self.headers.setdefault("User-Agent", f"mindwire-python/{__version__}")
        if token:
            self.headers["Authorization"] = f"Bearer {token}"

    def url(self, path: str) -> str:
        # Only relative API paths are accepted; tokens cannot be sent to another origin.
        parsed = urlsplit(path)
        if (
            not path.startswith("/")
            or path.startswith("//")
            or "\\" in path
            or parsed.scheme
            or parsed.netloc
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError(
                "Use an API path beginning with '/', and pass params separately"
            )
        return self.base_url + path

    def agent_params(self, agent: str | None = None) -> dict[str, str]:
        selected = self.agent if agent is None else agent
        return {"agent": selected} if selected else {}


class SSEDecoder:
    def __init__(self) -> None:
        self.data: list[str] = []

    def feed(self, line: str) -> Event | None:
        if line == "":
            return self.finish()
        field, _, value = line.partition(":")
        if field == "data":
            self.data.append(value.removeprefix(" "))
        return None

    def finish(self) -> Event | None:
        if not self.data:
            return None
        payload, self.data = "\n".join(self.data), []
        try:
            value = json.loads(payload)
        except ValueError as error:
            raise MindwireError("The runtime sent an invalid JSON event") from error
        if not isinstance(value, dict) or not isinstance(value.get("type"), str):
            raise MindwireError("The runtime sent an invalid event object")
        return cast(Event, value)


def is_open_sentinel(event: Event) -> bool:
    return (
        event["type"] == "status" and (event.get("meta") or {}).get("stream") == "open"
    )


def events(lines: Iterator[str], include_open_sentinel: bool) -> Iterator[Event]:
    decoder = SSEDecoder()
    for line in lines:
        event = decoder.feed(line)
        if event is not None and (include_open_sentinel or not is_open_sentinel(event)):
            yield event
    event = decoder.finish()
    if event is not None and (include_open_sentinel or not is_open_sentinel(event)):
        yield event


async def async_events(
    lines: AsyncIterator[str], include_open_sentinel: bool
) -> AsyncIterator[Event]:
    decoder = SSEDecoder()
    async for line in lines:
        event = decoder.feed(line)
        if event is not None and (include_open_sentinel or not is_open_sentinel(event)):
            yield event
    event = decoder.finish()
    if event is not None and (include_open_sentinel or not is_open_sentinel(event)):
        yield event
