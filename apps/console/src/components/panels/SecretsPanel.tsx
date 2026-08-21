// Console-owned connection credentials. The browser can see which entries exist, but never read them
// back: saving a value rotates it in the server-side encrypted vault.
import { useState } from "react";
import { KeyRound, RefreshCw, ShieldCheck } from "lucide-react";

import { api } from "@/lib/api";
import { useAsync } from "@/lib/useAsync";
import { Panel, Section, Spinner, ErrorNote, EmptyState } from "@/components/common/Panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "@/components/ui/sonner";
import type { SecretMetadata } from "@shared/api";

const labels: Record<SecretMetadata["kind"], string> = {
  "oblien-client-id": "Oblien client ID",
  "oblien-client-secret": "Oblien client secret",
  "runtime-token": "Runtime bearer token",
  "ssh-private-key": "SSH private key",
  "ssh-password": "SSH password",
  "ssh-passphrase": "SSH key passphrase",
  "runtime-config": "Encrypted runtime configuration",
};

export function SecretsPanel() {
  const q = useAsync<SecretMetadata[]>(() => api.secrets());
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [saving, setSaving] = useState(false);

  async function saveOblien() {
    if (!clientId.trim() || !clientSecret.trim()) {
      toast.error("Enter both values so MindWire can verify the new Oblien connection before saving it.");
      return;
    }
    setSaving(true);
    try {
      await api.connectOblien({ clientId: clientId.trim(), clientSecret: clientSecret.trim() });
      setClientId("");
      setClientSecret("");
      await q.reload();
      toast.success("Saved to the encrypted vault");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Could not save secret");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Panel
      title="Secrets"
      description="Credentials are encrypted at rest, scoped to your Console account, and write-only."
      actions={<Button size="sm" variant="outline" onClick={q.reload} disabled={q.loading}><RefreshCw className="size-3.5" />Refresh</Button>}
    >
      <div className="mb-6 flex items-start gap-3 border border-border bg-muted/20 px-4 py-3 text-sm text-muted-foreground">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-foreground" />
        <p>Secret values are never shown again after saving. They are decrypted only inside the server for the provider call that needs them.</p>
      </div>
      {q.loading && <Spinner label="Loading vault metadata…" />}
      {q.error && <ErrorNote message={q.error} />}
      <Section title="Stored credentials" description="Metadata only — no value is returned to this page.">
        {(q.data?.length ?? 0) === 0 ? <EmptyState>No saved credentials yet.</EmptyState> : (
          <div className="border border-border">
            {q.data?.map((secret) => (
              <div key={secret.name} className="flex items-center gap-3 border-b border-border px-4 py-3 last:border-0">
                <KeyRound className="size-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1"><p className="text-sm font-medium">{labels[secret.kind]}</p><p className="truncate font-mono text-xs text-muted-foreground" title={secret.name}>{secret.name} · updated {new Date(secret.updatedAt).toLocaleString()}</p></div>
                <span className="text-xs text-muted-foreground">Encrypted</span>
              </div>
            ))}
          </div>
        )}
      </Section>
      <Section title="Rotate Oblien credentials" description="Both values are required so the new pair is verified before it atomically replaces the encrypted vault entry.">
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5"><Label htmlFor="vault-oblien-id">Client ID</Label><Input id="vault-oblien-id" autoComplete="off" value={clientId} onChange={(e) => setClientId(e.target.value)} placeholder="oblien_…" /></div>
          <div className="space-y-1.5"><Label htmlFor="vault-oblien-secret">Client secret</Label><Input id="vault-oblien-secret" type="password" autoComplete="new-password" value={clientSecret} onChange={(e) => setClientSecret(e.target.value)} placeholder="Required to rotate" /></div>
        </div>
        <Button className="mt-4" onClick={saveOblien} disabled={saving}>{saving ? "Saving…" : "Save encrypted credentials"}</Button>
      </Section>
    </Panel>
  );
}
