# Mindwire ribbon artwork

Original procedural ribbon sculptures created for this landing page. No stock
images or third-party artwork were used.

The shapes are projected three-dimensional ribbons with calculated surface
normals, lighting, and a custom coral, parchment, and cobalt palette. The
transparent WebP exports are used by the hero, product examples, and closing.

Rebuild from the repository root:

```sh
node apps/web/scripts/generate-landing-ribbons.mjs
```

The generator retains full SVG sources in `.cache/mindwire-ribbons/` and writes
optimized 1400-pixel WebP exports here. The document, action, and interface
illustrations are original SVG components in `refined/Artwork.tsx`.

## Satin material refinements

`ribbon-knot-satin-v2.webp` and `ribbon-loop-satin-v2.webp` refine the original
geometry with brushed satin surfaces. They were edited with the built-in
`image_gen` tool, retaining transparency. The full edit prompts and source
references are recorded in `../textures/SOURCES.json`. The generator above
rebuilds the original geometric renders; it does not overwrite the refinements.
