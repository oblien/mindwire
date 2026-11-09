"""Wire payloads retain the shared API's field names, including camelCase keys."""

from typing import Any, TypedDict


class Event(TypedDict, total=False):
    type: str
    sequence: int
    replay: bool
    itemId: str
    sessionId: str
    text: str
    delta: bool
    result: dict[str, Any]
    tool: dict[str, Any]
    interaction: dict[str, Any]
    error: str
    meta: dict[str, Any]
    at: str


class _RunRequired(TypedDict):
    id: str
    chatId: str
    status: str


class RunRecord(_RunRequired, total=False):
    agent: str
    error: str
    kind: str
    parentId: str
    stopReason: str
    iterations: int


class WaitResult(TypedDict):
    run: RunRecord
    result: dict[str, Any] | None
