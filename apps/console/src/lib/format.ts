// Compact display formatting for the monitoring console — token counts and dollar costs get dense so a
// glance reads "1.2M tokens · $3.40" instead of raw digits. Pure, dependency-free.
import type { Usage } from "@shared/api";

/** `1234 → "1.2k"`, `1_200_000 → "1.2M"`, small values verbatim. Absent/zero → "0". */
export function compact(n: number | undefined): string {
  if (!n) return "0";
  const abs = Math.abs(n);
  if (abs < 1000) return String(n);
  if (abs < 1_000_000) return `${trim(n / 1000)}k`;
  if (abs < 1_000_000_000) return `${trim(n / 1_000_000)}M`;
  return `${trim(n / 1_000_000_000)}B`;
}

/** One decimal, but drop a trailing `.0` so `1.0k` reads `1k`. */
function trim(v: number): string {
  return v.toFixed(1).replace(/\.0$/, "");
}

/** Bytes → human units on the binary scale (`1536 → "1.5 KiB"`). Absent/zero → "0 B". */
export function formatBytes(n: number | undefined): string {
  if (!n) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : trim(v)} ${units[i]}`;
}

/** Seconds → coarse uptime (`"3d 4h"`, `"12m 5s"`); the two largest non-zero units. `0 → "0s"`. */
export function formatUptime(sec: number | undefined): string {
  if (!sec || sec < 0) return "0s";
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  const parts = [
    [d, "d"],
    [h, "h"],
    [m, "m"],
    [s, "s"],
  ] as const;
  const shown = parts.filter(([v]) => v > 0).slice(0, 2);
  return (shown.length ? shown : [[0, "s"] as const]).map(([v, u]) => `${v}${u}`).join(" ");
}

/** USD with sensible precision: sub-cent shows more places so tiny turns aren't rounded to `$0.00`. */
export function usd(n: number | undefined): string {
  if (n === undefined) return "—";
  if (n === 0) return "$0";
  if (n < 0.01) return `$${n.toFixed(4)}`;
  if (n < 1) return `$${n.toFixed(3)}`;
  return `$${n.toFixed(2)}`;
}

/**
 * Grand total for a {@link Usage} row: the agent's own `totalTokens` when it reported one, else the
 * sum of the component counters (input + output + cache + reasoning). Best-effort — 0 when nothing set.
 */
export function totalTokens(u: Usage | undefined): number {
  if (!u) return 0;
  if (typeof u.totalTokens === "number" && u.totalTokens > 0) return u.totalTokens;
  return (
    (u.inputTokens ?? 0) +
    (u.outputTokens ?? 0) +
    (u.cacheReadTokens ?? 0) +
    (u.cacheWriteTokens ?? 0) +
    (u.reasoningTokens ?? 0)
  );
}
