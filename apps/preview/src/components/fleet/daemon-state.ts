// Shared visual vocabulary for a daemon's provider and lifecycle state, used by both the compact
// Console block and the full daemon page so the dot/label/icon read identically in both places.
// Status tones are the one place functional color is allowed — they encode state, not chrome:
// green = reachable, amber = coming up, red = failed, off = a white film (never gray).
import { Server, Container, Cloud, MonitorCog, Terminal, type LucideIcon } from "lucide-react";

import type { DaemonProvider, DaemonState } from "@shared/api";

export const PROVIDER_ICON: Record<DaemonProvider, LucideIcon> = {
  local: MonitorCog,
  remote: Server,
  ssh: Terminal,
  docker: Container,
  oblien: Cloud,
};

export const STATE_TONE: Record<DaemonState, string> = {
  ready: "bg-emerald-500",
  provisioning: "bg-amber-500",
  error: "bg-destructive",
  off: "bg-ink/30",
};

export const STATE_LABEL: Record<DaemonState, string> = {
  ready: "Ready",
  provisioning: "Provisioning",
  error: "Error",
  off: "Off",
};
