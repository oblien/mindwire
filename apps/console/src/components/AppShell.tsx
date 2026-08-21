// The authenticated chrome: top nav · left capability-gated sidebar · center active surface · right
// chat rail (collapsible). Everything below the nav shares one `AppProvider` so the panels and the chat
// read the fleet, the active daemon, the selected agent, and liveness from context instead of
// prop-drilling. Navigation is real URL routing — the Console is `/`, and each daemon has its OWN page
// at `/daemons/:id` (not an inline expansion). Deep links work: the Hono server falls back to index.html
// for any non-API path, so `/daemons/abc` or `/models` load straight into that surface.
import { useState, type ComponentType, type ReactNode } from "react";
import { Routes, Route, Navigate, Outlet, useLocation, useNavigate } from "react-router-dom";

import { AppProvider, useApp } from "@/lib/app-context";
import { visibleNav, keyForPath, navItem, navPath, type ViewKey } from "@/lib/nav";
import type { AuthUser } from "@/lib/api";
import type { Capabilities, SessionStatus } from "@shared/api";

import { TopNav } from "@/components/TopNav";
import { Sidebar } from "@/components/Sidebar";
import { Chat } from "@/components/chat/Chat";

import { FleetPanel } from "@/components/panels/FleetPanel";
import { DaemonPage } from "@/components/panels/DaemonPage";
import { AgentPage } from "@/components/panels/AgentPage";
import { NotificationsPanel } from "@/components/panels/NotificationsPanel";
import { CapabilitiesPanel } from "@/components/panels/CapabilitiesPanel";
import { AgentAuthPanel } from "@/components/panels/AgentAuthPanel";
import { SettingsPanel } from "@/components/panels/SettingsPanel";
import { ModelsPanel } from "@/components/panels/ModelsPanel";
import { ProvidersPanel } from "@/components/panels/ProvidersPanel";
import { MemoryPanel } from "@/components/panels/MemoryPanel";
import { PromptsPanel } from "@/components/panels/PromptsPanel";
import { SubagentsPanel } from "@/components/panels/SubagentsPanel";
import { McpPanel } from "@/components/panels/McpPanel";
import { DoctorPanel } from "@/components/panels/DoctorPanel";

// Every surface except the Console (rendered at the route root) and the per-daemon page is a prop-less
// panel keyed by its nav slug — the same slug that is its URL path.
type PanelView = Exclude<ViewKey, "fleet">;

const PANELS: Record<PanelView, ComponentType> = {
  notifications: NotificationsPanel,
  capabilities: CapabilitiesPanel,
  auth: AgentAuthPanel,
  settings: SettingsPanel,
  models: ModelsPanel,
  providers: ProvidersPanel,
  memory: MemoryPanel,
  prompts: PromptsPanel,
  subagents: SubagentsPanel,
  mcp: McpPanel,
  doctor: DoctorPanel,
};

export function AppShell({
  status,
  onSession,
  user,
  onSignOut,
}: {
  status: SessionStatus;
  onSession: (status: SessionStatus) => void;
  user: AuthUser;
  onSignOut: () => void;
}) {
  return (
    <AppProvider status={status} setStatus={onSession}>
      <Routes>
        <Route element={<ShellLayout user={user} onSignOut={onSignOut} />}>
          <Route index element={<FleetPanel />} />
          <Route path="daemons/:id" element={<DaemonPage />} />
          <Route path="daemons/:id/agents/:agentId" element={<AgentPage />} />
          {(Object.keys(PANELS) as PanelView[]).map((key) => {
            const Panel = PANELS[key];
            return (
              <Route
                key={key}
                path={key}
                element={
                  <GatedSurface view={key}>
                    <Panel />
                  </GatedSurface>
                }
              />
            );
          })}
          {/* Providers, opened straight onto one provider — the destination of "Manage auth" on a
              Models group header, so a connection can be edited or removed from where you notice it.
              Must precede the catch-all, or the redirect swallows it. */}
          <Route
            path="providers/:providerId"
            element={
              <GatedSurface view="providers">
                <ProvidersPanel />
              </GatedSurface>
            }
          />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </AppProvider>
  );
}

// A capability guard for the agent-scoped panels: if the selected agent can't do this surface (e.g. the
// gated Models/MCP/Memory routes when the agent lacks them), bounce to the Console rather than render an
// empty panel — so a stale deep link never dead-ends.
function GatedSurface({ view, children }: { view: ViewKey; children: ReactNode }) {
  const { agent } = useApp();
  const caps: Capabilities | null = agent?.capabilities ?? null;
  const allowed = new Set(visibleNav(caps).map((n) => n.key));
  return allowed.has(view) ? <>{children}</> : <Navigate to="/" replace />;
}

// The persistent chrome around every route: top nav, left sidebar, the routed main surface (`Outlet`),
// and the collapsible chat rail. Only the center surface changes as you navigate.
function ShellLayout({ user, onSignOut }: { user: AuthUser; onSignOut: () => void }) {
  const navigate = useNavigate();
  const location = useLocation();
  const { activeDaemon, activeAgentId } = useApp();
  const [chatOpen, setChatOpen] = useState(true);

  const activeKey = keyForPath(location.pathname);
  // Remount key for the routed surface: the config scope is (active daemon, active agent). The panels run
  // their OWN agent-scoped fetches (models, config, providers…), whose dep arrays don't track the switcher,
  // so without this a sidebar agent/daemon switch would leave the current page showing the previous agent's
  // data until a manual navigate/refresh. Keying the Outlet on the scope remounts the panel — and only the
  // panel — on a switch, so it re-fetches. The chat sits OUTSIDE this (it's unlinked from the config agent),
  // so a live conversation is never disturbed. Plain route navigation keeps the same key (no remount).
  const scopeKey = `${activeDaemon?.id ?? "none"}:${activeAgentId ?? ""}`;
  // The nav's runtime selector (daemon) only belongs on agent-scoped surfaces — Console, Agents, and a
  // daemon's own page are eagle-view and transcend a single agent, so they hide it. (The agent picker
  // lives in the sidebar's Agent-group header, present on every surface.)
  const contextScoped = navItem(activeKey)?.agentScoped ?? false;

  return (
    <div className="flex h-dvh flex-col overflow-hidden">
      <TopNav
        onHome={() => navigate("/")}
        chatOpen={chatOpen}
        onToggleChat={() => setChatOpen((v) => !v)}
        contextScoped={contextScoped}
        user={user}
        onSignOut={onSignOut}
      />
      <div className="flex min-h-0 flex-1">
        <Sidebar active={activeKey} onSelect={(view) => navigate(navPath(view))} />
        <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <Outlet key={scopeKey} />
        </main>
        {chatOpen && (
          <aside className="flex w-[26rem] shrink-0 flex-col overflow-hidden border-l border-border">
            {/* The chat owns its target switcher (which agent the conversation runs against) — separate
                from the nav's runtime selector and the sidebar's config-agent switcher. They're different
                axes, so the chat header always shows its own, regardless of the current surface. */}
            <Chat />
          </aside>
        )}
      </div>
    </div>
  );
}
