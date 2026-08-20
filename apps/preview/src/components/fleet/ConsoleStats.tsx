// The console's overview strip — a bento row of the numbers you watch at a glance: how many daemons
// are deployed and up, how many agents have run, total turns, total tokens, and total spend. Daemon
// counts come from the fleet; the turn/token/cost figures come from the fleet-wide usage roll-up
// (`GET /api/usage`, folded from completed turns). It's the "watching point" header of the console.
import type { ComponentType } from "react";
import { Boxes, Cpu, Activity, Hash, DollarSign } from "lucide-react";

import { useApp } from "@/lib/app-context";
import { compact, totalTokens, usd } from "@/lib/format";
import { cn } from "@/lib/utils";

export function ConsoleStats() {
  const { fleet, usage } = useApp();

  const daemons = fleet?.daemons ?? [];
  const up = daemons.filter((d) => d.state === "ready").length;
  const rows = usage?.agents ?? [];
  const totals = usage?.totals;

  const tokens = totalTokens(totals?.usage);

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
      <Stat
        icon={Boxes}
        label="Runtimes"
        value={String(daemons.length)}
        sub={`${up} up · ${daemons.length - up} idle`}
      />
      <Stat
        icon={Cpu}
        label="Agents"
        value={String(rows.length)}
        sub={rows.length === 1 ? "1 tracked" : `${rows.length} tracked`}
      />
      <Stat
        icon={Activity}
        label="Turns"
        value={compact(totals?.turns)}
        sub="this session"
      />
      <Stat icon={Hash} label="Tokens" value={compact(tokens)} sub="all agents" />
      <Stat
        icon={DollarSign}
        label="Cost"
        value={usd(totals?.costUsd)}
        sub={totals?.costUsd === undefined ? "not reported" : "this session"}
      />
    </div>
  );
}

function Stat({
  icon: Icon,
  label,
  value,
  sub,
  className,
}: {
  icon: ComponentType<{ className?: string }>;
  label: string;
  value: string;
  sub?: string;
  className?: string;
}) {
  return (
    <div className={cn("relative overflow-hidden border border-border bg-card p-4", className)}>
      <Icon className="absolute right-3 top-3 size-4 text-ink/15" />
      <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className="mt-2 text-2xl font-semibold tabular-nums tracking-tight">{value}</div>
      {sub && <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{sub}</div>}
    </div>
  );
}
