from __future__ import annotations

from collections.abc import AsyncIterator, Iterator, Mapping
from typing import TYPE_CHECKING, Any

from ._errors import RunFailedError
from ._transport import run_record, segment
from ._types import Event, RunRecord, WaitResult

if TYPE_CHECKING:
    from ._client import AsyncMindwire, Mindwire


def completion(record: RunRecord, result: dict[str, Any] | None) -> WaitResult:
    if record["status"] != "done":
        detail = record.get("error", "")
        if record["status"] not in ("error", "cancelled"):
            detail = "event stream ended before the run reached a terminal state"
        raise RunFailedError(record["id"], record["status"], detail)
    return {"run": record, "result": result}


class _RunBase:
    def __init__(self, record: RunRecord) -> None:
        self._record = run_record(record)
        self._path = "/runs/" + segment(self._record["id"])

    @property
    def id(self) -> str:
        return self._record["id"]

    @property
    def chat_id(self) -> str:
        return self._record["chatId"]

    @property
    def status(self) -> str:
        """The latest known status. Call refresh() to read it from the runtime."""
        return self._record["status"]

    @property
    def value(self) -> RunRecord:
        return self._record.copy()


class Run(_RunBase):
    def __init__(self, client: Mindwire, record: RunRecord) -> None:
        super().__init__(record)
        self._client = client

    def stream(
        self, *, after: int | None = None, include_open_sentinel: bool = False
    ) -> Iterator[Event]:
        """Replay and follow events; close the iterator when stopping early."""
        return self._client.stream(
            self._path + "/stream",
            params={"after": after} if after is not None else None,
            include_open_sentinel=include_open_sentinel,
        )

    def __iter__(self) -> Iterator[Event]:
        return self.stream()

    def refresh(self) -> RunRecord:
        self._record = run_record(self._client.request("GET", self._path))
        return self.value

    def snapshot(self) -> dict[str, Any]:
        value: dict[str, Any] = self._client.request("GET", self._path + "/snapshot")
        self._record = run_record(value["run"])
        return value

    def cancel(self) -> None:
        """Stop the work on the runtime, rather than just disconnecting its stream."""
        self._client.request("POST", self._path + "/cancel")

    def respond(self, input: Mapping[str, Any]) -> None:
        """Answer an interaction with the shared API's response payload."""
        self._client.request("POST", self._path + "/respond", json=dict(input))

    def wait(self, *, throw_on_error: bool = True) -> WaitResult:
        result = None
        for event in self.stream():
            if event["type"] == "result":
                result = event.get("result")
        record = self.refresh()
        if throw_on_error:
            return completion(record, result)
        return {"run": record, "result": result}


class AsyncRun(_RunBase):
    def __init__(self, client: AsyncMindwire, record: RunRecord) -> None:
        super().__init__(record)
        self._client = client

    def stream(
        self, *, after: int | None = None, include_open_sentinel: bool = False
    ) -> AsyncIterator[Event]:
        """Replay and follow events. Use contextlib.aclosing when stopping early."""
        return self._client.stream(
            self._path + "/stream",
            params={"after": after} if after is not None else None,
            include_open_sentinel=include_open_sentinel,
        )

    def __aiter__(self) -> AsyncIterator[Event]:
        return self.stream()

    async def refresh(self) -> RunRecord:
        self._record = run_record(await self._client.request("GET", self._path))
        return self.value

    async def snapshot(self) -> dict[str, Any]:
        value: dict[str, Any] = await self._client.request(
            "GET", self._path + "/snapshot"
        )
        self._record = run_record(value["run"])
        return value

    async def cancel(self) -> None:
        await self._client.request("POST", self._path + "/cancel")

    async def respond(self, input: Mapping[str, Any]) -> None:
        await self._client.request("POST", self._path + "/respond", json=dict(input))

    async def wait(self, *, throw_on_error: bool = True) -> WaitResult:
        result = None
        async for event in self.stream():
            if event["type"] == "result":
                result = event.get("result")
        record = await self.refresh()
        if throw_on_error:
            return completion(record, result)
        return {"run": record, "result": result}
