// The Subagents surface — persistent subagent definition files (Claude `.claude/agents/*.md`). Thin
// binding of the shared DocStorePanel to the `/api/subagents` store; the list subtitle surfaces the
// parsed frontmatter description when present.
import { api } from "@/lib/api";
import { DocStorePanel, type DocStore } from "@/components/panels/DocStorePanel";

const store: DocStore = {
  title: "Subagents",
  description: "Persistent subagent definitions the agent can delegate to.",
  namePlaceholder: "researcher",
  bodyPlaceholder: "---\nname: researcher\ndescription: …\n---\nSystem prompt…",
  list: async (scope) =>
    (await api.subagents({ scope })).map((s) => ({ name: s.name, subtitle: s.meta?.description })),
  read: (name, scope) => api.subagent(name, { scope }),
  write: (name, content, scope) => api.setSubagent(name, content, { scope }),
  remove: (name, scope) => api.deleteSubagent(name, { scope }),
};

export function SubagentsPanel() {
  return <DocStorePanel store={store} />;
}
