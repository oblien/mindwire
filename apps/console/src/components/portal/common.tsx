import { useState, type ReactNode } from "react";
import { AlertCircle, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";

export const money = (cents: number) =>
  new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: 0,
  }).format(cents / 100);
export const date = (value: string | number | null) =>
  value
    ? new Date(value).toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
        year: "numeric",
      })
    : "Not recorded";
export function PageTitle({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h1 className="break-words text-[32px] font-normal leading-tight tracking-tight sm:text-[40px]">
          {title}
        </h1>
        <p className="mt-3 max-w-2xl text-base leading-relaxed text-muted-foreground">
          {description}
        </p>
      </div>
      {action}
    </header>
  );
}
export function Loading() {
  return (
    <div
      role="status"
      className="flex items-center gap-3 py-10 text-sm text-muted-foreground"
    >
      <Loader2 className="size-4 animate-spin" />
      Loading your account…
    </div>
  );
}
export function ErrorNotice({
  message,
  retry,
  reference,
}: {
  message: string;
  retry?: () => void;
  reference?: string;
}) {
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center gap-3 rounded-2xl bg-destructive/8 p-4 text-sm"
    >
      <AlertCircle className="size-4 shrink-0" />
      <div className="min-w-0 flex-1 break-words">
        <p>{message}</p>
        {reference && <p className="mt-2 text-xs text-muted-foreground">Support reference: {reference}</p>}
      </div>
      {retry && (
        <Button size="sm" variant="outline" onClick={retry}>
          Retry
        </Button>
      )}
    </div>
  );
}
export function ConfirmAction({
  label,
  title,
  description,
  onConfirm,
  destructive = false,
  disabled = false,
  cancelLabel = "Cancel",
}: {
  label: string;
  title: string;
  description: string;
  onConfirm: () => Promise<void>;
  destructive?: boolean;
  disabled?: boolean;
  cancelLabel?: string;
}) {
  const [open, setOpen] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!busy) {
          setOpen(value);
          setError("");
        }
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" disabled={disabled}>
          {label}
        </Button>
      </DialogTrigger>
      <DialogContent className="max-w-[calc(100vw-2rem)] sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {error && <ErrorNotice message={error} />}
        <DialogFooter>
          <Button
            variant="ghost"
            disabled={busy}
            onClick={() => setOpen(false)}
          >
            {cancelLabel}
          </Button>
          <Button
            variant={destructive ? "destructive" : "default"}
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await onConfirm();
                setOpen(false);
              } catch (error) {
                setError((error as Error).message);
              } finally {
                setBusy(false);
              }
            }}
          >
            {busy && <Loader2 className="size-4 animate-spin" />}
            {label}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
