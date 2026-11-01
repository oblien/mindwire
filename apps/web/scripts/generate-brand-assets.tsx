import { mkdir, readFile, writeFile } from "node:fs/promises";
import { ImageResponse } from "next/og";
import MindWireMark from "../components/brand/MindWireMark";
import mark from "../lib/brand-mark.json";

// Run from apps/web: bun scripts/generate-brand-assets.tsx
// The site, illustrations and exported assets use the same original geometry.
const root = new URL("../", import.meta.url);
const output = new URL("public/brand/", root);
await mkdir(output, { recursive: true });

const embeddedMark = `<svg x="254" y="158" width="25" height="25" viewBox="${mark.viewBox}"><path d="${mark.path}" fill="currentColor" stroke="none"/></svg>`;
for (const file of [
  "public/images/design-lab/linework/sdk-embed.svg",
  "components/design-lab/refined/linework-art.ts",
]) {
  const url = new URL(file, root);
  const source = await readFile(url, "utf8");
  await writeFile(
    url,
    source.replace(/<svg x="254" y="158"[^>]*>.*?<\/svg>/, embeddedMark),
  );
}

for (const [name, color] of [
  ["mark", "#1c1c1a"],
  ["mark-light", "#f8f7f3"],
]) {
  await writeFile(
    new URL(`${name}.svg`, output),
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${mark.viewBox}" fill="${color}"><title>MindWire</title><path d="${mark.path}"/></svg>\n`,
  );
}

const favicon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-3 -3 56 56"><title>MindWire</title><style>path{fill:#1c1c1a}@media(prefers-color-scheme:dark){path{fill:#f8f7f3}}</style><path d="${mark.path}"/></svg>\n`;
await writeFile(new URL("app/icon.svg", root), favicon);
await writeFile(new URL("public/favicon.svg", root), favicon);

for (const [file, size, inset, background, color] of [
  ["app/apple-icon.png", 180, 38, "#f8f7f3", "#1c1c1a"],
  ["public/brand/app-icon.png", 512, 108, "#22291f", "#f8f7f3"],
  ["public/brand/mark.png", 512, 0, "transparent", "#1c1c1a"],
] as const) {
  const response = new ImageResponse(
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        width: "100%",
        height: "100%",
        background,
        color,
      }}
    >
      <MindWireMark width={size - 2 * inset} height={size - 2 * inset} />
    </div>,
    { width: size, height: size },
  );
  await writeFile(
    new URL(file, root),
    Buffer.from(await response.arrayBuffer()),
  );
}

const font = await readFile(new URL("assets/social/host-medium.ttf", root));
for (const [name, color] of [
  ["wordmark", "#1c1c1a"],
  ["wordmark-light", "#f8f7f3"],
]) {
  const response = new ImageResponse(
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        gap: 30,
        width: "100%",
        height: "100%",
        color,
        fontFamily: "Host",
        fontSize: 120,
        fontWeight: 500,
        letterSpacing: -6,
      }}
    >
      <MindWireMark width={100} height={100} />
      <span>MindWire</span>
    </div>,
    {
      width: 700,
      height: 180,
      fonts: [{ name: "Host", data: font, weight: 500, style: "normal" }],
    },
  );
  await writeFile(
    new URL(`${name}.png`, output),
    Buffer.from(await response.arrayBuffer()),
  );
}
console.log(
  "Generated MindWire brand marks, wordmarks, favicon and app icons.",
);
