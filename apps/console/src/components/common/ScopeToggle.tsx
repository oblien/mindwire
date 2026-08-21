// The project/user scope selector shared by the memory, prompts, and subagent surfaces — `project` is
// a working directory, `user` is the agent's home config dir. A plain segmented control (no native
// radio) so it matches the rest of the monochrome chrome.
import { cn } from "@/lib/utils";
import type { MemoryScope } from "@shared/api";

const SCOPES: { key: MemoryScope; label: string }[] = [
  { key: "project", label: "Project" },
  { key: "user", label: "User" },
];

export function ScopeToggle({
  scope,
  onChange,
  scopes,
}: {
  scope: MemoryScope;
  onChange: (scope: MemoryScope) => void;
  // Restrict the rendered buttons to a supported subset (e.g. providers are user-only on opencode/Codex).
  // Defaults to both scopes when omitted.
  scopes?: MemoryScope[];
}) {
  const shown = scopes ? SCOPES.filter((s) => scopes.includes(s.key)) : SCOPES;
  return (
    <div className="inline-flex border border-border">
      {shown.map((s) => (
        <button
          key={s.key}
          type="button"
          onClick={() => onChange(s.key)}
          className={cn(
            "px-3 py-1.5 text-xs transition-colors",
            scope === s.key
              ? "bg-foreground text-background"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {s.label}
        </button>
      ))}
    </div>
  );
}
