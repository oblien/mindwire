// Shared scaffolding for every center-surface panel: a consistent header (title + optional
// description + right-aligned actions) over a scrollable body, plus the small status atoms
// (loading / error / empty) the panels lean on so they stay declarative and visually uniform.
import type { ReactNode } from "react";
import { Loader2, AlertTriangle } from "lucide-react";

import { cn } from "@/lib/utils";
import { ScrollArea } from "@/components/ui/scroll-area";

/** Every top-of-surface header — the panels, the daemon page, and the chat rail — shares this height and
 *  chrome so their bottom borders line up across the whole shell (one continuous line under the top nav),
 *  and the chat body never shifts as you navigate between surfaces. Add the surface's own horizontal
 *  padding (`px-6` for center panels, `px-3` for the narrow chat rail) via `cn(SURFACE_HEADER, …)`. */
export const SURFACE_HEADER = "flex h-16 shrink-0 items-center gap-3 border-b border-border";

export function Panel({
  title,
  description,
  actions,
  children,
  contentClassName,
  fill,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
  /** Override the body container (default `mx-auto max-w-4xl px-6 py-6`) — e.g. full-bleed two-pane surfaces. */
  contentClassName?: string;
  /** Fill the surface height instead of page-scrolling: the body becomes a flex column that owns its own
   *  height, so inner regions (e.g. a `TwoPane` rail) scroll independently. Use for master-detail panels. */
  fill?: boolean;
}) {
  return (
    <div className="flex h-full flex-col">
      <div className={cn(SURFACE_HEADER, "px-6")}>
        <div className="min-w-0">
          <h1 className="text-sm font-semibold tracking-tight">{title}</h1>
          {description && <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="ml-auto flex shrink-0 items-center gap-2">{actions}</div>}
      </div>
      {fill ? (
        <div className={cn("flex min-h-0 flex-1 flex-col", contentClassName)}>{children}</div>
      ) : (
        <ScrollArea className="flex-1">
          <div className={cn("mx-auto max-w-4xl px-6 py-6", contentClassName)}>{children}</div>
        </ScrollArea>
      )}
    </div>
  );
}

/** A titled group of controls inside a panel body. */
export function Section({
  title,
  description,
  children,
  className,
}: {
  title?: string;
  description?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("mb-8 last:mb-0", className)}>
      {title && <h2 className="mb-1 text-xs font-semibold uppercase tracking-wide">{title}</h2>}
      {description && <p className="mb-3 text-xs text-muted-foreground">{description}</p>}
      {!description && title && <div className="mb-3" />}
      {children}
    </section>
  );
}

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 py-10 text-sm text-muted-foreground">
      <Loader2 className="size-4 animate-spin" />
      {label ?? "Loading…"}
    </div>
  );
}

export function ErrorNote({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-2 border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <span className="min-w-0 break-words">{message}</span>
    </div>
  );
}

export function EmptyState({ children }: { children: ReactNode }) {
  return (
    <div className="border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
      {children}
    </div>
  );
}

/** A labeled key/value row for read-only detail grids. */
export function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline gap-3 border-b border-border py-2 text-sm last:border-0">
      <span className="w-40 shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}
