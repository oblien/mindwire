#!/usr/bin/env bash
#
# sse-probe.sh — does an SSE endpoint stream live, or does a proxy buffer it?
#
# Posts a turn to the daemon, then times every event on GET /runs/{id}/stream and prints a verdict.
# Run it against the two hops to isolate WHERE streaming breaks:
#
#   1) Daemon directly (inside the sandbox — baseline, should always be "LIVE"):
#        ./sse-probe.sh http://127.0.0.1:8790
#
#   2) Through the Oblien runtime proxy (from outside — the hop we suspect):
#        ./sse-probe.sh https://<gateway-host>/proxy/8790 "Authorization: Bearer <runtime-jwt>"
#
# If (1) is LIVE and (2) is BUFFERED, the runtime proxy does not stream SSE — switch the app's
# cloud stream transport to the exec channel. If BOTH buffer, it's the daemon. If both are LIVE
# but the app shows nothing, it's client-side parsing (watch the app's "undecodable event" logs).
#
# Args: <base-url> [auth-header] [agent-type]
set -euo pipefail

BASE="${1:?usage: sse-probe.sh <base-url> [auth-header] [agent-type]}"
AUTH="${2:-}"
AGENT="${3:-claude-code}"
BASE="${BASE%/}"

curl_args=(-sS --max-time 130)
[ -n "$AUTH" ] && curl_args+=(-H "$AUTH")

echo "→ POST $BASE/turns?agent=$AGENT"
resp=$(curl "${curl_args[@]}" -X POST "$BASE/turns?agent=$AGENT" \
  -H 'Content-Type: application/json' \
  -d '{"chatId":"sse-probe","message":"Reply with the three words: red green blue"}')
echo "  $resp"
run=$(printf '%s' "$resp" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
[ -z "$run" ] && { echo "no run id — turn POST failed"; exit 1; }

echo "→ GET  $BASE/runs/$run/stream"
echo "----"
curl "${curl_args[@]}" -N "$BASE/runs/$run/stream" | python3 -c '
import sys, time
start = time.time(); first = None; last = None; n = 0
for line in sys.stdin:
    line = line.rstrip("\n")
    if not line:            # blank line between SSE frames
        continue
    el = int((time.time() - start) * 1000)
    n += 1
    if first is None: first = el
    last = el
    print(f"+{el:>6}ms | {line}", flush=True)
    if "\"type\":\"result\"" in line:
        break
print("----")
if n <= 1:
    print(f"events={n} — need >1 event to judge (turn may have errored instantly)")
else:
    spread = last - first
    if first > 3000 and spread < 250:
        print(f"VERDICT: BUFFERED — proxy withheld {first}ms, then dumped {n} events in {spread}ms (no live SSE)")
    else:
        print(f"VERDICT: LIVE — {n} events spread over {spread}ms (first +{first}ms)")
'
