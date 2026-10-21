import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ArrowLeft, Cpu, HardDrive, Loader2, MemoryStick, RefreshCw } from "lucide-react";
import { request } from "@/lib/api";
import { useCloud, computerState } from "@/lib/cloud";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  ConfirmAction,
  date,
  ErrorNotice,
  Loading,
  money,
  PageTitle,
} from "./common";

export function ComputerDetail() {
  const { id } = useParams(),
    cloud = useCloud(),
    navigate = useNavigate(),
    computer = cloud.computers.find((c) => c.id === id);
  const [status, setStatus] = useState<string>(),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const workspace = computer?.workspaceId;
  const cloudAction = cloud.action;
  const refresh = useCallback(async () => {
    try {
      setError("");
      if (!workspace) {
        if (id) await cloudAction(id, "reconcile");
        return;
      }
      const data = await request<{
        workspace?: {
          status?: string;
          info?: { is_running?: boolean; status?: string };
        };
        status?: string;
        info?: { is_running?: boolean; status?: string };
      }>(`/api/cloud/provider/workspace/${encodeURIComponent(workspace)}`);
      const live = data.workspace ?? data;
      setStatus(
        live.info?.is_running === true
          ? "Running"
          : live.info?.is_running === false
            ? "Stopped"
            : (live.info?.status ?? live.status ?? "Unknown"),
      );
      setError("");
    } catch (error) {
      setError((error as Error).message);
    }
  }, [workspace, id, cloudAction]);
  useEffect(() => {
    if (workspace) void refresh();
  }, [refresh, workspace]);
  async function power(operation: "start" | "stop" | "restart") {
    if (!workspace || busy) return;
    setBusy(true);
    setError("");
    try {
      await request(
        `/api/cloud/provider/workspace/${encodeURIComponent(workspace)}/${operation}`,
        "POST",
        {},
      );
      await refresh();
    } catch (error) {
      setError((error as Error).message);
    } finally {
      setBusy(false);
    }
  }
  if (!cloud.loaded) return cloud.error ? null : <Loading />;
  if (!computer)
    return (
      <div className="space-y-4">
        <PageTitle
          title="Computer not found"
          description="This computer is no longer listed in your account."
        />
        <Button asChild variant="outline">
          <Link to="/computers">Back to computers</Link>
        </Button>
      </div>
    );
  const pending = ["draft", "checkout_pending", "payment_pending"].includes(
    computer.state,
  );
  return (
    <div className="space-y-7">
      <Button asChild size="sm" variant="ghost"><Link to="/computers"><ArrowLeft />All computers</Link></Button>
      <PageTitle
        title={computer.name}
        description={`${computer.plan.name} · ${computer.image} · ${computerState(computer)}`}
        action={
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => void refresh()}
          >
            <RefreshCw />
            Refresh
          </Button>
        }
      />
      {error && error !== cloud.error && <ErrorNotice message={error} retry={() => void refresh()} />}
      {computer.error && computer.error !== cloud.error && computer.error !== error && <ErrorNotice message={computer.error} />}
      <div className="grid gap-4 sm:grid-cols-3">
        {[
          {
            icon: Cpu,
            label: "Shared CPU",
            value: `${computer.plan.vcpus} vCPU`,
          },
          {
            icon: MemoryStick,
            label: "Memory",
            value: `${computer.plan.memoryMb / 1024} GB`,
          },
          {
            icon: HardDrive,
            label: "Included storage",
            value: `${computer.plan.diskGb} GB`,
          },
        ].map((item) => (
          <Card key={item.label}>
            <CardContent className="space-y-4 p-5">
              <p className="flex items-center gap-2 text-xs text-muted-foreground">
                <item.icon className="size-4" />
                {item.label}
              </p>
              <p className="text-3xl font-medium">{item.value}</p>
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardContent className="space-y-5 p-6">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-medium">Computer</h2>
              <span className="rounded-full bg-[var(--mw-mint-soft)] px-3 py-1.5 text-xs capitalize text-[var(--mw-mint)]">
                {status ?? computerState(computer)}
              </span>
            </div>
            <p className="text-sm leading-relaxed text-muted-foreground">
              Connect from the Mindwire app to use your desktop, files and
              development tools.
            </p>
            {workspace && (
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  disabled={busy || status === "Running"}
                  onClick={() => void power("start")}
                >
                  {busy && <Loader2 className="animate-spin" />}Start
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={() => void power("restart")}
                >
                  Restart
                </Button>
                <ConfirmAction
                  label="Stop"
                  title="Stop this computer?"
                  description="Running processes will stop. Your stored files and subscription are preserved."
                  disabled={busy || status === "Stopped"}
                  onConfirm={() => power("stop")}
                />
              </div>
            )}
            <a
              className="inline-block text-xs text-muted-foreground underline-offset-4 hover:underline"
              href="https://mindwire.sh/get-started"
              target="_blank"
              rel="noreferrer"
            >
              Get the Mindwire app
            </a>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="space-y-5 p-6">
            <h2 className="text-sm font-medium">Monthly subscription</h2>
            <p className="text-3xl font-medium">
              {money(computer.plan.monthlyCents)}
              <span className="text-sm font-normal text-muted-foreground">
                {" "}
                /month
              </span>
            </p>
            <p className="text-sm text-muted-foreground">
              {computer.subscription?.paidThrough
                ? `${computer.subscription.cancelAtPeriodEnd ? "Paid through" : "Next renewal"} ${date(computer.subscription.paidThrough)}`
                : computerState(computer)}
            </p>
            {pending ? (
              <Button
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  setError("");
                  try {
                    await cloud.action(computer.id, "checkout");
                  } catch (error) {
                    setError((error as Error).message);
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                {busy && <Loader2 className="size-4 animate-spin" />}
                Continue checkout
              </Button>
            ) : (
              <Button
                variant="outline"
                onClick={async () => {
                  try {
                    await cloud.action(computer.id, "billing-portal");
                  } catch (error) {
                    setError((error as Error).message);
                  }
                }}
              >
                Manage billing
              </Button>
            )}
            <Link
              className="ml-3 text-xs text-muted-foreground hover:underline"
              to="/billing"
            >
              Plans & billing
            </Link>
          </CardContent>
        </Card>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-4 border-t border-border pt-6">
        <div>
          <h2 className="text-sm font-medium">
            {pending ? "Cancel this purchase" : "Delete this computer"}
          </h2>
          <p className="mt-1 max-w-lg text-xs leading-relaxed text-muted-foreground">
            {pending
              ? "Close an unpaid checkout to choose another computer or plan."
              : "Permanently remove the computer and stop its next renewal. Storage retention follows your subscription terms."}
          </p>
        </div>
        <ConfirmAction
          destructive
          label={pending ? "Cancel purchase" : "Delete computer"}
          title={pending ? "Cancel this purchase?" : `Delete ${computer.name}?`}
          description={
            pending
              ? "An already completed payment cannot be discarded. Your other computers are unaffected."
              : "The computer will be removed. Running work will stop, and this action cannot be undone. Your other computers are unaffected."
          }
          onConfirm={async () => {
            if (pending) await cloud.action(computer.id, "checkout/cancel");
            else {
              if (!workspace)
                throw new Error(
                  "Computer setup must finish before deletion. You can stop renewal from Billing.",
                );
              await request(
                `/api/cloud/provider/workspace/${encodeURIComponent(workspace)}`,
                "DELETE",
              );
              await cloud.reload();
            }
            navigate("/computers");
          }}
        />
      </div>
    </div>
  );
}
