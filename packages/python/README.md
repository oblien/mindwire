# MindWire for Python

Build products with the same agent runtime used by MindWire's TypeScript and Go
SDKs. This initial Python SDK connects to an **existing local or remote runtime**
over HTTP and server-sent events. It includes synchronous and asynchronous clients,
turns, streaming, run snapshots, cancellation, interaction responses and conversation
handoffs. The low-level `request()` method exposes the rest of the shared API.

Requires Python 3.10 or newer. Licensed under Apache-2.0.

## Install

Once the first release is published:

```sh
pip install mindwire
```

For development from this repository:

```sh
python -m pip install -e './packages/python[dev]'
```

## Connect and run

Start or connect to a [MindWire runtime](https://mindwire.sh/docs/guides/quickstart)
and authenticate its agent. Set `MINDWIRE_BASE_URL` and `MINDWIRE_TOKEN` in your
application environment. A local runtime usually listens on `http://127.0.0.1:8790`;
use its configured bearer token. Use HTTPS for connections across the network.

```python
import os
from mindwire import Mindwire

with Mindwire(
    base_url=os.environ["MINDWIRE_BASE_URL"],
    token=os.environ["MINDWIRE_TOKEN"],
    agent="codex",
) as mw:
    run = mw.turn(
        chat_id="launch",
        message="Read brief.md and write a launch plan.",
        cwd="/workspace/product",  # A directory on the runtime's computer.
    )
    for event in run:
        if event["type"] == "text":
            print(event.get("text", ""), end="", flush=True)
    outcome = run.wait()
    print(outcome["run"]["status"])
```

`run.wait()` consumes the stream and checks the final run record. Calling it after
iteration replays the stream internally to collect its result. It raises
`RunFailedError` for errors, cancellation, or a stream ending before a terminal
status. Pass `throw_on_error=False` to inspect that outcome yourself.

The Python SDK does not install, launch or provision the runtime in this release.
Local, Docker, SSH and cloud environments work when you provide a reachable
runtime URL and token. Harness authentication and capability support are owned by
the runtime; the SDK does not sign in to an agent on your behalf.

## Async applications

```python
import asyncio
import os
from mindwire import AsyncMindwire

async def main():
    async with AsyncMindwire(
        base_url=os.environ["MINDWIRE_BASE_URL"],
        token=os.environ["MINDWIRE_TOKEN"],
        agent="claude-code",
    ) as mw:
        run = await mw.turn(chat_id="research", message="Summarize this project.")
        async for event in run:
            if event["type"] == "text":
                print(event.get("text", ""), end="", flush=True)
        await run.wait()

asyncio.run(main())
```

## Runs and streams

- `mw.turn(...)` starts a turn. Supply `request_id` and retain it when retrying an
  unacknowledged creation request on runtimes supporting `turnRequestVersion`.
- `mw.run(run_id)` and `mw.latest_run(chat_id)` reattach to existing work.
- `run.snapshot()` reads current output and its cursor;
  `run.stream(after=snapshot["sequence"])` follows newer events on runtimes that
  advertise cursor support.
- `run.cancel()` stops execution. Closing a client or event stream only closes
  observation; the runtime continues admitted work while it remains running.
- `run.respond({...})` answers a supported interaction using the shared API payload.
- `mw.turn(..., mode="resolve", resolve={"maxIterations": 5})` runs bounded
  continuation when the selected harness supports it.

To stop reading early, explicitly close the stream using `contextlib.closing`
(sync) or `contextlib.aclosing` (async). For example:

```python
from contextlib import closing

with closing(run.stream()) as stream:
    for event in stream:
        if event["type"] == "interaction":
            break
```

Unary requests have a configurable timeout (`timeout=120.0`, in seconds). SSE
streams have no read timeout so an agent can continue working through idle periods.
The client does not automatically retry turns or reconnect interrupted streams.

## Shared API and handoffs

`mw.request(method, path, json=..., params=...)` returns the parsed response body.
It accepts runtime API paths, not arbitrary URLs. All payloads, response objects,
turn `options` and event fields retain the shared API's **camelCase** field names.
Python method parameters use snake_case.

```python
bots = mw.request("GET", "/bots")
config = mw.request("GET", "/config", params={"agent": "codex"})
preview = mw.preview_handoff("launch")
handoff = mw.handoff("launch", {
    "newChatId": "launch-review",
    "requestId": "launch-review-001",
    "targetAgentId": "saved-claude-profile-id",
    "task": "Review the plan and identify missing steps.",
})
review_run = mw.run(handoff["run"]["id"])
```

Handoffs require a compatible runtime, an eligible source conversation and a saved
destination agent profile. Use `preview` to inspect the context before submission;
see the [handoff guide](https://mindwire.sh/docs/guides/handoffs) for field semantics.

`APIError` preserves `status`, `method`, `path` and the server's `body`.
`ConnectionError` and `TimeoutError` identify transport failures. All inherit
from `MindwireError`. Errors do not expose the configured bearer token.

## Development and releases

```sh
cd packages/python
python -m pytest
ruff check src tests
mypy src
python -m build
python -m twine check --strict dist/*
```

The Python package has its own version, initially `0.1.0`. The runtime and
TypeScript package keep their existing release cycle. See [RELEASING.md](RELEASING.md)
for PyPI setup and publishing.
