// The sidebar's nav model: which surfaces exist, how they group, and how each is capability-gated.
// **Fleet** (Console) opens the list — it manages the session's runtimes and, on a runtime's own page,
// lists the agents deployed on it (there is no separate fleet-wide Agents surface; a runtime owns its
// agents). The rest are *agent-scoped*: they configure the currently-selected adapter, and the
// capability-gated ones only appear when that agent actually supports them (the daemon's own gating).
import {
  Gauge,
  Bell,
  KeyRound,
  LockKeyhole,
  Settings2,
  Cpu,
  Plug,
  FileText,
  Terminal,
  Users,
  Puzzle,
  Stethoscope,
  type LucideIcon,
} from "lucide-react";

import type { Capabilities } from "@shared/api";

export type ViewKey =
  | "fleet"
  | "notifications"
  | "capabilities"
  | "auth"
  | "secrets"
  | "settings"
  | "models"
  | "providers"
  | "memory"
  | "prompts"
  | "subagents"
  | "mcp"
  | "doctor";

/** The nav group whose header hosts the agent switcher (see Sidebar / AgentSwitcher) rather than a plain
 *  label — the surfaces under it all configure the currently-selected adapter. */
export const AGENT_GROUP = "Agent";

/** The eyebrow shown above that group's switcher. The group is keyed internally by AGENT_GROUP ("Agent"),
 *  but presented to the user as this label — it names the group's *function* (configuring the selected
 *  adapter) so it parallels the other groups' eyebrows ("Deployment", "Diagnostics"). Not "Settings":
 *  that collides with the Settings surface inside the group. */
export const AGENT_GROUP_LABEL = "Configuration";

export interface NavItem {
  key: ViewKey;
  label: string;
  icon: LucideIcon;
  group: string;
  /** Gate against the selected agent's capabilities. Absent → always shown. */
  enabled?: (caps: Capabilities) => boolean;
  /** Operates on the currently-selected adapter (drives the "for <agent>" header hint). */
  agentScoped?: boolean;
}

export const NAV: NavItem[] = [
  { key: "fleet", label: "Console", icon: Gauge, group: "Deployment" },
  { key: "secrets", label: "Secrets", icon: LockKeyhole, group: "Deployment" },
  // Daemon-WIDE notification routing (channels + rules). No capability gate — it's a property of the
  // runtime, not the adapter. `agentScoped` shows the context switcher so the user sees which runtime
  // they're configuring and agent-scoped rules can default to the selected adapter.
  {
    key: "notifications",
    label: "Notifications",
    icon: Bell,
    group: "Deployment",
    agentScoped: true,
  },

  { key: "capabilities", label: "Capabilities", icon: Puzzle, group: AGENT_GROUP, agentScoped: true },
  { key: "auth", label: "Agent auth", icon: KeyRound, group: AGENT_GROUP, agentScoped: true },
  { key: "settings", label: "Settings", icon: Settings2, group: AGENT_GROUP, agentScoped: true },
  {
    key: "models",
    label: "Models",
    icon: Cpu,
    group: AGENT_GROUP,
    agentScoped: true,
    enabled: (c) => c.models,
  },
  // No capability gate: Providers is the models.dev catalog browser (reference data the SDK fetches
  // live), which every agent can browse. Only *storing a key* for a provider is capability-gated — that
  // path (the daemon's provider registry) is disabled inside the panel when the agent lacks it.
  {
    key: "providers",
    label: "Providers",
    icon: Plug,
    group: AGENT_GROUP,
    agentScoped: true,
  },
  {
    key: "memory",
    label: "Memory",
    icon: FileText,
    group: AGENT_GROUP,
    agentScoped: true,
    enabled: (c) => c.memory,
  },
  {
    key: "prompts",
    label: "Prompts",
    icon: Terminal,
    group: AGENT_GROUP,
    agentScoped: true,
    enabled: (c) => c.promptTemplates,
  },
  {
    key: "subagents",
    label: "Subagents",
    icon: Users,
    group: AGENT_GROUP,
    agentScoped: true,
    enabled: (c) => c.subagentDefs,
  },
  {
    key: "mcp",
    label: "MCP",
    icon: Plug,
    group: AGENT_GROUP,
    agentScoped: true,
    enabled: (c) => c.mcpConfig,
  },

  { key: "doctor", label: "Doctor", icon: Stethoscope, group: "Diagnostics", agentScoped: true },
];

/** The nav items visible for a given capability set (null caps = only the un-gated items). */
export function visibleNav(caps: Capabilities | null): NavItem[] {
  return NAV.filter((n) => !n.enabled || (caps ? n.enabled(caps) : false));
}

/** URL path for a nav surface. The Console (`fleet`) is the app root; every other key is its own path. */
export function navPath(key: ViewKey): string {
  return key === "fleet" ? "/" : `/${key}`;
}

/** URL path for ONE provider's page on the Providers surface. `keyForPath` splits on "/", so this keeps
 *  the Providers nav item lit — it's the same surface, opened straight onto a provider. */
export function providerPath(id: string): string {
  return `/providers/${encodeURIComponent(id)}`;
}

/** Map a URL pathname back to the nav key it highlights. A daemon page (`/daemons/:id`) sits under the
 *  Console, so it keeps `fleet` lit; anything unrecognized also falls back to the Console. */
export function keyForPath(pathname: string): ViewKey {
  const seg = pathname.replace(/^\/+/, "").split("/")[0] ?? "";
  if (seg === "" || seg === "daemons") return "fleet";
  const hit = NAV.find((n) => n.key === seg);
  return hit ? hit.key : "fleet";
}

/** Look up a nav item by key (for header labels). */
export function navItem(key: ViewKey): NavItem | undefined {
  return NAV.find((n) => n.key === key);
}
