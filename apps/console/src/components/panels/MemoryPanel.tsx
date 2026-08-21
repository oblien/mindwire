// The Memory surface — the agent's persistent memory file (Claude's CLAUDE.md, Codex's AGENTS.md) at
// either the project or user scope. Read → edit → save/delete against `GET/PUT/DELETE /api/memory`.
import { useEffect, useState } from "react";
import { Save, Trash2, Loader2 } from "lucide-react";

import { api } from "@/lib/api";
import { useAsync } from "@/lib/useAsync";
import { Panel, Spinner, ErrorNote } from "@/components/common/Panel";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { ScopeToggle } from "@/components/common/ScopeToggle";
import { toast } from "@/components/ui/sonner";
import type { MemoryDoc, MemoryScope } from "@shared/api";

export function MemoryPanel() {
  const [scope, setScope] = useState<MemoryScope>("project");
  const q = useAsync<MemoryDoc>(() => api.memory({ scope }), [scope]);

  const [content, setContent] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (q.data) setContent(q.data.content);
  }, [q.data]);

  async function save() {
    setBusy(true);
    try {
      await api.setMemory(content, { scope });
      q.reload();
      toast.success("Memory saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save memory");
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await api.deleteMemory({ scope });
      setContent("");
      q.reload();
      toast.success("Memory deleted");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete memory");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel
      title="Memory"
      description="Instructions the agent loads on every run."
      actions={
        <>
          <Button size="sm" variant="ghost" onClick={remove} disabled={busy || !q.data?.exists}>
            <Trash2 className="size-3.5" />
            Delete
          </Button>
          <Button size="sm" onClick={save} disabled={busy}>
            {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            Save
          </Button>
        </>
      }
    >
      <div className="mb-4 flex items-center justify-between gap-3">
        <ScopeToggle scope={scope} onChange={setScope} />
        {q.data && (
          <Badge variant={q.data.exists ? "outline" : "secondary"}>
            {q.data.exists ? "exists" : "new"}
          </Badge>
        )}
      </div>

      {q.loading && <Spinner />}
      {q.error && <ErrorNote message={q.error} />}
      {q.data && (
        <>
          <p className="mb-2 break-all font-mono text-xs text-muted-foreground">{q.data.path}</p>
          <Textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            rows={18}
            spellCheck={false}
            className="font-mono text-xs"
            placeholder="# Project instructions…"
          />
        </>
      )}
    </Panel>
  );
}
