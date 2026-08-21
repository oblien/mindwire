// The Doctor surface — the daemon's environment diagnostics (`GET /doctor`) as a checklist. Read-only;
// a Refresh re-runs the probe.
import { RefreshCw, CheckCircle2, AlertTriangle, XCircle } from "lucide-react";

import { api } from "@/lib/api";
import { useAsync } from "@/lib/useAsync";
import { Panel, Spinner, ErrorNote, EmptyState } from "@/components/common/Panel";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import type { DoctorReport, Check } from "@shared/api";

function statusIcon(status: Check["status"]) {
  if (status === "ok") return <CheckCircle2 className="size-4 text-foreground" />;
  if (status === "warn") return <AlertTriangle className="size-4 text-muted-foreground" />;
  return <XCircle className="size-4 text-destructive" />;
}

export function DoctorPanel() {
  const q = useAsync<DoctorReport>(() => api.doctor());

  return (
    <Panel
      title="Doctor"
      description="Environment diagnostics from the runtime."
      actions={
        <Button size="sm" variant="outline" onClick={q.reload} disabled={q.loading}>
          <RefreshCw className="size-3.5" />
          Refresh
        </Button>
      }
    >
      {q.loading && <Spinner />}
      {q.error && <ErrorNote message={q.error} />}
      {q.data && (
        <>
          <div className="mb-4 flex items-center gap-2">
            <Badge variant={q.data.ok ? "default" : "destructive"}>
              {q.data.ok ? "All good" : "Attention needed"}
            </Badge>
          </div>
          {q.data.checks.length === 0 ? (
            <EmptyState>No checks reported.</EmptyState>
          ) : (
            <div className="border border-border">
              {q.data.checks.map((c, i) => (
                <div
                  key={i}
                  className="flex items-start gap-3 border-b border-border px-4 py-3 last:border-0"
                >
                  <span className="mt-0.5">{statusIcon(c.status)}</span>
                  <div className="min-w-0">
                    <p className="text-sm font-medium">{c.name}</p>
                    {c.detail && (
                      <p className="mt-0.5 break-words text-xs text-muted-foreground">{c.detail}</p>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </Panel>
  );
}
