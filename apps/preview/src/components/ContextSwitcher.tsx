// The app's runtime selector: which daemon (location) everything below is scoped to. It used to also
// carry the agent (adapter) picker, but agent selection now lives in the sidebar's Agent-group header
// (see AgentSwitcher) — right above the surfaces it configures — so this control is daemon-only. It
// appears in the top nav on agent-scoped surfaces (labelled RUNTIME); the trigger reads the daemon's
// status dot + label straight from the live context so it stays compact and always truthful.
import { useApp } from "@/lib/app-context";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { toast } from "@/components/ui/sonner";
import { PROVIDER_ICON, STATE_TONE } from "@/components/fleet/daemon-state";

/** The dot tone for the active daemon: ready-but-unreachable shows amber, not a premature green. */
function useDaemonDot(): string {
  const { activeDaemon, health } = useApp();
  if (!activeDaemon) return "bg-ink/30";
  if (activeDaemon.state === "ready" && !health) return "bg-amber-500";
  return STATE_TONE[activeDaemon.state];
}

export function ContextSwitcher({
  className,
  disabled,
}: {
  className?: string;
  /** Lock the control (e.g. while a turn is streaming — you can't retarget an in-flight turn). */
  disabled?: boolean;
}) {
  const { fleet, activeDaemon, applyFleet } = useApp();

  const daemons = fleet?.daemons ?? [];
  const dot = useDaemonDot();

  async function switchDaemon(id: string) {
    if (id === activeDaemon?.id) return;
    try {
      applyFleet(await api.activateDaemon(id));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not switch runtime");
    }
  }

  const trigger =
    "h-full w-auto justify-start gap-2 border-0 bg-transparent px-3 text-xs shadow-none " +
    "hover:bg-accent focus-visible:ring-0 [&>svg]:size-3.5 [&>svg]:opacity-45";

  return (
    <div className={cn("inline-flex h-9 items-center border border-border", className)}>
      <Select
        value={activeDaemon?.id ?? ""}
        onValueChange={switchDaemon}
        disabled={disabled || daemons.length === 0}
      >
        <SelectTrigger className={trigger} aria-label="Active runtime">
          <span className={cn("size-1.5 shrink-0 rounded-full", dot)} />
          <span className="max-w-[13rem] truncate">{activeDaemon?.label ?? "No runtime"}</span>
        </SelectTrigger>
        <SelectContent align="start">
          {daemons.map((d) => {
            const Icon = PROVIDER_ICON[d.provider];
            return (
              <SelectItem key={d.id} value={d.id}>
                <span className="flex items-center gap-2">
                  <span className={cn("size-1.5 shrink-0 rounded-full", STATE_TONE[d.state])} />
                  <Icon className="size-3.5 opacity-70" />
                  <span className="truncate">{d.label}</span>
                </span>
              </SelectItem>
            );
          })}
        </SelectContent>
      </Select>
    </div>
  );
}
