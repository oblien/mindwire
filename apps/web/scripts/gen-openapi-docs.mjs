// Generates the REST/SSE reference from the daemon-owned openapi.json into content/docs/reference/rest,
// one MDX page per operation grouped by tag. fumadocs-openapi emits pages that render through <APIPage>
// (see components/api-page.tsx) with auto TS/Go/curl samples + an interactive playground. Output is
// gitignored and regenerated at build time. No wire changes — this only *reads* openapi.json.
import { generateFiles } from "fumadocs-openapi";
import { openapi } from "../lib/openapi.ts";
import fs from "node:fs";
import path from "node:path";

const OUT = path.resolve("content/docs/reference/rest");
const SPEC = path.resolve("../../daemon/openapi.json");

if (!fs.existsSync(SPEC)) {
  console.log("gen-openapi-docs: no daemon/openapi.json found — skipping (commit daemon/openapi.json)");
  process.exit(0);
}

// Clean stale output so renamed/removed operations don't linger.
fs.rmSync(OUT, { recursive: true, force: true });

// Flatten the per-operation file name to `{method}-{path}` within each tag folder, so the sidebar is one
// level of tag folders over readable operation pages (not a deep path-segment tree).
function name(output) {
  if (output.type === "webhook") {
    const n = output.item.name.replace(/[^\w]+/g, "-").replace(/^-|-$/g, "").toLowerCase();
    return `${output.item.method}-${n || "webhook"}`;
  }
  const p = output.item.path
    .replace(/^\//, "")
    .replace(/[{}]/g, "")
    .replace(/[^\w]+/g, "-")
    .replace(/^-|-$/g, "")
    .toLowerCase();
  return `${output.item.method}-${p || "root"}`;
}

await generateFiles({
  input: openapi,
  output: OUT,
  per: "operation",
  groupBy: "tag",
  name,
  // Let fumadocs write per-folder meta.json (tag titles + page order); the top-level ordering is set by
  // content/docs/reference/meta.json.
  meta: true,
});

// Route-safe tag folders. fumadocs derives each tag folder name from the tag verbatim, so a tag like
// "Turns & Runs" becomes a folder literally named `turns-&-runs` — and Next cannot route a path segment
// containing `&` (every page under it 404s at runtime). Rename any tag folder carrying route-unsafe
// characters to a slug; the folder's own meta.json keeps its human `title` ("Turns & runs"), so only the
// URL segment changes. openapi.json is never touched. Renames are mapped into the root meta below.
const folderRenames = new Map();
for (const entry of fs.readdirSync(OUT, { withFileTypes: true })) {
  if (!entry.isDirectory()) continue;
  const safe = entry.name
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
  if (safe && safe !== entry.name) {
    fs.renameSync(path.join(OUT, entry.name), path.join(OUT, safe));
    folderRenames.set(entry.name, safe);
  }
}

// Give the generated root folder a proper sidebar title while preserving the tag order fumadocs picked
// (remapping any folder we just renamed so the page order still points at real folders).
const rootMeta = path.join(OUT, "meta.json");
if (fs.existsSync(rootMeta)) {
  const m = JSON.parse(fs.readFileSync(rootMeta, "utf8"));
  if (Array.isArray(m.pages)) m.pages = m.pages.map((p) => folderRenames.get(p) ?? p);
  fs.writeFileSync(rootMeta, JSON.stringify({ title: "REST API", ...m }, null, 2) + "\n");
}

const count = fs
  .readdirSync(OUT, { recursive: true })
  .filter((f) => typeof f === "string" && f.endsWith(".mdx")).length;
console.log(`gen-openapi-docs: wrote ${count} REST reference page(s) to content/docs/reference/rest`);
