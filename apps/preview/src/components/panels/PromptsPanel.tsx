// The Prompts surface — saved prompt templates (Claude slash-commands, Codex saved prompts). Thin
// binding of the shared DocStorePanel to the `/api/prompts` store.
import { api } from "@/lib/api";
import { DocStorePanel, type DocStore } from "@/components/panels/DocStorePanel";

const store: DocStore = {
  title: "Prompts",
  description: "Saved prompt templates the agent can invoke by name.",
  namePlaceholder: "review-pr",
  bodyPlaceholder: "Prompt body… use $ARGUMENTS for passthrough.",
  list: async (scope) => (await api.prompts({ scope })).map((p) => ({ name: p.name })),
  read: (name, scope) => api.prompt(name, { scope }),
  write: (name, content, scope) => api.setPrompt(name, content, { scope }),
  remove: (name, scope) => api.deletePrompt(name, { scope }),
};

export function PromptsPanel() {
  return <DocStorePanel store={store} />;
}
