import { describe, expect, test } from "bun:test";
import type { Field } from "../../../packages/sdk/src/types";
import { resolveAuthForm } from "../src/lib/auth-form";

const fields: Field[] = [
  { key: "auth", label: "Authentication", type: "select", required: true, default: "key", options: [
    { value: "key", label: "API key" }, { value: "token", label: "Token" },
    { value: "identity", label: "Sign-in", help: "Uses the workspace identity." },
  ] },
  { key: "key", label: "API key", type: "secret", required: true, visibleWhen: { key: "auth", equals: "key" } },
  { key: "token", label: "Token", type: "secret", required: true, visibleWhen: { key: "auth", equals: "token" } },
];
const sections = [{ id: "auth", title: "Authentication", fieldKeys: ["auth", "key", "token"] }];

describe("daemon auth form", () => {
  test("resolves defaults and submits only active, declared fields", () => {
    const form = resolveAuthForm(fields, sections, { key: " new-key ", token: "hidden", unknown: "ignored" });
    expect(form.valid).toBe(true);
    expect(form.values).toEqual({ auth: "key", key: "new-key" });
    expect(form.sections[0].fields.map((field) => field.key)).toEqual(["auth", "key"]);
  });

  test("requires the selected credential and exposes selected-option help", () => {
    expect(resolveAuthForm(fields, sections, { auth: "token", key: "hidden" }).valid).toBe(false);
    const token = resolveAuthForm(fields, sections, { auth: "token", key: "hidden", token: "new-token" });
    expect(token.valid).toBe(true);
    expect(token.values).toEqual({ auth: "token", token: "new-token" });
    const identity = resolveAuthForm(fields, sections, { auth: "identity", key: "hidden", token: "hidden" });
    expect(identity.valid).toBe(true);
    expect(identity.values).toEqual({ auth: "identity" });
    expect(identity.sections[0].help).toBe("Uses the workspace identity.");
  });

  test("rejects undeclared and empty choices", () => {
    for (const auth of ["", "unknown"]) {
      expect(resolveAuthForm(fields, sections, { auth, key: "key", token: "token" }).valid).toBe(false);
    }
  });

  test("older forms keep labels and requiredUnless without sections", () => {
    const old: Field[] = [
      { key: "key", label: "API key", type: "secret", required: true, requiredUnless: ["token"] },
      { key: "token", label: "Token", type: "secret", advanced: true },
    ];
    const form = resolveAuthForm(old, [], { token: "legacy-token" });
    expect(form.valid).toBe(true);
    expect(form.sections.map((section) => section.title)).toEqual(["API key", "Token"]);
    expect(resolveAuthForm(old, [], {}).valid).toBe(false);
  });
});
