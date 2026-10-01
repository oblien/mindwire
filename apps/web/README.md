# @mindwire/web

Mindwire’s mobile product site and developer documentation. Built with Next.js App
Router, React, Tailwind CSS, and Fumadocs. The Node deployment uses Next’s
`standalone` output.

## Develop and build

Run `bun install` at the repository root, then from this directory:

```sh
bun run dev          # Fumadocs + Next development server
bun run check        # TypeScript
bun run build        # Generate reference docs, then production build
bun run serve        # Run the production site on port 4327
```

The web build generates REST, TypeScript, Go, and agent-catalog reference pages.
Build the TypeScript SDK first when starting from a clean checkout:

```sh
bun --filter=mindwire run build
bun --filter=@mindwire/web run build
```

The Docker build does both. It needs a running Node server; `out/` static exports
are not used.

## Product pages

- `/`: iPhone product overview, interactive sample app, workspace switching,
  connection choices, and the open-source runtime/SDK/Console.
- `/get-started/`: iPhone download and computer/server/cloud setup.
- `/pricing/`: own-machine runtime costs and current Oblien cloud plans.
- `/docs/`: developer documentation and generated references.

Shared product URLs live in `lib/product.ts`. Marketing styles are scoped in
`components/marketing/`; the docs theme stays in `app/globals.css`.
The app preview uses fictional example content and local interactions only.

Product pages keep the existing navigation, footer, neutral palette, and `Corners`
crosshair marks. New sections follow the site's original type scale and square
buttons; the iPhone illustration retains its native rounded controls.
The desktop preview scales with browser height. Check the complete hero at
1512 × 850 and 1280 × 720, including its headline, actions, phone, and preview tabs.
The workflow walkthrough advances only while visible, pauses on hover or focus,
and stays on a step after manual selection. It includes a play/pause control,
keyboard tab navigation, and a static presentation for reduced-motion preferences.

The social preview is the checked-in `app/opengraph-image.png`. To change its
artwork, edit `scripts/gen-social-image.tsx` and run `bun run gen:social`. It is
served as a static image and needs no image-generation service at runtime.

### iPhone download link

Set `NEXT_PUBLIC_IOS_APP_URL` to the public HTTPS App Store or TestFlight URL before
building. Until it is set, **Download app** opens `/get-started/#iphone`, which
clearly says that the iPhone download is coming soon. There is no fake store link
or signup form.

Docker Compose passes this variable as a web build argument. With a direct Docker
build, pass `--build-arg NEXT_PUBLIC_IOS_APP_URL=...`. Rebuild the web image after
changing it. `NEXT_PUBLIC_CONSOLE_URL` configures the separate web Console link.

### Workspace pricing

`lib/workspace-pricing.ts` reads the public
`https://api.oblien.com/pricing/calculator` endpoint on the server. Next revalidates
it hourly. No account token is needed and visiting another page does not poll it.

Only active rates and account plans are displayed. Proposed compute cards from
`compute_rate_cards` are never used as quotes. Shared running pools and individual
workspace ceilings are labeled separately. Missing, malformed, or unavailable
pricing produces a link to Oblien’s prices instead of invented fallback amounts.

The Node process needs outbound HTTPS and write access to its `.next` cache and
prerendered routes for revalidation. The web Docker image gives its unprivileged
`node` user ownership of those files.

## Documentation

Author `.mdx` files in `content/docs/` with `title` and `description` frontmatter.
Add the page to its directory’s `meta.json` to place it in the navigation. Generated
reference pages and `.source/` are build outputs; edit their source generators.

Fumadocs serves the search, raw markdown, and `llms.txt` routes. A page such as
`/docs/guides/project-sync/` also has a markdown view at
`/docs/guides/project-sync.md`.
