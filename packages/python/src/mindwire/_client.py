from __future__ import annotations

from collections.abc import AsyncIterator, Iterator, Mapping
from typing import Any

import httpx

from ._errors import MindwireError
from ._runs import AsyncRun, Run
from ._transport import (
    Connection,
    async_events,
    decode_response,
    events,
    network_errors,
    run_record,
    segment,
    stream_timeout,
)
from ._types import Event

Query = Mapping[str, str | int | float | bool | None]


def turn_body(
    chat_id: str,
    message: str,
    cwd: str | None,
    options: Mapping[str, Any] | None,
    request_id: str | None,
    mode: str,
    resolve: Mapping[str, Any] | None,
) -> dict[str, Any]:
    body: dict[str, Any] = {"chatId": chat_id, "message": message, "mode": mode}
    for key, value in (
        ("cwd", cwd),
        ("options", options),
        ("requestId", request_id),
        ("resolve", resolve),
    ):
        if value is not None:
            body[key] = value
    return body


class Mindwire:
    """A synchronous connection to an existing local or remote MindWire runtime."""

    def __init__(
        self,
        *,
        base_url: str,
        token: str | None = None,
        agent: str | None = None,
        timeout: float | None = 120.0,
        headers: Mapping[str, str] | None = None,
        transport: httpx.BaseTransport | None = None,
    ) -> None:
        self._connection = Connection(base_url, token, agent, headers)
        self._http = httpx.Client(
            headers=self._connection.headers,
            timeout=timeout,
            transport=transport,
            follow_redirects=False,
        )

    def __enter__(self) -> Mindwire:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def close(self) -> None:
        """Close HTTP connections; admitted work continues on the runtime."""
        self._http.close()

    def request(
        self, method: str, path: str, *, json: Any = None, params: Query | None = None
    ) -> Any:
        """Call any shared API endpoint using its native JSON fields."""
        with network_errors():
            response = self._http.request(
                method, self._connection.url(path), json=json, params=params
            )
            return decode_response(response)

    def stream(
        self,
        path: str,
        *,
        params: Query | None = None,
        include_open_sentinel: bool = False,
    ) -> Iterator[Event]:
        with network_errors():
            timeout = stream_timeout(self._http.timeout)
            with self._http.stream(
                "GET",
                self._connection.url(path),
                params=params,
                headers={"Accept": "text/event-stream"},
                timeout=timeout,
            ) as response:
                if not response.is_success:
                    response.read()
                    decode_response(response)
                if "text/event-stream" not in response.headers.get("content-type", ""):
                    raise MindwireError(
                        "The runtime did not return an SSE event stream"
                    )
                response.encoding = "utf-8"
                yield from events(response.iter_lines(), include_open_sentinel)

    def health(self) -> dict[str, Any]:
        value: dict[str, Any] = self.request("GET", "/healthz")
        return value

    def catalog(self) -> dict[str, Any]:
        value: dict[str, Any] = self.request("GET", "/catalog")
        return value

    def turn(
        self,
        *,
        chat_id: str,
        message: str,
        agent: str | None = None,
        cwd: str | None = None,
        options: Mapping[str, Any] | None = None,
        request_id: str | None = None,
        mode: str = "turn",
        resolve: Mapping[str, Any] | None = None,
    ) -> Run:
        """Start work. Reuse request_id when retrying an unacknowledged request."""
        data = self.request(
            "POST",
            "/turns",
            params=self._connection.agent_params(agent),
            json=turn_body(chat_id, message, cwd, options, request_id, mode, resolve),
        )
        return Run(self, run_record(data))

    def run(self, run_id: str) -> Run:
        return Run(self, run_record(self.request("GET", "/runs/" + segment(run_id))))

    def latest_run(self, chat_id: str) -> Run | None:
        data = self.request("GET", f"/chats/{segment(chat_id)}/run")
        return Run(self, run_record(data)) if data is not None else None

    def preview_handoff(self, chat_id: str) -> dict[str, Any]:
        value: dict[str, Any] = self.request(
            "GET", f"/chats/{segment(chat_id)}/handoff"
        )
        return value

    def handoff(self, chat_id: str, input: Mapping[str, Any]) -> dict[str, Any]:
        """Submit a handoff payload with stable request and destination IDs."""
        value: dict[str, Any] = self.request(
            "POST", f"/chats/{segment(chat_id)}/handoff", json=dict(input)
        )
        return value


class AsyncMindwire:
    """An asynchronous connection for Python applications and background services."""

    def __init__(
        self,
        *,
        base_url: str,
        token: str | None = None,
        agent: str | None = None,
        timeout: float | None = 120.0,
        headers: Mapping[str, str] | None = None,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        self._connection = Connection(base_url, token, agent, headers)
        self._http = httpx.AsyncClient(
            headers=self._connection.headers,
            timeout=timeout,
            transport=transport,
            follow_redirects=False,
        )

    async def __aenter__(self) -> AsyncMindwire:
        return self

    async def __aexit__(self, *args: Any) -> None:
        await self.close()

    async def close(self) -> None:
        await self._http.aclose()

    async def request(
        self, method: str, path: str, *, json: Any = None, params: Query | None = None
    ) -> Any:
        with network_errors():
            response = await self._http.request(
                method, self._connection.url(path), json=json, params=params
            )
            return decode_response(response)

    async def stream(
        self,
        path: str,
        *,
        params: Query | None = None,
        include_open_sentinel: bool = False,
    ) -> AsyncIterator[Event]:
        with network_errors():
            timeout = stream_timeout(self._http.timeout)
            async with self._http.stream(
                "GET",
                self._connection.url(path),
                params=params,
                headers={"Accept": "text/event-stream"},
                timeout=timeout,
            ) as response:
                if not response.is_success:
                    await response.aread()
                    decode_response(response)
                if "text/event-stream" not in response.headers.get("content-type", ""):
                    raise MindwireError(
                        "The runtime did not return an SSE event stream"
                    )
                response.encoding = "utf-8"
                async for event in async_events(
                    response.aiter_lines(), include_open_sentinel
                ):
                    yield event

    async def health(self) -> dict[str, Any]:
        value: dict[str, Any] = await self.request("GET", "/healthz")
        return value

    async def catalog(self) -> dict[str, Any]:
        value: dict[str, Any] = await self.request("GET", "/catalog")
        return value

    async def turn(
        self,
        *,
        chat_id: str,
        message: str,
        agent: str | None = None,
        cwd: str | None = None,
        options: Mapping[str, Any] | None = None,
        request_id: str | None = None,
        mode: str = "turn",
        resolve: Mapping[str, Any] | None = None,
    ) -> AsyncRun:
        data = await self.request(
            "POST",
            "/turns",
            params=self._connection.agent_params(agent),
            json=turn_body(chat_id, message, cwd, options, request_id, mode, resolve),
        )
        return AsyncRun(self, run_record(data))

    async def run(self, run_id: str) -> AsyncRun:
        data = await self.request("GET", "/runs/" + segment(run_id))
        return AsyncRun(self, run_record(data))

    async def latest_run(self, chat_id: str) -> AsyncRun | None:
        data = await self.request("GET", f"/chats/{segment(chat_id)}/run")
        return AsyncRun(self, run_record(data)) if data is not None else None

    async def preview_handoff(self, chat_id: str) -> dict[str, Any]:
        value: dict[str, Any] = await self.request(
            "GET", f"/chats/{segment(chat_id)}/handoff"
        )
        return value

    async def handoff(self, chat_id: str, input: Mapping[str, Any]) -> dict[str, Any]:
        value: dict[str, Any] = await self.request(
            "POST", f"/chats/{segment(chat_id)}/handoff", json=dict(input)
        )
        return value
