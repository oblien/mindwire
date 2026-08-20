// A reusable list + editor for the scoped, name-keyed markdown stores (prompt templates and subagent
// definitions). Both are the same shape — a list of named docs at a project/user scope, each with raw
// markdown content — so they share this panel and only differ in the CRUD functions passed in.
import { useEffect, useState } from "react";
import { FileText } from "lucide-react";

import { useAsync } from "@/lib/useAsync";
import { Panel, Spinner } from "@/components/common/Panel";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ScopeToggle } from "@/components/common/ScopeToggle";
import {
  TwoPane,
  RailNew,
  RailStatus,
  RailList,
  RailItem,
  FormCard,
  Field,
  FormActions,
  EmptyPane,
} from "@/components/common/config-ui";
import { toast } from "@/components/ui/sonner";
import type { MemoryScope } from "@shared/api";

export interface DocItem {
  name: string;
  subtitle?: string;
}

export interface DocStore {
  title: string;
  description: string;
  namePlaceholder: string;
  bodyPlaceholder: string;
  list: (scope: MemoryScope) => Promise<DocItem[]>;
  read: (name: string, scope: MemoryScope) => Promise<{ content?: string }>;
  write: (name: string, content: string, scope: MemoryScope) => Promise<unknown>;
  remove: (name: string, scope: MemoryScope) => Promise<unknown>;
}

// A distinct sentinel for the "new doc" editor state (no real doc name is empty-after-trim here).
const NEW = Symbol("new");
type Selection = string | typeof NEW | null;

export function DocStorePanel({ store }: { store: DocStore }) {
  const [scope, setScope] = useState<MemoryScope>("project");
  const listQ = useAsync<DocItem[]>(() => store.list(scope), [scope]);

  const [selected, setSelected] = useState<Selection>(null);
  const [name, setName] = useState("");
  const [content, setContent] = useState("");
  const [busy, setBusy] = useState(false);
  const [loadingDoc, setLoadingDoc] = useState(false);

  // Reset the editor when the scope changes.
  useEffect(() => {
    setSelected(null);
    setName("");
    setContent("");
  }, [scope]);

  async function open(docName: string) {
    setSelected(docName);
    setName(docName);
    setContent("");
    setLoadingDoc(true);
    try {
      const doc = await store.read(docName, scope);
      setContent(doc.content ?? "");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not read");
    } finally {
      setLoadingDoc(false);
    }
  }

  function startNew() {
    setSelected(NEW);
    setName("");
    setContent("");
  }

  async function save() {
    const docName = name.trim();
    if (!docName) return toast.error("A name is required.");
    setBusy(true);
    try {
      await store.write(docName, content, scope);
      listQ.reload();
      setSelected(docName);
      toast.success("Saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save");
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (typeof selected !== "string") return;
    setBusy(true);
    try {
      await store.remove(selected, scope);
      listQ.reload();
      setSelected(null);
      setName("");
      setContent("");
      toast.success("Deleted");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete");
    } finally {
      setBusy(false);
    }
  }

  const editing = selected !== null;
  const isNew = selected === NEW;
  const items = listQ.data ?? [];

  return (
    <Panel
      title={store.title}
      description={store.description}
      actions={<ScopeToggle scope={scope} onChange={setScope} />}
    >
      <TwoPane
        rail={
          <>
            <RailNew label="New entry" onClick={startNew} />
            <RailStatus
              loading={listQ.loading}
              error={listQ.error}
              empty={!!listQ.data && items.length === 0}
            />
            {items.length > 0 && (
              <RailList>
                {items.map((d) => (
                  <RailItem
                    key={d.name}
                    active={selected === d.name}
                    onClick={() => open(d.name)}
                    media={<FileText className="size-4 text-ink/70" />}
                    title={d.name}
                    subtitle={d.subtitle}
                  />
                ))}
              </RailList>
            )}
          </>
        }
      >
        {!editing ? (
          <EmptyPane
            icon={<FileText className="size-5" />}
            title="Nothing selected"
            hint="Pick an entry from the list, or create a new one to start editing."
          />
        ) : loadingDoc ? (
          <Spinner />
        ) : (
          <FormCard
            media={<FileText className="size-5 text-ink/70" />}
            title={name || "New entry"}
            subtitle={store.title}
            footer={
              <FormActions saving={busy} onSave={save} onDelete={remove} deletable={!isNew} />
            }
          >
            <Field label="Name" htmlFor="doc-name">
              <Input
                id="doc-name"
                value={name}
                disabled={!isNew}
                onChange={(e) => setName(e.target.value)}
                placeholder={store.namePlaceholder}
                spellCheck={false}
                autoComplete="off"
              />
            </Field>
            <Field label="Content" htmlFor="doc-body">
              <Textarea
                id="doc-body"
                value={content}
                onChange={(e) => setContent(e.target.value)}
                rows={16}
                spellCheck={false}
                className="font-mono text-xs"
                placeholder={store.bodyPlaceholder}
              />
            </Field>
          </FormCard>
        )}
      </TwoPane>
    </Panel>
  );
}
