// The top chrome. The Oblien mark is the home affordance (Oblien is the parent product), then the
// product wordmark. When the current surface is *agent-scoped* (Capabilities, Settings, Models, …), the
// nav also hosts the runtime selector — the daemon everything below is scoped to (the agent is picked in
// the sidebar's Agent-group header). On eagle-view surfaces (Console, Agents, a daemon's page) that
// selector is hidden: those transcend a single agent. On the right: an external Docs link, the chat-rail
// toggle, — once an Oblien account
// is linked — its account chip with an unlink action, and finally the signed-in user with sign-out.
// The console is multi-user and session-protected: `user` is the authenticated identity everything
// below is isolated under, and `onSignOut` ends that Better Auth session and returns to the login gate.
import { ArrowUpRight, LogOut, Moon, PanelRightClose, PanelRightOpen, Sun } from "lucide-react";

import { api, type AuthUser } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { OblienMark } from "@/components/OblienMark";
import { ContextSwitcher } from "@/components/ContextSwitcher";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { toast } from "@/components/ui/sonner";

const DOCS_URL = "https://mindwire.sh/docs";

export function TopNav({
  onHome,
  chatOpen,
  onToggleChat,
  contextScoped,
  user,
  onSignOut,
}: {
  onHome: () => void;
  chatOpen: boolean;
  onToggleChat: () => void;
  /** Show the runtime selector — true only on agent-scoped surfaces. */
  contextScoped: boolean;
  /** The authenticated user everything below is isolated under. */
  user: AuthUser;
  /** End the Better Auth session and drop back to the login gate. */
  onSignOut: () => void;
}) {
  const { status, setStatus } = useApp();
  const { theme, toggle: toggleTheme } = useTheme();

  async function unlinkOblien() {
    try {
      setStatus(await api.disconnectOblien());
      toast.success("Oblien account unlinked");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not unlink");
    }
  }

  return (
    <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border px-4">
      <button
        type="button"
        className="flex items-center rounded-md outline-none transition-opacity hover:opacity-80 focus-visible:ring-1 focus-visible:ring-ring"
        onClick={onHome}
        aria-label="Home"
      >
        <OblienMark />
      </button>
      <Separator orientation="vertical" className="h-5" />
      <span className="text-sm font-medium tracking-tight">
        MindWire <span className="text-muted-foreground">console</span>
      </span>

      {contextScoped && (
        <div className="ml-auto flex items-center gap-2.5">
          <span className="hidden text-[0.65rem] font-medium uppercase tracking-wider text-muted-foreground md:inline">
            Runtime
          </span>
          <ContextSwitcher />
        </div>
      )}

      <div className={cn("flex items-center gap-1", !contextScoped && "ml-auto")}>
        {contextScoped && <Separator orientation="vertical" className="mx-1 h-5" />}
        <a
          href={DOCS_URL}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 px-2 py-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
        >
          Docs
          <ArrowUpRight className="size-3.5" />
        </a>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" onClick={toggleTheme} aria-label="Toggle theme">
              {theme === "dark" ? <Sun className="size-4" /> : <Moon className="size-4" />}
            </Button>
          </TooltipTrigger>
          <TooltipContent>{theme === "dark" ? "Light mode" : "Dark mode"}</TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" onClick={onToggleChat} aria-label="Toggle chat">
              {chatOpen ? (
                <PanelRightClose className="size-4" />
              ) : (
                <PanelRightOpen className="size-4" />
              )}
            </Button>
          </TooltipTrigger>
          <TooltipContent>{chatOpen ? "Hide chat" : "Show chat"}</TooltipContent>
        </Tooltip>

        {status.oblien && (
          <>
            <Separator orientation="vertical" className="mx-1 h-5" />
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={unlinkOblien}
                  className="group inline-flex items-center gap-1.5 px-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
                  aria-label="Unlink Oblien account"
                >
                  <OblienMark showWord={false} size={18} />
                  <span className="hidden font-mono sm:inline">{status.oblien.label}</span>
                  <LogOut className="size-3.5 opacity-0 transition-opacity group-hover:opacity-100" />
                </button>
              </TooltipTrigger>
              <TooltipContent>Unlink Oblien account</TooltipContent>
            </Tooltip>
          </>
        )}

        <Separator orientation="vertical" className="mx-1 h-5" />
        <span className="hidden max-w-[12rem] truncate text-xs text-muted-foreground sm:inline">
          {user.email}
        </span>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" onClick={onSignOut} aria-label="Sign out">
              <LogOut className="size-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Sign out</TooltipContent>
        </Tooltip>
      </div>
    </header>
  );
}
