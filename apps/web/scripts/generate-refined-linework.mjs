// Original artwork and motion geometry for the refined landing previews.
// Run from the repository root: node apps/web/scripts/generate-refined-linework.mjs
import { readFileSync, writeFileSync } from "node:fs";
const brandMark = JSON.parse(
  readFileSync(new URL("../lib/brand-mark.json", import.meta.url), "utf8"),
);

{
  // Original, editable vector studies. No grain, filters, copied paths, or bitmaps.
  const destination = "apps/web/public/images/design-lab/linework";
  const ink = "#292c27";
  const paper = "#f3f2ed";
  const path = (d, attrs = "") => `<path d="${d}" ${attrs}/>`;
  const circle = (x, y, r, attrs = "") =>
    `<circle cx="${x}" cy="${y}" r="${r}" ${attrs}/>`;
  const rect = (x, y, w, h, r, attrs = "") =>
    `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${r}" ${attrs}/>`;
  const dot = (x, y, r = 3) => circle(x, y, r, `fill="${ink}" stroke="none"`);
  const node = (x, y, r = 12, hatch = false) =>
    circle(x, y, r, `fill="${hatch ? "url(#hatch)" : paper}"`);
  const cross = (x, y, size = 5) =>
    path(`M${x - size} ${y}H${x + size}M${x} ${y - size}V${y + size}`);
  const svg = (title, content) =>
    `<svg xmlns="http://www.w3.org/2000/svg" width="800" height="800" viewBox="0 0 400 400" fill="none" stroke="${ink}" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><title>${title}</title><defs><pattern id="hatch" width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(-32)"><path d="M0 0V6" stroke="${ink}" stroke-width="1"/></pattern></defs>${content}</svg>\n`;
  const save = (name, title, content) =>
    writeFileSync(`${destination}/${name}.svg`, svg(title, content));

  save(
    "knowledge-map",
    "Knowledge, connected by context",
    [
      path(
        "M57 138C115 103 136 65 203 88S302 113 339 161M70 279C136 300 225 335 308 280",
        'stroke-dasharray="3 7" opacity=".55"',
      ),
      path("M79 142L205 91L322 154L263 292L108 273Z"),
      path(
        "M79 142Q120 197 202 202T322 154M108 273L202 202L205 91M202 202L263 292",
      ),
      path(
        "M79 142L108 273M205 91L263 292",
        'stroke-dasharray="4 6" opacity=".65"',
      ),
      node(79, 142, 18),
      node(205, 91, 15, true),
      node(322, 154, 13),
      node(108, 273, 14, true),
      node(263, 292, 22),
      circle(202, 202, 28, `fill="${paper}"`),
      circle(202, 202, 18, 'stroke-dasharray="2 5"'),
      dot(202, 202, 4),
      path("M251 286H274M251 292H274M251 298H264", 'stroke-width="1.2"'),
      dot(79, 142),
      dot(322, 154),
      cross(41, 72),
      cross(354, 323),
      path("M54 339H164M179 339H202M218 339H337", 'opacity=".26"'),
      ...[54, 109, 164, 218, 273, 337].map((x) =>
        path(`M${x} 336V342`, 'opacity=".45"'),
      ),
    ].join(""),
  );

  save(
    "tool-routes",
    "Tools as a set of possible actions",
    [
      path("M61 202H125"),
      path("M125 202C172 202 166 83 211 83H307"),
      path("M125 202C185 202 174 157 222 157H338"),
      path("M125 202C185 202 175 245 222 245H338"),
      path("M125 202C173 202 166 320 211 320H307"),
      path("M210 64V338", 'stroke-dasharray="3 7" opacity=".35"'),
      path("M57 168V146H86M57 236V260H86", 'opacity=".55"'),
      circle(125, 202, 28, `fill="${paper}"`),
      circle(125, 202, 16, 'fill="url(#hatch)"'),
      node(307, 83, 14),
      rect(325, 144, 26, 26, 6, `fill="${paper}"`),
      circle(338, 245, 14, 'fill="url(#hatch)"'),
      node(307, 320, 14),
      dot(61, 202, 4),
      dot(307, 83, 3),
      path("M302 320H312M307 315V325M332 157H344"),
      circle(210, 157, 4, `fill="${paper}"`),
      circle(210, 245, 4, `fill="${paper}"`),
      path("M268 72L279 83L268 94", 'stroke-width="1.1"'),
      path("M291 234L302 245L291 256", 'stroke-width="1.1"'),
    ].join(""),
  );

  save(
    "interface-frames",
    "An agent fitted to your interface",
    [
      rect(52, 60, 240, 232, 30, 'stroke-dasharray="4 6" opacity=".6"'),
      rect(83, 88, 240, 232, 38),
      rect(114, 117, 229, 230, 48),
      path("M83 155H322M151 89V319", 'stroke-dasharray="2 6" opacity=".48"'),
      path("M114 182H343M182 117V347", 'opacity=".3"'),
      rect(213, 217, 96, 94, 24, `fill="${paper}"`),
      rect(225, 229, 72, 70, 18, 'stroke-dasharray="3 5"'),
      path("M240 254H282M240 264H271M240 275H278"),
      circle(290, 304, 15, `fill="${paper}"`),
      dot(290, 304, 4),
      dot(103, 108, 3),
      dot(117, 108, 2),
      cross(53, 327),
      cross(341, 61),
      path("M43 371H347", 'stroke-dasharray="1 8" opacity=".45"'),
    ].join(""),
  );

  save(
    "identity-system",
    "Your identity, carried into an interface",
    [
      path(
        "M42 102V67H78M327 322H361V287",
        'stroke-dasharray="3 6" opacity=".35"',
      ),
      rect(154, 65, 191, 222, 13, 'fill="#faf9f5"'),
      path("M154 93H345"),
      dot(168, 79, 2.5),
      circle(179, 79, 2),
      circle(190, 79, 2),
      rect(174, 111, 150, 61, 7, 'fill="#e4d7ec" stroke-opacity=".55"'),
      path(
        "M188 128H249M188 140H282M188 153H230",
        'stroke-width="2" opacity=".65"',
      ),
      rect(174, 188, 64, 52, 6, 'fill="#e9eee3" stroke-opacity=".4"'),
      rect(250, 188, 74, 52, 6, 'stroke-dasharray="3 5"'),
      path("M263 205H310M263 215H299", 'opacity=".55"'),
      rect(54, 148, 185, 187, 12, 'fill="#f9f8f3"'),
      '<text x="77" y="253" font-family="Georgia, serif" font-size="80" fill="#292c27" stroke="none" letter-spacing="-5">Aa</text>',
      path("M76 170H116M76 278H214", 'opacity=".4"'),
      circle(89, 304, 10, 'fill="#cdb7dd"'),
      circle(124, 304, 10, 'fill="#d1dfbf"'),
      circle(159, 304, 10, 'fill="#e8bea3"'),
      path("M188 299H216M188 308H206"),
      path("M260 275H325M260 285H301", 'opacity=".5"'),
      path("M267 320H344", 'stroke-dasharray="2 6" opacity=".5"'),
      dot(267, 320, 3),
    ].join(""),
  );

  save(
    "context-sources",
    "Source documents connected to useful context",
    [
      rect(110, 59, 184, 235, 10, 'fill="#e0e8d3"'),
      path("M130 80H214M130 92H253", 'opacity=".5"'),
      rect(84, 81, 184, 235, 10, 'fill="#f0e2b4"'),
      path("M104 103H229", 'opacity=".45"'),
      path(
        "M64 109H205L240 144V324Q240 339 225 339H64Q50 339 50 325V123Q50 109 64 109Z",
        'fill="#faf9f5"',
      ),
      path("M205 109V134Q205 144 215 144H240", 'fill="#e9eee3"'),
      rect(73, 137, 33, 8, 3, 'fill="#292c27" stroke="none"'),
      path(
        "M73 170H209M73 185H198M73 200H213M73 215H168",
        'stroke-width="2" opacity=".58"',
      ),
      rect(72, 236, 121, 40, 6, 'fill="#e7eddf" stroke="none"'),
      path("M83 249H178M83 260H160", 'opacity=".65"'),
      path("M73 297H167M73 308H194", 'opacity=".5"'),
      path("M240 207H296Q332 207 332 243V268", 'stroke-dasharray="3 5"'),
      rect(207, 265, 148, 76, 12, 'fill="#f8f7f3"'),
      circle(230, 289, 7, 'fill="#cdddb8"'),
      circle(230, 316, 7, 'fill="#f0e0ad"'),
      path("M248 284H335M248 294H309M248 311H327M248 321H298", 'opacity=".6"'),
      dot(332, 265, 3),
      path("M42 74H64M53 63V85M322 99H344M333 88V110", 'opacity=".4"'),
    ].join(""),
  );

  save(
    "action-workflow",
    "A tool call, an approval, and a completed action",
    [
      path("M48 212V333H127", 'stroke-dasharray="2 7" opacity=".25"'),
      rect(52, 58, 193, 120, 12, 'fill="#faf9f5"'),
      path("M52 85H245"),
      dot(67, 72, 2.5),
      circle(79, 72, 2),
      circle(90, 72, 2),
      path(
        "M110 109L94 125L110 141M181 109L197 125L181 141M154 104L139 146",
        'stroke-width="2.3"',
      ),
      path("M74 160H207", 'opacity=".35"'),
      path("M245 115H282Q312 115 312 145V181", 'stroke-dasharray="3 5"'),
      rect(221, 182, 134, 76, 14, 'fill="#efd1bb"'),
      rect(240, 198, 78, 30, 10, 'fill="#faf9f5"'),
      path(
        "M273 210L273 246L282 236L289 249L297 245L290 232H304Z",
        'fill="#292c27" stroke="#faf9f5" stroke-width="1.5"',
      ),
      path("M263 202L259 197M271 199V191M279 202L283 197", 'stroke="#8d5e44"'),
      path("M287 258V272Q287 292 267 292H252", 'stroke-dasharray="3 5"'),
      rect(137, 294, 213, 62, 12, 'fill="#faf9f5"'),
      circle(163, 325, 14, 'fill="#d6e1c5"'),
      path("M156 325L161 330L171 319", 'stroke-width="1.8"'),
      path("M190 316H326M190 328H307M190 339H276", 'opacity=".55"'),
      path("M46 37H70M58 25V49M342 85H365M353 73V97", 'opacity=".4"'),
    ].join(""),
  );

  save(
    "product-frame",
    "The agent layer inside your product",
    [
      path(
        "M22 81V59H45M355 59H378V81M22 319V341H45M355 341H378V319",
        'stroke-dasharray="3 5" opacity=".35"',
      ),
      rect(34, 73, 331, 250, 12, `fill="#fbfaf7"`),
      path("M34 99H365M71 99V323"),
      dot(48, 86, 2.8),
      circle(59, 86, 2.3),
      circle(69, 86, 2.3),
      circle(52, 121, 5, 'fill="url(#hatch)"'),
      path("M46 151H58M46 170H58M46 189H58", 'opacity=".65"'),
      rect(87, 117, 158, 122, 7, 'fill="#e8ece2" stroke-opacity=".4"'),
      path(
        "M94 220C98 155 130 126 174 125C211 124 235 153 239 191V232H94Z",
        'fill="#dcd5e5" stroke="none"',
      ),
      path(
        "M94 225C139 189 171 214 239 166V232H94Z",
        'fill="#f6f4ed" stroke="none"',
      ),
      '<g transform="translate(162 176) rotate(-24)">',
      ...[0, 1, 2, 3, 4].map(
        (i) =>
          `<ellipse cx="${i * 8 - 16}" cy="0" rx="${52 - i * 5}" ry="${15 + i * 7}" ${i === 2 ? 'stroke-dasharray="3 5"' : ""}/>`,
      ),
      circle(6, -2, 6, 'fill="#b66f56" stroke="none"'),
      "</g>",
      path(
        "M88 257H204M88 269H229M88 281H185",
        'stroke-width="2" opacity=".5"',
      ),
      rect(88, 299, 39, 7, 3, `fill="${ink}" stroke="none"`),
      path("M138 302H174", 'opacity=".3"'),
      rect(260, 106, 116, 230, 21, 'fill="#efeee8"'),
      circle(318, 135, 13),
      circle(318, 135, 6, 'fill="url(#hatch)"'),
      path("M300 135H288M337 135H349", 'stroke-dasharray="2 4"'),
      rect(274, 166, 86, 38, 12, 'fill="#fbfaf7" stroke-opacity=".6"'),
      path("M286 179H347M286 190H333", 'opacity=".55"'),
      rect(289, 218, 71, 35, 11, 'fill="#e7ddd7" stroke-dasharray="3 4"'),
      path("M300 231H348M300 241H338", 'opacity=".5"'),
      path("M318 264V279", 'stroke-dasharray="2 4"'),
      circle(318, 291, 12),
      path("M312 291L316 295L325 286"),
      rect(175, 306, 118, 39, 10, `fill="#fbfaf7"`),
      circle(193, 325, 7, 'fill="url(#hatch)"'),
      path("M208 319H275M208 329H258", 'opacity=".55"'),
    ].join(""),
  );

  writeFileSync(
    `${destination}/SOURCES.json`,
    JSON.stringify(
      {
        method: "Original code-drawn SVG illustrations",
        created: "2026-10-31",
        direction:
          "Fine ink contours, interrupted paths, sparse hatching, and smooth neutral surfaces. Controlled lilac, sage, butter, and apricot fields distinguish identity, context, and actions. Inspired by the visual language of the supplied references; all geometry is original.",
        assets: [
          ["knowledge-map", "Knowledge organized as a connected map"],
          ["tool-routes", "A tool catalogue as branching action routes"],
          [
            "interface-frames",
            "An agent fitted within adaptable interface boundaries",
          ],
          [
            "product-frame",
            "A clean product illustration with an integrated agent",
          ],
          [
            "identity-system",
            "A brand and interface kit with typography and palette",
          ],
          ["context-sources", "Source documents and connected references"],
          [
            "action-workflow",
            "Tool call, human approval, and completed result",
          ],
        ].map(([name, concept]) => ({
          name,
          concept,
          output: `apps/web/public/images/design-lab/linework/${name}.svg`,
        })),
      },
      null,
      2,
    ) + "\n",
  );

  console.log("Saved seven original linework illustrations.");
}

{
  const directory = "apps/web/public/images/design-lab/linework";
  const ink = "#292c27";
  const path = (d, attrs = "") => `<path d="${d}" ${attrs}/>`;
  const circle = (x, y, r, attrs = "") =>
    `<circle cx="${x}" cy="${y}" r="${r}" ${attrs}/>`;
  const dot = (x, y, r = 3) =>
    circle(x, y, r, 'fill="currentColor" stroke="none"');
  const svg = (title, body) =>
    `<svg xmlns="http://www.w3.org/2000/svg" width="800" height="800" viewBox="0 0 400 400" fill="none" stroke="currentColor" color="${ink}" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><title>${title}</title>${body}</svg>\n`;
  const handoff = [
    path("M72 145L42 195L72 245M342 145L372 195L342 245", 'stroke-width="2"'),
    '<rect x="92" y="97" width="232" height="197" rx="14" fill="#faf9f5"/>',
    path("M92 128H324"),
    dot(109, 112, 3),
    circle(122, 112, 2.5),
    circle(135, 112, 2.5),
    '<rect x="111" y="147" width="103" height="67" rx="7" fill="#e8eee0" stroke-opacity=".45"/>',
    path("M126 164H195M126 177H180M126 190H191", 'opacity=".6"'),
    path("M111 236H211M111 249H194M111 263H181", 'opacity=".5"'),
    '<rect x="231" y="146" width="74" height="128" rx="13" fill="#e4d9ea"/>',
    `<svg x="254" y="158" width="25" height="25" viewBox="${brandMark.viewBox}"><path d="${brandMark.path}" fill="currentColor" stroke="none"/></svg>`,
    path("M245 198H290M245 208H280"),
    '<rect x="244" y="225" width="49" height="32" rx="8" fill="#faf9f5" stroke-dasharray="3 4"/>',
    path("M254 237H283M254 246H275", 'opacity=".55"'),
    path("M60 328H206Q246 328 246 295", 'stroke-dasharray="3 6"'),
    dot(60, 328, 4),
    circle(246, 294, 5, 'fill="#faf9f5"'),
    path("M133 68H162M148 60V76M326 327H352M339 315V339", 'opacity=".4"'),
  ].join("");
  writeFileSync(
    `${directory}/sdk-embed.svg`,
    svg("Code that becomes part of your product", handoff),
  );

  function project(u, v) {
    const R = 117,
      r = 49;
    const x = (R + r * Math.cos(v)) * Math.cos(u),
      y = (R + r * Math.cos(v)) * Math.sin(u),
      z = r * Math.sin(v);
    const ax = 0.78,
      ay = 0.42,
      az = -0.46;
    const x1 = x * Math.cos(ay) + z * Math.sin(ay),
      z1 = -x * Math.sin(ay) + z * Math.cos(ay);
    const y1 = y * Math.cos(ax) - z1 * Math.sin(ax);
    return [
      200 + x1 * Math.cos(az) - y1 * Math.sin(az),
      200 + x1 * Math.sin(az) + y1 * Math.cos(az),
    ];
  }
  function curve(fn, n = 160) {
    return (
      Array.from({ length: n + 1 }, (_, i) => {
        const p = fn((i / n) * Math.PI * 2);
        return `${i ? "L" : "M"}${p[0].toFixed(2)} ${p[1].toFixed(2)}`;
      }).join(" ") + "Z"
    );
  }
  const around = Array.from({ length: 9 }, (_, i) =>
    curve((u) => project(u, (i / 9) * Math.PI * 2)),
  );
  const across = Array.from({ length: 16 }, (_, i) =>
    curve((v) => project((i / 16) * Math.PI * 2, v), 80),
  );
  const weave = [
    '<g data-motion="drift">',
    ...around.map((d, i) =>
      path(
        d,
        `opacity="${i % 3 === 0 ? ".9" : ".5"}" ${i === 3 ? 'stroke="#d5c1df"' : ""} ${i % 3 === 1 ? 'stroke-dasharray="2 5"' : ""}`,
      ),
    ),
    ...across.map((d, i) =>
      path(
        d,
        `opacity="${i % 4 === 0 ? ".9" : ".44"}" ${i % 4 === 2 ? 'stroke-dasharray="2 6"' : ""}`,
      ),
    ),
    ...[
      [0.4, 0, "#d5b9e2"],
      [2.7, 1.3, "#e2b49b"],
      [4.1, 4.1, "#d1deb9"],
    ].map(([u, v, color]) => {
      const [x, y] = project(u, v);
      return circle(x, y, 3.5, `fill="${color}" stroke="none"`);
    }),
    "</g>",
    path("M31 101V79H53M347 321H369V299", 'opacity=".65"'),
    path("M40 321H78M321 79H359", 'stroke-dasharray="2 6" opacity=".5"'),
    dot(40, 321, 2.5),
    dot(359, 79, 2.5),
  ].join("");
  writeFileSync(
    `${directory}/closing-weave.svg`,
    svg("Many possibilities, one connected system", weave),
  );

  const body = (name) =>
    readFileSync(`${directory}/${name}.svg`, "utf8")
      .replace(/^<svg[^>]*>/, "")
      .replace(/<title>.*?<\/title>/, "")
      .replace(/<\/svg>\s*$/, "");
  const trace = (markup) =>
    markup.replace(
      /<(path|ellipse|rect)([^>]*stroke-dasharray="([\d. ]+)"[^>]*)\/>/g,
      (_, tag, attributes, dashes) => {
        const shift =
          dashes
            .trim()
            .split(/\s+/)
            .reduce((sum, n) => sum + Number(n), 0) * 4;
        return `<${tag}${attributes} data-motion="trace" style="--trace-shift:-${shift}px"/>`;
      },
    );
  const traveller = (d, duration = 30, delay = 0) =>
    `<circle cx="0" cy="0" r="3.1" fill="currentColor" stroke="none" data-motion="travel" style="offset-path:path('${d}');--travel-duration:${duration}s;animation-delay:${delay}s"/>`;
  const identity = body("identity-system");
  const context = body("context-sources");
  const actions = body("action-workflow");
  const scenes = {
    identity: {
      markup:
        trace(identity) +
        traveller("M325 65Q345 65 345 85V267Q345 287 325 287", 22),
    },
    context: {
      markup:
        trace(context) + traveller("M240 207H296Q332 207 332 243V265", 18),
    },
    actions: {
      markup:
        trace(actions) + traveller("M245 115H282Q312 115 312 145V181", 14),
    },
    handoff: {
      markup: trace(handoff) + traveller("M60 328H206Q246 328 246 295", 20),
    },
    weave: {
      markup: trace(weave).replace(
        "</g>",
        traveller(around[2], 48) + traveller(around[6], 61, -21) + "</g>",
      ),
    },
  };
  writeFileSync(
    "apps/web/components/design-lab/refined/linework-art.ts",
    `// Original SVG geometry. Static assets live in public/images/design-lab/linework.\nexport const lineworkArt = ${JSON.stringify(scenes, null, 2)} as const;\n\nexport type LineworkVariant = keyof typeof lineworkArt;\n`,
  );
  const manifest = JSON.parse(
    readFileSync(`${directory}/SOURCES.json`, "utf8"),
  );
  manifest.assets.push(
    {
      name: "sdk-embed",
      concept: "An agent embedded into a product through code",
      output: `${directory}/sdk-embed.svg`,
    },
    {
      name: "closing-weave",
      concept: "A woven orbital field of connected possibilities",
      output: `${directory}/closing-weave.svg`,
    },
  );
  manifest.generator = "apps/web/scripts/generate-refined-linework.mjs";
  manifest.motion =
    "Native CSS motion within the SVG: advancing dashed paths, traveling points, and a very slow six-degree drift in the closing weave. Static geometry is preserved for reduced motion.";
  writeFileSync(
    `${directory}/SOURCES.json`,
    JSON.stringify(manifest, null, 2) + "\n",
  );
  console.log(
    "Created handoff, closing weave, and five animated scene variants.",
  );
}
