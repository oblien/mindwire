import { useRef, useState } from "react";
import { Link, Navigate, NavLink, Route, Routes, useLocation } from "react-router-dom";
import {
  ArrowUpRight,
  CreditCard,
  LayoutDashboard,
  LogOut,
  Monitor,
  Menu,
  X,
  Smartphone,
  UserRound,
} from "lucide-react";
import type { AuthUser } from "@/lib/api";
import { CloudProvider, useCloud } from "@/lib/cloud";
import { ThemeButton } from "@/components/ThemeButton";
import styles from "./portal.module.css";
import { BrandMark } from "@/components/BrandMark";
import { Button } from "@/components/ui/button";
import { CloudComputers, NewComputer, Billing } from "../CloudComputers";
import { Overview } from "./Overview";
import { Account } from "./Account";
import { Devices } from "./Devices";
import { ComputerDetail } from "./ComputerDetail";
import { ErrorNotice } from "./common";

const navigation = [
  { to: "/", label: "Overview", icon: LayoutDashboard },
  { to: "/computers", label: "Cloud Computers", icon: Monitor },
  { to: "/devices", label: "Devices", icon: Smartphone },
  { to: "/billing", label: "Plans & billing", icon: CreditCard },
  { to: "/account", label: "Account", icon: UserRound },
];
export function Portal({
  user,
  onUpdated,
  onSignOut,
  onSignedOut,
}: {
  user: AuthUser;
  onUpdated: () => Promise<void>;
  onSignOut: () => Promise<void>;
  onSignedOut: () => void;
}) {
  return (
    <CloudProvider>
      <PortalShell
        user={user}
        onUpdated={onUpdated}
        onSignOut={onSignOut}
        onSignedOut={onSignedOut}
      />
    </CloudProvider>
  );
}
function PortalShell({
  user,
  onUpdated,
  onSignOut,
  onSignedOut,
}: Parameters<typeof Portal>[0]) {
  const cloud = useCloud();
  const location = useLocation();
  const purchaseError = cloud.computers.find(computer => computer.purchase && location.pathname === `/computers/${computer.id}`)?.error;
  const [menuOpen, setMenuOpen] = useState(false);
  const menuTrigger = useRef<HTMLButtonElement>(null);
  const currentPage = navigation.find((item) => item.to === "/" ? location.pathname === "/" : location.pathname.startsWith(item.to))?.label ?? "Your account";
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  return (
    <div className={styles.shell} onKeyDown={event => { if (event.key === "Escape" && menuOpen) { setMenuOpen(false); menuTrigger.current?.focus(); } }}>
      <a href="#account-content" className={styles.skip}>Skip to content</a>
      <aside className={styles.sidebar}>
        <div className={styles.brand}>
          <div><Link to="/" aria-label="Mindwire home" onClick={() => setMenuOpen(false)}><BrandMark size={22} /></Link><p className={styles.accountType}>Personal account</p></div>
          <Button ref={menuTrigger} size="icon" variant="ghost" className={styles.menuButton} aria-label={menuOpen ? "Close navigation" : "Open navigation"} aria-expanded={menuOpen} aria-controls="account-navigation" onClick={() => setMenuOpen(!menuOpen)}>{menuOpen ? <X /> : <Menu />}</Button>
        </div>
        <nav id="account-navigation" aria-label="Account navigation" className={styles.nav} data-open={menuOpen}>
          {navigation.map((item) => <NavLink end={item.to === "/"} key={item.to} to={item.to} onClick={() => setMenuOpen(false)}><item.icon className="size-[18px]" />{item.label}</NavLink>)}
        </nav>
        <div className={styles.sidebarBottom} data-open={menuOpen}>
          <a href="https://oblien.com/" target="_blank" rel="noreferrer" className={styles.help}>Building for customers?<ArrowUpRight className="size-4" /></a>
          <a href="https://mindwire.sh/docs" target="_blank" rel="noreferrer" className={styles.help}>Help & documentation<ArrowUpRight className="size-4" /></a>
          <div className={styles.profile}>
            <span className={styles.avatar} aria-hidden="true">{(user.name || user.email).slice(0,2).toUpperCase()}</span>
            <div><p>{user.name || "Your account"}</p><p>{user.email}</p></div>
          </div>
        </div>
      </aside>
      <div className={styles.workspace}>
        <header className={styles.topbar}>
          <p className={styles.location}><span>Personal workspace /</span><strong>{currentPage}</strong></p>
          <div className={styles.headerActions}>
            <ThemeButton />
          <Button
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              setError("");
              try {
                await onSignOut();
              } catch (error) {
                setError((error as Error).message);
              } finally {
                setBusy(false);
              }
            }}
          >
            <LogOut className="size-3.5" />
            Sign out
          </Button>
          </div>
        </header>
        <main id="account-content" tabIndex={-1} className={styles.main}>
          {error && <ErrorNotice message={error} />}
          {cloud.error && cloud.error !== purchaseError && (
            <ErrorNotice
              message={cloud.error}
              retry={() => void cloud.retryRecovery().catch(() => {})}
            />
          )}
          <Routes>
            <Route path="/" element={<Overview user={user} />} />
            <Route path="/computers" element={<CloudComputers />} />
            <Route path="/computers/new" element={<NewComputer />} />
            <Route path="/computers/:id" element={<ComputerDetail />} />
            <Route path="/billing" element={<Billing />} />
            <Route
              path="/devices"
              element={<Devices onSignedOut={onSignedOut} />}
            />
            <Route
              path="/account"
              element={<Account user={user} onUpdated={onUpdated} onDeleted={onSignedOut} />}
            />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
      </div>
    </div>
  );
}
