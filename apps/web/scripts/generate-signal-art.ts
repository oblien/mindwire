/** Original Mindwire vector artwork. Run with `bun scripts/generate-signal-art.ts`. */
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { signalPoster, type SignalVariant } from "../components/landing/effects/signal-field";

const directory = fileURLToPath(new URL("../public/images/framework/", import.meta.url));
const ink = "#202523", paper = "#f7f5ed", coral = "#ef795b", blue = "#6676dc", mint = "#b5d1bd";
const svg = (w: number, h: number, body: string, title: string) => `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" fill="none"><title>${title}</title>${body.replace(/[ \t]+\n/g, "\n")}</svg>\n`;
const save = (name: string, w: number, h: number, body: string, title: string) => writeFileSync(directory + name + ".svg", svg(w, h, body, title));
const line = (d: string, color = ink, width = 1.5, extra = "") => `<path d="${d}" stroke="${color}" stroke-width="${width}" stroke-linecap="round" stroke-linejoin="round" ${extra}/>`;
const dot = (x: number, y: number, radius = 2, fill = ink) => `<circle cx="${x}" cy="${y}" r="${radius}" fill="${fill}"/>`;
const text = (x: number, y: number, value: string, size = 8, color = ink) => `<text x="${x}" y="${y}" fill="${color}" font-family="monospace" font-size="${size}" letter-spacing="1">${value}</text>`;
const rect = (x: number, y: number, w: number, h: number, fill: string, radius = 0, extra = "") => `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${radius}" fill="${fill}" ${extra}/>`;

// Knowledge is a bound, layered folio: three materials, one shared spine.
save("files", 366, 216, `
  ${line("M24 198H54M304 168H341M318 164L323 168L318 172", ink, 1, 'opacity=".35"')}
  <g transform="rotate(-14 172 174)">
    ${rect(67, 108, 203, 135, blue, 9)}
    ${line("M80 122H192M80 128H145", "#fff", 1, 'opacity=".5"')}
    ${text(230, 129, "03", 9, "#fff")}
  </g>
  <g transform="rotate(7 204 182)">
    ${rect(95, 101, 201, 137, mint, 9)}
    ${line("M108 114H196", ink, 1, 'opacity=".3"')}
    ${text(267, 122, "02")}
  </g>
  <g transform="rotate(-3 186 181)">
    ${rect(80, 119, 218, 139, "#141a1718", 9)}
    ${rect(80, 113, 218, 139, paper, 9)}
    ${rect(80, 113, 29, 139, coral, 9)}
    ${rect(99, 113, 10, 139, coral)}
    ${[132, 153, 174, 195, 216].map(y => dot(94, y, 2.5, "#ab4934")).join("")}
    ${text(122, 135, "YOUR CONTEXT", 7)}
    ${text(263, 135, "01", 8)}
    ${line("M122 146H280", "#d3d4c9", 1)}
    ${rect(122, 159, 93, 5, ink, 2)}
    ${rect(122, 170, 133, 4, "#b7bdb3", 2)}
    ${rect(122, 180, 111, 4, "#b7bdb3", 2)}
    ${rect(122, 200, 38, 19, blue, 4)}
    ${rect(166, 200, 54, 19, mint, 4)}
    ${line("M237 210L246 201L255 210L246 219Z", ink, 1.3)}
  </g>
  ${dot(33, 198, 3, coral)}
`, "Mindwire knowledge folio");

// A product surface assembled from independent layers, rather than a browser screenshot.
save("website", 366, 216, `
  <g transform="rotate(-7 180 170)">
    ${rect(65, 105, 253, 134, mint, 12)}
    ${rect(51, 96, 253, 134, blue, 12)}
    ${rect(37, 87, 253, 134, paper, 12)}
    ${line("M52 114H276", "#d6d9ce", 1)}
    ${dot(54, 101, 3, coral)}
    ${line("M64 101H102", ink, 2)}
    ${line("M244 101H258M266 101H275", ink, 1.5)}
    ${rect(51, 128, 61, 78, "#e8ebdf", 5)}
    ${line("M63 143H96M63 155H89M63 167H96M63 179H81", "#8e998b", 2.5)}
    ${dot(66, 194, 4, blue)}
    ${rect(126, 129, 60, 5, ink, 2)}
    ${rect(126, 141, 47, 4, "#9ca597", 2)}
    ${rect(126, 160, 53, 32, coral, 5)}
    ${line("M137 181L144 172L153 177L168 166", ink, 2)}
    ${rect(196, 127, 79, 79, ink, 7)}
    ${[0, 1, 2, 3, 4, 5, 6].map(i => line(`M${204 + i * 6} 196C${187 + i * 6} 169 ${246 + i * 6} 172 ${229 + i * 6} 137`, [mint, paper, coral][i % 3], 3.5)).join("")}
  </g>
  <g transform="rotate(7 292 174)">
    ${rect(268, 154, 67, 41, "#13191418", 9)}
    ${rect(268, 150, 67, 41, paper, 9)}
    ${rect(278, 161, 22, 18, coral, 9)}
    ${dot(291, 170, 6, paper)}
    ${line("M308 164H324M308 174H320", ink, 2)}
  </g>
`, "Mindwire layered product surface");

// A patch bay makes tools tangible without reproducing another application's UI.
save("workspace", 366, 216, `
  ${line("M76 177V186Q76 199 91 199H168Q182 199 182 213M182 177V213M288 177V186Q288 199 274 199H197Q182 199 182 213", "#7d8980", 2)}
  ${[
    { x: 34, fill: coral, label: "TOOLS", symbol: line("M59 131L51 138L59 145M91 131L99 138L91 145M80 127L70 149", ink, 2.5) },
    { x: 140, fill: mint, label: "CONTEXT", symbol: `<ellipse cx="182" cy="131" rx="13" ry="5" stroke="${ink}" stroke-width="2"/>${line("M169 131V146C169 153 195 153 195 146V131M169 139C169 146 195 146 195 139", ink, 2)}` },
    { x: 246, fill: blue, label: "ACTIONS", symbol: line("M275 138H300M291 129L300 138L291 147M275 129V147", paper, 2.5) },
  ].map(({ x, fill, label, symbol }) => `
    ${rect(x + 3, 109, 84, 72, "#15201a18", 12)}
    ${rect(x, 104, 84, 72, fill, 12)}
    ${line(`M${x + 12} 114H${x + 72}`, ink, 1, 'opacity=".18"')}
    ${symbol}
    ${text(x + 12, 164, label, 7, fill === blue ? paper : ink)}
    ${dot(x + 42, 182, 4, ink)}
    ${dot(x + 42, 182, 1.5, paper)}
  `).join("")}
  ${dot(182, 213, 7, ink)}${dot(182, 213, 3, coral)}
`, "Mindwire modular tool patch bay");

save("api", 366, 216, `
  ${rect(32, 93, 302, 147, "#17201b14", 10)}
  ${rect(28, 89, 302, 147, paper, 10)}
  ${rect(28, 89, 27, 147, ink, 10)}
  ${rect(45, 89, 10, 147, ink)}
  ${[105, 126, 147, 168, 189, 210].map((y, i) => dot(42, y, 2.5, i === 0 ? coral : "#69766b")).join("")}
  ${line("M69 112H316", "#d5d9cf", 1)}
  ${line("M303 100H313M308 95V105", ink, 1.3)}
  ${rect(69, 99, 26, 4, coral, 2)}
`, "Mindwire SDK instruction card");

// Shared line icons use the same square endpoints and folded paths as the larger scenes.
save("files-icon", 40, 40, `${line("M12 5H31V29H12ZM8 10H5V35H26V32M17 12H26M17 18H26M17 24H23", ink, 1.6)}${dot(31, 5, 3)}`, "Knowledge layers");
save("website-icon", 40, 40, `${line("M5 5H25V25H5ZM15 15H35V35H15ZM5 11H25M15 21H35", ink, 1.6)}${rect(29, 29, 3, 3, ink)}`, "Product layers");
save("workspace-icon", 40, 40, `${line("M8 9H20V20H32V31M8 31H20V20M20 9V20", ink, 1.6)}${[ [8,9],[20,9],[32,20],[8,31],[32,31] ].map(([x,y])=>rect(x-3,y-3,6,6,paper,1,`stroke="${ink}" stroke-width="1.4"`)).join("")}${dot(20,20,3)}`, "Tool connections");
save("agents-icon", 40, 40, `${line("M20 5V11M20 29V35M5 20H11M29 20H35M9 9L13 13M27 27L31 31M9 31L13 27M27 13L31 9", ink, 1.6)}<circle cx="20" cy="20" r="9" stroke="${ink}" stroke-width="1.6"/>${line("M16 20H24M20 16V24", ink, 1.6)}`, "Agent signal");
save("api-icon", 40, 40, `${line("M10 10L3 20L10 30M30 10L37 20L30 30M25 5L15 35", ink, 1.8)}${dot(20,20,3)}`, "SDK connection");

const theme = `<style>:root{--panel:#d9d9d9;--ink:#202523;--edge:#aeb3ac}@media(prefers-color-scheme:dark){:root{--panel:#242424;--ink:#e7e9df;--edge:#596158}}</style>`;
save("hub", 239, 239, `${theme}
  ${rect(0, 0, 239, 239, "var(--panel)", 42)}
  ${rect(12, 12, 215, 215, "none", 33, 'stroke="var(--edge)" stroke-width="1"')}
  ${[ [26,26],[213,26],[26,213],[213,213] ].map(([x,y])=>`<circle cx="${x}" cy="${y}" r="3" stroke="var(--ink)" stroke-width="1"/>`).join("")}
  <circle cx="119.5" cy="118" r="70" stroke="var(--edge)" stroke-width="1"/>
  <circle cx="119.5" cy="118" r="62" stroke="var(--edge)" stroke-width="1" stroke-dasharray="1 5"/>
  ${line("M32 118H49M190 118H207M119.5 31V48M119.5 188V205", "var(--ink)", 1)}
  ${line("M82 139V110C82 92 111 92 111 110V135C111 142 127 142 127 135V110C127 92 156 92 156 110V139", "var(--ink)", 9)}
  ${dot(82, 144, 4.5, coral)}${dot(156, 144, 4.5, blue)}
`, "Mindwire signal relay");
save("hub-ticks", 83, 7, `${line("M0 3.5H24M59 3.5H83", ink, 1)}${dot(34,3.5,2)}${dot(42,3.5,2)}${dot(50,3.5,2)}`, "Relay signal indicator");

const loops = (count: number, cx: number, cy: number, size: number, turn: number, color = ink) => Array.from({ length: count }, (_, i) => {
  const inset = i * size / count * .3;
  return `<rect x="${cx-size/2+inset}" y="${cy-size/2+inset}" width="${size-inset*2}" height="${size-inset*2}" rx="${(size-inset*2)*.28}" stroke="${color}" stroke-width="1" transform="rotate(${i*turn} ${cx} ${cy})"/>`;
}).join("");
save("canvas-motif", 105, 29, `${line("M1 21H16Q23 21 23 14T30 7H39Q46 7 46 14T53 21H62Q69 21 69 14T76 7H90", ink, 1.5)}${line("M1 15H10Q17 15 17 8T24 1H33Q40 1 40 8T47 15H56Q63 15 63 8T70 1H84", ink, 1, 'opacity=".4"')}${dot(94,7,4)}${dot(1,21,1)}`, "Mindwire signal signature");
save("cross", 22, 22, `${line("M2 6V2H6M16 2H20V6M20 16V20H16M6 20H2V16", ink, 1.5)}${dot(11,11,2)}`, "Signal registration mark");
save("connect-motif", 140, 160, `${loops(10,70,79,82,9)}${line("M70 3V23M70 135V157M7 79H20M120 79H133", ink, 1)}${dot(70,79,5)}`, "Mindwire signal coil");
save("sphere", 160, 160, `${loops(16,80,80,104,7)}${dot(80,80,5)}`, "Interwoven signal coil");
save("globe", 328, 327, `${loops(26,164,164,205,5.6)}${line("M164 9V45M164 283V319M9 164H45M283 164H319", ink, 1)}${dot(164,164,8)}${dot(164,18,3)}${dot(310,164,3)}`, "Mindwire wire sculpture");
save("footer-orbit", 33, 33, `${line("M8 5H23Q28 5 28 10V24Q28 28 24 28H10Q5 28 5 23V11Q5 8 8 8H19Q22 8 22 11V21Q22 23 20 23H12Q10 23 10 21V15Q10 12 13 12H17Q19 12 19 14V18", ink, 1.5)}`, "Continuous wire");

const ribbon = (offset: number, color: string) => line(`M${60+offset} 313C${-10+offset} 226 ${258+offset} 235 ${191+offset} 153S${-2+offset} 66 ${103+offset} 14`, color, 11);
save("cursor-trail", 290, 322, `${[-20,-6,8,22,36].map((v,i)=>ribbon(v,[blue,mint,paper,coral,ink][i])).join("")}`, "Mindwire woven signal ribbon");
save("footer-trail", 190, 100, `<g transform="translate(47 -2) rotate(90 50 50) scale(.29)">${[-20,-6,8,22,36].map((v,i)=>ribbon(v,[blue,mint,paper,coral,ink][i])).join("")}</g>`, "Woven signal signature");

for (const variant of ["bloom", "cobalt", "thermal", "lime", "closing"] as SignalVariant[]) {
  writeFileSync(directory + variant + ".svg", signalPoster(variant));
}
console.log("Generated original Mindwire SVG illustrations, symbols, and signal fields.");
