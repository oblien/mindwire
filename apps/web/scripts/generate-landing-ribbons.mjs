/** Rebuild the landing artwork from its original vector geometry. */
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { mkdir } from "node:fs/promises";

const require = createRequire(import.meta.url);
const nextRequire = createRequire(require.resolve("next/package.json"));
const sharp = nextRequire("sharp");
const sourceDirectory = fileURLToPath(
  new URL("../../../.cache/mindwire-ribbons/", import.meta.url),
);
const outputDirectory = fileURLToPath(
  new URL("../public/images/design-lab/sculpted/", import.meta.url),
);
const generator = fileURLToPath(
  new URL("./generate-landing-ribbons.py", import.meta.url),
);
const result = spawnSync("python3", [generator, sourceDirectory], {
  stdio: "inherit",
});
if (result.status !== 0) throw new Error("Ribbon vector generation failed");
await mkdir(outputDirectory, { recursive: true });
for (const kind of ["knot", "loop"]) {
  await sharp(`${sourceDirectory}/ribbon-${kind}.svg`, { density: 180 })
    .resize(1400, 1400)
    .webp({ quality: 92 })
    .toFile(`${outputDirectory}/ribbon-${kind}.webp`);
}
console.log(
  "Exported both original ribbon sculptures; SVG sources are in .cache/mindwire-ribbons.",
);
