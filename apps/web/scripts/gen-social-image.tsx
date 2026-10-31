import { writeFile, readFile } from "node:fs/promises";
import { renderToStaticMarkup } from "react-dom/server";
import SiteIllustration, {
  type SiteIllustrationKind,
} from "../components/site/SiteIllustration";
import { renderSocialImage } from "../lib/social-image";
import { heroHeadline, heroDescription, heroAgents } from "../lib/hero";

const pages: {
  route: string;
  title: string;
  description: string;
  label: string;
  art: SiteIllustrationKind;
  hero?: boolean;
}[] = [
  {
    route: "",
    title: heroHeadline.join("\n"),
    description: heroDescription.join("\n"),
    label: "Mindwire",
    art: "product",
    hero: true,
  },
  {
    route: "sdk",
    title: "The agent layer.\nAlready built.",
    description:
      "Bring capable agents into your product with one SDK and runtime.",
    label: "SDK & runtime",
    art: "code",
  },
  {
    route: "cloud",
    title: "Your work.\nAlways with you.",
    description:
      "A personal computer for your projects. Online, wherever you are.",
    label: "Mindwire Cloud",
    art: "cloud",
  },
  {
    route: "app",
    title: "Your work.\nWith you.",
    description:
      "Agents, files, and projects in one workspace. Pick up where you left off.",
    label: "Mindwire App",
    art: "devices",
  },
  {
    route: "use-cases",
    title: "What will\nyours do?",
    description:
      "Explore products with agents built in. Your interface. Their capabilities.",
    label: "Product examples",
    art: "product",
  },
  {
    route: "get-started",
    title: "Make room for\nyour next idea.",
    description: "Set up Mindwire, connect your agent, and get to work.",
    label: "Get started",
    art: "devices",
  },
  {
    route: "developers",
    title: "Your project.\nWithin reach.",
    description:
      "Files, changes, and agent work. Keep building from wherever you are.",
    label: "Developer mode",
    art: "code",
  },
  {
    route: "sandboxing",
    title: "A place to work.\nRoom to build.",
    description:
      "Give agents an isolated workspace, with the tools and boundaries you choose.",
    label: "Sandboxing",
    art: "context",
  },
  {
    route: "changelog",
    title: "Always\nmoving forward.",
    description: "New releases, improvements, and updates from Mindwire.",
    label: "Changelog",
    art: "context",
  },
];

async function artwork(kind: SiteIllustrationKind) {
  if (kind === "cloud" || kind === "devices") {
    const svg = renderToStaticMarkup(<SiteIllustration kind={kind} />).replace(
      "<svg ",
      '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="400" ',
    );
    return `data:image/svg+xml;base64,${Buffer.from(svg).toString("base64")}`;
  }
  const names = {
    code: "sdk-embed",
    context: "context-sources",
    identity: "identity-system",
    product: "product-frame",
  };
  const data = await readFile(
    new URL(
      `../public/images/design-lab/linework/${names[kind]}.svg`,
      import.meta.url,
    ),
    "base64",
  );
  return `data:image/svg+xml;base64,${data}`;
}

async function main() {
  const agents = await Promise.all(
    heroAgents.map(async (agent) => {
      const file =
        agent.id === "codex" || agent.id === "claude"
          ? `${agent.id}-color`
          : agent.id === "opencode"
            ? "opencode-light"
            : agent.id;
      const svg = await readFile(
        new URL(`../public/logos/${file}.svg`, import.meta.url),
        "base64",
      );
      return { name: agent.name, image: `data:image/svg+xml;base64,${svg}` };
    }),
  );
  for (const page of pages) {
    const response = await renderSocialImage({
      ...page,
      artwork: await artwork(page.art),
      agents: page.hero ? agents : undefined,
    });
    const directory = new URL(
      `../app/${page.route ? page.route + "/" : ""}`,
      import.meta.url,
    );
    await writeFile(
      new URL("opengraph-image.png", directory),
      new Uint8Array(await response.arrayBuffer()),
    );
    await writeFile(
      new URL("opengraph-image.alt.txt", directory),
      `Mindwire — ${page.title.replace(/\n/g, " ")} ${page.description.replace(/\n/g, " ")}\n`,
    );
    console.log(`Generated /${page.route} social preview`);
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
