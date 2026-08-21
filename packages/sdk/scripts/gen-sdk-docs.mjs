#!/usr/bin/env node
// Generates the TypeScript SDK reference by running TypeDoc (markdown plugin, MDX flavor) over
// src/index.ts, then post-processing into the web app's docs so it renders through Fumadocs:
// inject frontmatter, drop the duplicate H1, rewrite cross-links to trailing-slash routes, and write
// fumadocs meta.json for the sidebar. Property/parameter groups use HTML tables so generic types
// (`Promise<Run>`) and object literals stay MDX-safe. Output is gitignored (regenerated at build time).

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const sdkDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const OUT = path.resolve(sdkDir, "../../apps/web/content/docs/reference/sdk-ts");
const PUBLIC_PATH = "/docs/reference/sdk-ts/";

const typedoc = path.join(sdkDir, "node_modules/.bin/typedoc");
const bin = fs.existsSync(typedoc) ? typedoc : "typedoc";

fs.rmSync(OUT, { recursive: true, force: true });

execFileSync(
  bin,
  [
    "--plugin", "typedoc-plugin-markdown",
    "--entryPoints", "src/index.ts",
    "--tsconfig", "tsconfig.json",
    "--out", OUT,
    "--readme", "none",
    "--hidePageHeader",
    "--hideBreadcrumbs",
    "--disableSources",
    "--fileExtension", ".mdx",
    // HTML tables keep `<`/`{` in type cells out of the MDX parser's way.
    "--interfacePropertiesFormat", "htmlTable",
    "--classPropertiesFormat", "htmlTable",
    "--typeDeclarationFormat", "htmlTable",
    "--parametersFormat", "htmlTable",
    "--typeAliasPropertiesFormat", "htmlTable",
    "--enumMembersFormat", "htmlTable",
    "--propertyMembersFormat", "htmlTable",
    "--publicPath", PUBLIC_PATH,
    "--entryFileName", "index",
  ],
  { cwd: sdkDir, stdio: ["ignore", "inherit", "inherit"] },
);

// ---- post-process every generated markdown file --------------------------

const KIND = /^#\s+(?:(?:Class|Interface|Function|Type Alias|Enumeration|Variable|Namespace):\s*)?(.+?)(?:\(\))?\s*$/;

function walk(dir) {
  const out = [];
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...walk(p));
    else if (e.name.endsWith(".mdx")) out.push(p);
  }
  return out;
}

for (const file of walk(OUT)) {
  let src = fs.readFileSync(file, "utf8");

  // Rewrite absolute `/docs/reference/sdk-ts/...mdx(#anchor)` → trailing-slash route `.../ (#anchor)`.
  src = src.replace(
    /\]\((\/docs\/reference\/sdk-ts\/[^)\s]+?)\.mdx(#[^)]*)?\)/g,
    (_m, p, hash) => `](${p}/${hash ?? ""})`,
  );

  // First H1 → frontmatter title; drop it (Fumadocs renders the title from frontmatter).
  const lines = src.split("\n");
  let title = path.basename(file, ".mdx");
  const isIndex = path.basename(file) === "index.mdx";
  const h1 = lines.findIndex((l) => l.startsWith("# "));
  if (h1 !== -1) {
    const m = KIND.exec(lines[h1]);
    if (m) title = m[1].trim();
    lines.splice(h1, 1);
    while (lines[h1] === "") lines.splice(h1, 1); // trim blank lines left behind
  }
  if (isIndex) title = "TypeScript SDK";

  const body = lines.join("\n").trimStart();
  fs.writeFileSync(file, `---\ntitle: "${title}"\n---\n\n${body}`);
}

// ---- fumadocs meta.json for the sidebar ----------------------------------

const write = (p, obj) => fs.writeFileSync(p, JSON.stringify(obj, null, 2) + "\n");

write(path.join(OUT, "meta.json"), {
  title: "TypeScript SDK",
  pages: ["index", "classes", "functions", "interfaces", "type-aliases"],
});

const folderTitles = {
  classes: "Classes",
  functions: "Functions",
  interfaces: "Interfaces",
  "type-aliases": "Type aliases",
};
for (const [dir, title] of Object.entries(folderTitles)) {
  const d = path.join(OUT, dir);
  if (fs.existsSync(d)) write(path.join(d, "meta.json"), { title });
}

const count = walk(OUT).length;
console.log(`gen-sdk-docs: wrote ${count} TypeScript SDK reference page(s) to content/docs/reference/sdk-ts`);
