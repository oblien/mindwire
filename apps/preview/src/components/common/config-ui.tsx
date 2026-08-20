// Shared building blocks for the two-pane config surfaces (Providers, MCP, Prompts, Subagents). They all
// share the same shape — a left rail that lists the configured entries with a "New" affordance, and a
// right pane that is either an empty prompt or a card-framed editor with grouped fields and a footer of
// actions. Factoring that shape here keeps every config surface visually identical and the panels down to
// their actual data wiring. Monochrome ink palette, sharp corners, currentColor icons — same rules as the
// rest of the console.
import type { ReactNode } from "react";
import { Loader2, Plus, Save, Trash2 } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Spinner, ErrorNote } from "@/components/common/Panel";

/* ------------------------------------------------------------------ layout ---------- */

/** The list-rail + editor grid every config surface sits in. Rail is fixed-width, editor takes the rest.
 *  With `fill`, the grid claims its parent's height and each column scrolls on its own (master-detail) —
 *  the rail scrolls internally while the detail pane stays in view; without it, the page scrolls as one. */
export function TwoPane({
  rail,
  children,
  fill,
}: {
  rail: ReactNode;
  children: ReactNode;
  fill?: boolean;
}) {
  if (fill) {
    return (
      <div className="grid min-h-0 flex-1 gap-6 md:grid-cols-[15rem_minmax(0,1fr)]">
        <div className="flex min-h-0 flex-col gap-2">{rail}</div>
        <div className="min-h-0 min-w-0 overflow-y-auto">{children}</div>
      </div>
    );
  }
  return (
    <div className="grid items-start gap-6 md:grid-cols-[15rem_minmax(0,1fr)]">
      <div className="space-y-2">{rail}</div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

/* -------------------------------------------------------------------- rail ---------- */

/** The full-width "add an entry" button that heads every rail. */
export function RailNew({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center justify-center gap-1.5 border border-dashed border-ink/25 py-2 text-xs font-medium text-muted-foreground transition-colors hover:border-ink/40 hover:bg-ink/[0.03] hover:text-foreground"
    >
      <Plus className="size-3.5" />
      {label}
    </button>
  );
}

/** Loading / error / empty status for the rail, so panels don't repeat the ternaries. */
export function RailStatus({
  loading,
  error,
  empty,
  emptyText = "Nothing here yet.",
}: {
  loading: boolean;
  error?: string | null;
  empty: boolean;
  emptyText?: string;
}) {
  if (loading) return <Spinner />;
  if (error) return <ErrorNote message={error} />;
  if (empty)
    return <p className="px-1 py-6 text-center text-xs text-muted-foreground">{emptyText}</p>;
  return null;
}

/** The bordered container the rail items divide. Pass `className` to make it scroll (e.g. a long list in a
 *  `fill` TwoPane: `flex-1 min-h-0 overflow-y-auto`). */
export function RailList({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("divide-y divide-border border border-border", className)}>{children}</div>
  );
}

/** One selectable rail entry: a media tile (logo/icon), a title, an optional subtitle, optional trailing. */
export function RailItem({
  active,
  onClick,
  media,
  title,
  subtitle,
  trailing,
}: {
  active: boolean;
  onClick: () => void;
  media: ReactNode;
  title: string;
  subtitle?: string;
  trailing?: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2.5 px-2.5 py-2 text-left transition-colors hover:bg-accent",
        active && "bg-accent",
      )}
    >
      <span className="flex size-7 shrink-0 items-center justify-center border border-ink/15 bg-ink/[0.03]">
        {media}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-xs font-medium">{title}</span>
        {subtitle && (
          <span className="block truncate text-[11px] text-muted-foreground">{subtitle}</span>
        )}
      </span>
      {trailing && <span className="shrink-0">{trailing}</span>}
    </button>
  );
}

/* ------------------------------------------------------------------- editor --------- */

/** The card the editor lives in: a header (media tile + title/subtitle), the fields, and a footer. */
export function FormCard({
  media,
  title,
  subtitle,
  children,
  footer,
}: {
  media: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <div className="border border-border bg-card">
      <div className="flex items-center gap-3 border-b border-border px-5 py-4">
        <span className="flex size-9 shrink-0 items-center justify-center border border-ink/15 bg-ink/[0.03]">
          {media}
        </span>
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold leading-tight">{title}</div>
          {subtitle && (
            <div className="mt-0.5 truncate text-xs text-muted-foreground">{subtitle}</div>
          )}
        </div>
      </div>
      <div className="space-y-5 px-5 py-5">{children}</div>
      {footer && (
        <div className="flex items-center gap-2 border-t border-border px-5 py-3.5">{footer}</div>
      )}
    </div>
  );
}

/** A titled cluster of fields inside the card, with a faint rule under the label. */
export function FieldGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="space-y-4">
      <div className="border-b border-border pb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
        {title}
      </div>
      {children}
    </div>
  );
}

/** Two fields side by side on wide viewports, stacked on narrow. */
export function TwoCol({ children }: { children: ReactNode }) {
  return <div className="grid gap-4 sm:grid-cols-2">{children}</div>;
}

/** A labeled control with an optional hint below — the atom every form field is built from. */
export function Field({
  label,
  htmlFor,
  hint,
  children,
}: {
  label: string;
  htmlFor?: string;
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={htmlFor} className="text-xs font-medium text-muted-foreground">
        {label}
      </Label>
      {children}
      {hint && <p className="text-[11px] leading-relaxed text-muted-foreground">{hint}</p>}
    </div>
  );
}

/** A generic monochrome segmented control (transport toggles, mode switches, …). */
export function SegmentedControl<T extends string>({
  value,
  options,
  onChange,
  className,
}: {
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
  className?: string;
}) {
  return (
    <div className={cn("inline-flex border border-border", className)}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          className={cn(
            "px-3 py-1.5 text-xs transition-colors",
            value === o.value
              ? "bg-foreground text-background"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** The standard Save + (optional) Delete footer, with a busy spinner on Save. */
export function FormActions({
  saving,
  onSave,
  onDelete,
  deletable,
  saveLabel = "Save",
  deleteLabel = "Delete",
}: {
  saving: boolean;
  onSave: () => void;
  onDelete?: () => void;
  deletable?: boolean;
  saveLabel?: string;
  deleteLabel?: string;
}) {
  return (
    <>
      <Button size="sm" onClick={onSave} disabled={saving}>
        {saving ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
        {saveLabel}
      </Button>
      {deletable && onDelete && (
        <Button
          size="sm"
          variant="ghost"
          onClick={onDelete}
          disabled={saving}
          className="text-muted-foreground hover:text-destructive"
        >
          <Trash2 className="size-4" />
          {deleteLabel}
        </Button>
      )}
    </>
  );
}

/** The right-pane placeholder before an entry is selected — an icon over a title and a hint. */
export function EmptyPane({
  icon,
  title,
  hint,
}: {
  icon: ReactNode;
  title: string;
  hint: string;
}) {
  return (
    <div className="flex min-h-[18rem] flex-col items-center justify-center border border-dashed border-border px-6 py-12 text-center">
      <span className="mb-3 flex size-10 items-center justify-center border border-ink/15 bg-ink/[0.03] text-muted-foreground">
        {icon}
      </span>
      <p className="text-sm font-medium">{title}</p>
      <p className="mt-1 max-w-xs text-xs text-muted-foreground">{hint}</p>
    </div>
  );
}
