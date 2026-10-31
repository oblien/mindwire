import { productAgents } from "./product";

export const heroHeadline = [
  "The best agents.",
  "Inside your product.",
] as const;
export const heroDescription = [
  "Give them your tools, your knowledge, and your interface.",
  "Mindwire brings it together.",
] as const;

export const heroAgents = [
  ...productAgents.slice(0, 3),
  { id: "antigravity", name: "Antigravity", href: "/docs/guides/agents" },
  productAgents[3],
] as const;
