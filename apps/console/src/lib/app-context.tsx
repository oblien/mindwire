// Shared authenticated-session state for the whole shell. A session owns a **fleet** of daemons; one is
// *active* at a time, and within it one *agent* (adapter type) is selected. Everything below the nav
// reads from here instead of prop-drilling:
//   • fleet + active daemon      — the deployment surface (where daemons run, how many agents each has)
//   • active agent (adapter)     — which adapter the agent-scoped CONFIG panels target (sidebar-driven)
//   • chat agent (adapter)       — which adapter the CHAT talks to; UNLINKED from the config agent, so
//                                  configuring one adapter never re-points a live conversation on another
//   • agent / health / catalog   — resolved live against the active daemon (+ active/chat agent)
//   • provider availability      — what the Add-daemon dialog may offer
//   • chatId                      — a thread id per (daemon, CHAT agent) pair, so switching gives a clean rail
//
// Ordering note: `setActiveAgent(activeAgentId)` runs in the render body so the api layer's `?agent=`
// scoping is set *before* the effect-driven loaders below fire — otherwise the first agent-scoped fetch
// after a switch would race with the wrong (or no) agent.
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { api, setActiveAgent } from "@/lib/api";
import { useAsync } from "@/lib/useAsync";
import type {
  AgentInfo,
  Catalog,
  ChatSummary,
  DaemonView,
  FleetView,
  Health,
  ProviderAvailability,
  SessionStatus,
  UsageReport,
} from "@shared/api";

export interface AppState {
  status: SessionStatus;
  setStatus: (s: SessionStatus) => void;

  // ---- fleet ----
  fleet: FleetView | null;
  fleetError: string | null;
  fleetLoading: boolean;
  /** Refetch the fleet from the server. */
  reloadFleet: () => void;
  /** Commit a fresh fleet returned by a mutation (activate/add/duplicate/remove/down/provision). */
  applyFleet: (next: FleetView) => void;
  /** The currently-selected daemon (turns + config target this one), or null before the fleet loads. */
  activeDaemon: DaemonView | null;

  // ---- active agent (adapter type on the active daemon) — the CONFIG scope ----
  /** Selected adapter id for the config surfaces, or `undefined` to use the daemon's default agent. */
  activeAgentId: string | undefined;
  setActiveAgentId: (id: string | undefined) => void;
  /** Select a (daemon, agent) pair in one move — activates the daemon if needed, then pins the agent
   *  (without the daemon-switch auto-reset clobbering it). Used by the fleet-wide Agents roster. */
  setActiveContext: (daemonId: string, agentId?: string) => Promise<void>;

  // ---- chat agent (adapter the CHAT talks to) — UNLINKED from the config agent above ----
  /** Adapter id the chat runs turns against, or `undefined` for the daemon's default agent. Independent
   *  of {@link activeAgentId}: the chat header's switcher writes this; the sidebar's writes the config one. */
  chatAgentId: string | undefined;
  setChatAgentId: (id: string | undefined) => void;
  /** The chat agent's capabilities/schema/auth — resolved live for `chatAgentId` (not the config agent), so
   *  the chat gates its controls (input/interrupt/vision…) on the agent it's actually talking to. */
  chatAgent: AgentInfo | null;

  // ---- live introspection of the active daemon ----
  /** The active agent's capabilities/schema/auth. `null` until resolved (or when the daemon is off). */
  agent: AgentInfo | null;
  agentError: string | null;
  agentLoading: boolean;
  reloadAgent: () => void;
  /** Adapter types the active daemon hosts (drives the agent switcher). */
  catalog: Catalog | null;
  reloadCatalog: () => void;
  /** Active-daemon liveness for the header dot. */
  health: Health | null;
  reloadHealth: () => void;

  // ---- deployment options ----
  providers: ProviderAvailability | null;

  // ---- fleet-wide token accounting (the monitoring console's spend view) ----
  /** Cumulative per-(daemon, agent) token usage across the session, or null before first load. */
  usage: UsageReport | null;
  /** Refetch usage — called when a turn settles so the console reflects the latest spend. */
  reloadUsage: () => void;

  // ---- chat threads (on the active daemon) ----
  /** The selected chat id — the thread the rail shows and the next turn targets. Starts as a fresh
   *  per-(daemon, CHAT agent) id, but `selectChat`/`newChat` can move it to any thread. */
  chatId: string;
  /** Switch the rail to an existing recorded chat. */
  selectChat: (id: string) => void;
  /** Start a brand-new thread (mints a fresh id and selects it) on the current (daemon, CHAT agent) pair. */
  newChat: () => void;
  /** Recorded chats on the active daemon, newest first (drives the chat list). */
  chats: ChatSummary[];
  chatsLoading: boolean;
  /** Refetch the chat list — called when a turn settles (a new thread may have been recorded). */
  reloadChats: () => void;
}

const Ctx = createContext<AppState | null>(null);

export function AppProvider({
  status,
  setStatus,
  children,
}: {
  status: SessionStatus;
  setStatus: (s: SessionStatus) => void;
  children: ReactNode;
}) {
  // ---- fleet (owned here so mutations can commit a fresh view without a round-trip) ----
  const [fleet, setFleet] = useState<FleetView | null>(null);
  const [fleetError, setFleetError] = useState<string | null>(null);
  const [fleetLoading, setFleetLoading] = useState(true);

  const reloadFleet = useCallback(() => {
    setFleetLoading(true);
    api
      .fleet()
      .then((f) => {
        setFleet(f);
        setFleetError(null);
      })
      .catch((e: unknown) => setFleetError(e instanceof Error ? e.message : String(e)))
      .finally(() => setFleetLoading(false));
  }, []);

  const applyFleet = useCallback((next: FleetView) => {
    setFleet(next);
    setFleetError(null);
    setFleetLoading(false);
  }, []);

  useEffect(() => reloadFleet(), [reloadFleet]);

  const activeDaemon = useMemo<DaemonView | null>(() => {
    if (!fleet) return null;
    return (
      fleet.daemons.find((d) => d.id === fleet.activeDaemonId) ??
      fleet.daemons.find((d) => d.active) ??
      fleet.daemons[0] ??
      null
    );
  }, [fleet]);

  // ---- active agent ----
  const [activeAgentId, setActiveAgentId] = useState<string | undefined>(undefined);

  // Switching daemons drops the agent selection back to that daemon's default — UNLESS the switch came
  // from `setActiveContext`, which is deliberately pinning an agent on the daemon it's activating.
  const skipAgentReset = useRef(false);
  useEffect(() => {
    if (skipAgentReset.current) {
      skipAgentReset.current = false;
      return;
    }
    setActiveAgentId(undefined);
  }, [activeDaemon?.id]);

  // Pick a (daemon, agent) pair in one move. Activating a daemon and pinning an agent are two state
  // updates; React batches them into one commit, so we flag the reset effect to stand down for that
  // commit — otherwise it would fire on the daemon-id change and wipe the agent we just set.
  const setActiveContext = useCallback(
    async (daemonId: string, agentId?: string) => {
      if (daemonId !== activeDaemon?.id) {
        skipAgentReset.current = true;
        try {
          applyFleet(await api.activateDaemon(daemonId));
        } catch (e) {
          skipAgentReset.current = false;
          throw e;
        }
      }
      setActiveAgentId(agentId);
    },
    [activeDaemon?.id, applyFleet],
  );

  // ---- chat agent — the adapter the CHAT talks to, independent of the config scope above ----
  // Its own selection so configuring one adapter (sidebar) never re-points a live conversation on another.
  // Like the config agent it falls back to the daemon default when unset, and resets on a runtime switch
  // (the daemon is session-global) — but nothing here touches the api layer's `?agent=` module var, which
  // stays bound to the config scope; the chat passes its agent EXPLICITLY on each turn / history read.
  const [chatAgentId, setChatAgentId] = useState<string | undefined>(undefined);
  useEffect(() => {
    setChatAgentId(undefined);
  }, [activeDaemon?.id]);

  // Sync the api layer's scope *now*, during render, so the loaders below pick it up on this pass. This is
  // the CONFIG agent only — the chat never rides this module var (see above).
  setActiveAgent(activeAgentId);

  // ---- live introspection, re-keyed on (daemon, agent, readiness) ----
  const ready = !!activeDaemon && activeDaemon.state === "ready";
  const scopeKey = `${activeDaemon?.id ?? "none"}:${activeAgentId ?? ""}:${activeDaemon?.state ?? "none"}`;

  const agentQ = useAsync<AgentInfo | null>(
    () => (ready ? api.agent() : Promise.resolve(null)),
    [scopeKey],
  );
  // The chat agent's live introspection — keyed on the CHAT agent, addressed explicitly via `agentFor`
  // (not the module-level config scope), so the two agents' capabilities never cross-contaminate.
  const chatScopeKey = `${activeDaemon?.id ?? "none"}:${chatAgentId ?? ""}:${activeDaemon?.state ?? "none"}`;
  const chatAgentQ = useAsync<AgentInfo | null>(
    () => (ready ? api.agentFor(chatAgentId) : Promise.resolve(null)),
    [chatScopeKey],
  );
  const healthQ = useAsync<Health | null>(
    () => (ready ? api.health() : Promise.resolve(null)),
    [scopeKey],
  );
  const catalogQ = useAsync<Catalog | null>(
    () => (ready ? api.catalog() : Promise.resolve(null)),
    // Catalog is agent-independent — key only on the daemon + readiness.
    [`${activeDaemon?.id ?? "none"}:${activeDaemon?.state ?? "none"}`],
  );

  const providersQ = useAsync<ProviderAvailability>(() => api.providerAvailability(), []);

  // Fleet-wide token accounting. Loaded once, then refetched on demand (`reloadUsage`) whenever a turn
  // settles, so the monitoring console's spend view tracks activity without polling.
  const usageQ = useAsync<UsageReport>(() => api.usage(), []);

  // ---- per-(daemon, CHAT agent) chat thread ids ----
  // Each (daemon, CHAT agent) pair keeps a "current thread" id in a ref so switching the chat's target
  // gives a clean rail and coming back returns to where you were. Keyed on the CHAT agent (not the config
  // one), so re-scoping the sidebar leaves the live thread untouched. `selectChat`/`newChat` mutate that
  // ref and bump a nonce to re-render (the id is read from the ref during render, so the bump surfaces it).
  const threadKey = `${activeDaemon?.id ?? "none"}:${chatAgentId ?? ""}`;
  const threads = useRef(new Map<string, string>());
  if (!threads.current.has(threadKey)) threads.current.set(threadKey, crypto.randomUUID());
  const [, bumpChat] = useState(0);
  const chatId = threads.current.get(threadKey)!;

  const selectChat = useCallback(
    (id: string) => {
      threads.current.set(threadKey, id);
      bumpChat((n) => n + 1);
    },
    [threadKey],
  );
  const newChat = useCallback(() => {
    threads.current.set(threadKey, crypto.randomUUID());
    bumpChat((n) => n + 1);
  }, [threadKey]);

  // The active daemon's recorded chats (shared across its agents). Loaded when the daemon is ready and
  // refetched on demand (a settled turn may have recorded a new thread). Empty while off/unreachable.
  const chatsQ = useAsync<ChatSummary[]>(
    () => (ready ? api.chats() : Promise.resolve([])),
    [`${activeDaemon?.id ?? "none"}:${activeDaemon?.state ?? "none"}`],
  );

  const value = useMemo<AppState>(
    () => ({
      status,
      setStatus,
      fleet,
      fleetError,
      fleetLoading,
      reloadFleet,
      applyFleet,
      activeDaemon,
      activeAgentId,
      setActiveAgentId,
      setActiveContext,
      chatAgentId,
      setChatAgentId,
      chatAgent: chatAgentQ.data,
      agent: agentQ.data,
      agentError: agentQ.error,
      agentLoading: agentQ.loading,
      reloadAgent: agentQ.reload,
      catalog: catalogQ.data,
      reloadCatalog: catalogQ.reload,
      health: healthQ.data,
      reloadHealth: healthQ.reload,
      providers: providersQ.data,
      usage: usageQ.data,
      reloadUsage: usageQ.reload,
      chatId,
      selectChat,
      newChat,
      chats: chatsQ.data ?? [],
      chatsLoading: chatsQ.loading,
      reloadChats: chatsQ.reload,
    }),
    [
      status,
      setStatus,
      fleet,
      fleetError,
      fleetLoading,
      reloadFleet,
      applyFleet,
      activeDaemon,
      activeAgentId,
      setActiveContext,
      chatAgentId,
      chatAgentQ,
      agentQ,
      catalogQ,
      healthQ,
      providersQ.data,
      usageQ,
      chatId,
      selectChat,
      newChat,
      chatsQ,
    ],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useApp(): AppState {
  const v = useContext(Ctx);
  if (!v) throw new Error("useApp must be used within an AppProvider");
  return v;
}
