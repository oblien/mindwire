from typing import Any


class MindwireError(Exception):
    """Base error raised by the SDK."""


class APIError(MindwireError):
    """A non-success HTTP response, with the daemon's error body preserved."""

    def __init__(self, status: int, method: str, path: str, body: Any) -> None:
        self.status = status
        self.method = method
        self.path = path
        self.body = body
        detail = body.get("error", "") if isinstance(body, dict) else str(body)
        super().__init__(f"{method} {path} returned {status}: {detail}")


class ConnectionError(MindwireError):
    """The runtime could not be reached or its connection was interrupted."""


class TimeoutError(MindwireError):
    """An HTTP operation timed out. A run may still be executing on the runtime."""


class RunFailedError(MindwireError):
    def __init__(self, run_id: str, status: str, detail: str = "") -> None:
        self.run_id = run_id
        self.status = status
        super().__init__(f"run {run_id} {status}" + (f": {detail}" if detail else ""))
