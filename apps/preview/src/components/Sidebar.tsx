// The left nav. Items come from `nav.ts`, filtered by the selected agent's capabilities (an agent that
// can't do custom providers / subagents / etc. never shows those surfaces), then grouped by their
// `group` field in first-seen order. Runtime is always present; the rest appear once an agent resolves.
import { useMemo } from "react";

import { useApp } from "@/lib/app-context";
import { visibleNav, AGENT_GROUP, AGENT_GROUP_LABEL, type NavItem, type ViewKey } from "@/lib/nav";
import { cn } from "@/lib/utils";
import { AgentSwitcher } from "@/components/AgentSwitcher";

export function Sidebar({
  active,
  onSelect,
}: {
  active: ViewKey;
  onSelect: (key: ViewKey) => void;
}) {
  const { agent } = useApp();

  const groups = useMemo(() => {
    const items = visibleNav(agent?.capabilities ?? null);
    const order: string[] = [];
    const byGroup = new Map<string, NavItem[]>();
    for (const item of items) {
      if (!byGroup.has(item.group)) {
        byGroup.set(item.group, []);
        order.push(item.group);
      }
      byGroup.get(item.group)!.push(item);
    }
    return order.map((g) => ({ group: g, items: byGroup.get(g)! }));
  }, [agent]);

  return (
    <nav className="flex w-52 shrink-0 flex-col gap-5 overflow-y-auto border-r border-border p-3">
      {groups.map(({ group, items }) => (
        <div key={group} className="space-y-1">
          {/* Every group carries the same uppercase eyebrow. The Agent group's eyebrow ("Configuration")
              names its function and sits above the live agent switcher — the switcher picks which adapter
              all the surfaces below configure; every other group's eyebrow is just its name. */}
          <p className="px-2 pb-1 text-[0.7rem] font-medium uppercase tracking-wider text-muted-foreground">
            {group === AGENT_GROUP ? AGENT_GROUP_LABEL : group}
          </p>
          {group === AGENT_GROUP && (
            <div className="pb-1">
              <AgentSwitcher />
            </div>
          )}
          {items.map((item) => {
            const Icon = item.icon;
            const isActive = item.key === active;
            return (
              <button
                key={item.key}
                type="button"
                onClick={() => onSelect(item.key)}
                className={cn(
                  "relative flex w-full items-center gap-2.5 px-2.5 py-1.5 text-left text-sm transition-colors",
                  // Active isn't a solid white slab (too loud, reads as "over solid") — it's the same
                  // translucent film + left accent bar the active DaemonBlock uses, so "selected" reads
                  // identically everywhere in the app.
                  isActive
                    ? "bg-ink/6 font-medium text-foreground"
                    : "text-muted-foreground hover:bg-ink/3 hover:text-foreground",
                )}
              >
                {isActive && <span className="absolute inset-y-1 left-0 w-0.5 bg-ink/50" />}
                <Icon className="size-4 shrink-0" />
                <span className="truncate">{item.label}</span>
              </button>
            );
          })}
        </div>
      ))}
    </nav>
  );
}
