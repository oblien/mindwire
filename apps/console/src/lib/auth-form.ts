import type { AuthMethod, Field } from "@shared/api";

/** Apply daemon defaults and conditions once for rendering, validation, and submission. */
export function resolveAuthForm(
  fields: Field[],
  declarations: NonNullable<AuthMethod["sections"]>,
  drafts: Record<string, string>,
) {
  const all = Object.fromEntries(fields.map((field) => [field.key, (drafts[field.key] ?? field.default ?? "").trim()]));
  const active = fields.filter((field) => !field.visibleWhen || all[field.visibleWhen.key] === field.visibleWhen.equals);
  const values = Object.fromEntries(active.map((field) => [field.key, all[field.key]]));
  const valid = active.every((field) => {
    const required = field.required && !(field.requiredUnless ?? []).some((key) => values[key]);
    if (required && field.type !== "toggle" && !values[field.key]) return false;
    return field.type !== "select" || !values[field.key] || field.options?.some((option) => option.value === values[field.key]);
  });

  const used = new Set<string>();
  const sections: { id: string; title: string; fields: Field[]; help: string }[] = [];
  function addSection(id: string, title: string, members: Field[], help?: string) {
    if (!members.length) return;
    const copy = [help, ...members.flatMap((field) => [
      field.help, field.options?.find((option) => option.value === values[field.key])?.help,
    ])].filter((text): text is string => Boolean(text));
    sections.push({ id, title, fields: members, help: [...new Set(copy)].join("\n") });
  }
  for (const section of declarations) {
    const members = section.fieldKeys.flatMap((key) => {
      const field = active.find((candidate) => candidate.key === key);
      if (!field || used.has(key)) return [];
      used.add(key);
      return [field];
    });
    addSection(section.id, section.title, members, section.help);
  }
  // Older daemons can still present each field with its own label and help.
  for (const field of active.filter((field) => !used.has(field.key))) {
    addSection(`field:${field.key}`, field.label, [field]);
  }
  return { values, valid, sections };
}
