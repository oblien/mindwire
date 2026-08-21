// Generates the Go SDK reference from daemon/sdk (package `mindwire`) into content/docs/reference/sdk-go/
// index.mdx by running gomarkdoc (pinned, via `go run`) in `plain` format — which avoids the GitHub HTML
// (<details>, file-header comment) that MDX can't parse. daemon/sdk is one package, so it's one page.
// Needs a Go toolchain (CI already has it for the daemon); skips gracefully if `go` is unavailable.
// Output is gitignored and regenerated at build time. Read-only over the SDK — no wire changes.
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

// Wraps each maximal run of tab-indented lines (gomarkdoc's Go signatures/examples) in a ```go fence,
// stripping one leading tab. A run may contain interior blank/tab-only lines (e.g. within an import
// block); trailing blanks are pushed back out. Prose and lists sit at column 0, so they're untouched.
function fenceTabIndentedCode(md) {
  const lines = md.split("\n");
  const out = [];
  for (let i = 0; i < lines.length; ) {
    const atFlowBoundary = out.length === 0 || out[out.length - 1].trim() === "";
    if (lines[i].startsWith("\t") && atFlowBoundary) {
      const block = [];
      while (i < lines.length && (lines[i].startsWith("\t") || lines[i].trim() === "")) {
        block.push(lines[i]);
        i++;
      }
      while (block.length && block[block.length - 1].trim() === "") block.pop();
      out.push("```go", ...block.map((l) => l.replace(/^\t/, "")), "```", "");
      continue;
    }
    out.push(lines[i]);
    i++;
  }
  return out.join("\n");
}

const GOMARKDOC = "github.com/princjef/gomarkdoc/cmd/gomarkdoc@v1.1.0";
const DAEMON = path.resolve("../../daemon");
const OUT = path.resolve("content/docs/reference/sdk-go");
const TMP = path.join(OUT, ".gomarkdoc.md");

if (!fs.existsSync(path.join(DAEMON, "sdk"))) {
  console.log("gen-go-docs: no daemon/sdk found — skipping");
  process.exit(0);
}

fs.rmSync(OUT, { recursive: true, force: true });
fs.mkdirSync(OUT, { recursive: true });

// Distinguish "no Go toolchain" (a local docs edit without Go — skip the page, keep the build green)
// from "Go is here but gomarkdoc broke" (a real failure — fail loudly rather than silently shipping
// docs missing the Go SDK reference). CI installs Go, so there it always takes the fail-loud path.
let hasGo = true;
try {
  execFileSync("go", ["version"], { stdio: "ignore" });
} catch {
  hasGo = false;
}
if (!hasGo) {
  console.log("gen-go-docs: no Go toolchain found — skipping the Go SDK reference.");
  fs.rmSync(OUT, { recursive: true, force: true });
  process.exit(0);
}

try {
  execFileSync(
    "go",
    ["run", GOMARKDOC, "--format", "plain", "--output", TMP, "./sdk"],
    { cwd: DAEMON, stdio: ["ignore", "inherit", "inherit"] },
  );
} catch (err) {
  console.error(`gen-go-docs: gomarkdoc failed with Go present — the Go SDK reference cannot be generated.`);
  console.error(err?.message ?? err);
  fs.rmSync(OUT, { recursive: true, force: true });
  process.exit(1);
}

let src = fs.readFileSync(TMP, "utf8");
fs.rmSync(TMP, { force: true });

// MDX can't parse HTML comments (gomarkdoc's "Code generated" header) — strip them.
src = src.replace(/<!--[\s\S]*?-->/g, "");
// Drop the package H1 (`# mindwire`); Fumadocs renders the page title from frontmatter.
src = src.replace(/^#\s+mindwire\s*$/m, "").trimStart();
// gomarkdoc's `plain` format emits the package-import header as a bare `import "…"` line. MDX treats a
// top-level `import`/`export` as ESM and would try to resolve the module path — fence it as Go code.
src = src.replace(/^(import(?:\s+[\w.]+)?\s+"[^"]+")\s*$/m, "```go\n$1\n```");
// MDX disables indented code blocks (they're ambiguous with JSX indentation), so gomarkdoc's
// tab-indented signatures/examples would be parsed as prose — and their raw `{…}` as JSX expressions.
// Convert every tab-indented run into a fenced ```go block (dedented) so it renders as code.
src = fenceTabIndentedCode(src);

const frontmatter =
  `---\n` +
  `title: "Go SDK"\n` +
  `description: "Native in-process Go SDK — github.com/oblien/mindwire/daemon/sdk."\n` +
  `---\n\n`;

fs.writeFileSync(path.join(OUT, "index.mdx"), frontmatter + src + "\n");
fs.writeFileSync(
  path.join(OUT, "meta.json"),
  JSON.stringify({ title: "Go SDK", pages: ["index"] }, null, 2) + "\n",
);

console.log("gen-go-docs: wrote Go SDK reference to content/docs/reference/sdk-go/index.mdx");
