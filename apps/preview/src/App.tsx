// The console is multi-user and fully session-protected. Boot order is: resolve *who* you are (Better
// Auth, `api.account.me`) → if nobody, show the login gate → once signed in, resolve *your* isolated
// console session (`api.session`) and drop into the shell. Each user's fleet, Oblien link, and API keys
// live server-side under their user id, never crossing between users or reaching the browser.
import { useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";

import { api, type AuthUser } from "@/lib/api";
import { AppShell } from "@/components/AppShell";
import { AuthScreen } from "@/components/AuthScreen";
import { Button } from "@/components/ui/button";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { SessionStatus } from "@shared/api";

export default function App() {
  // `undefined` = still checking who you are; `null` = signed out (show the gate); a user = signed in.
  const [user, setUser] = useState<AuthUser | null | undefined>(undefined);
  const [status, setStatus] = useState<SessionStatus | null>(null);
  const [failed, setFailed] = useState(false);
  const [loading, setLoading] = useState(false);

  // Once we know who the user is, resolve their console session (the server keys it by user id).
  const bootstrapSession = useCallback(() => {
    setLoading(true);
    setFailed(false);
    api
      .session()
      .then(setStatus)
      .catch(() => setFailed(true))
      .finally(() => setLoading(false));
  }, []);

  // On first load, ask the server who we are. No user → the gate; a user → bootstrap their session.
  useEffect(() => {
    api.account
      .me()
      .then((res) => setUser(res?.user ?? null))
      .catch(() => setUser(null));
  }, []);

  useEffect(() => {
    if (user) bootstrapSession();
  }, [user, bootstrapSession]);

  const onAuthed = useCallback((u: AuthUser) => {
    setUser(u);
  }, []);

  // Sign out clears the Better Auth session server-side, then drops back to the gate.
  const onSignOut = useCallback(() => {
    void api.account.signOut().finally(() => {
      setUser(null);
      setStatus(null);
    });
  }, []);

  return (
    <TooltipProvider>
      {user === undefined ? (
        <div className="grid min-h-dvh place-items-center">
          <Loader2 className="size-5 animate-spin text-muted-foreground" />
        </div>
      ) : user === null ? (
        <AuthScreen onAuthed={onAuthed} />
      ) : loading ? (
        <div className="grid min-h-dvh place-items-center">
          <Loader2 className="size-5 animate-spin text-muted-foreground" />
        </div>
      ) : status?.ready ? (
        <AppShell status={status} onSession={setStatus} user={user} onSignOut={onSignOut} />
      ) : (
        <div className="grid min-h-dvh place-items-center px-4 text-center">
          <div className="space-y-3">
            <p className="text-sm text-muted-foreground">
              {failed ? "Couldn’t reach the server." : "No session."}
            </p>
            <Button variant="outline" size="sm" onClick={bootstrapSession}>
              Retry
            </Button>
          </div>
        </div>
      )}
      <Toaster />
    </TooltipProvider>
  );
}
