// Where an agent's model lives in its settings schema. Both the Models panel and the Providers catalog
// browser write the active model to the sticky config, and both must target the SAME native key — the
// field the daemon marks canonical `model` (falling back to a literal `model` key). Factoring the lookup
// here keeps the two surfaces in lockstep and off any per-harness hardcoding.
import type { SettingsSchema } from "@shared/api";

/** The native config key that holds the model (the field whose canon is "model"), or "model" if none. */
export function modelFieldKey(schema: SettingsSchema | undefined): string {
  for (const s of schema?.sections ?? []) {
    for (const f of s.fields) {
      if (f.canon === "model" || f.key === "model") return f.key;
    }
  }
  return "model";
}

/** Whether the agent exposes a model field at all — the gate for showing a "set this model" affordance. */
export function hasModelField(schema: SettingsSchema | undefined): boolean {
  for (const s of schema?.sections ?? []) {
    for (const f of s.fields) {
      if (f.canon === "model" || f.key === "model") return true;
    }
  }
  return false;
}
