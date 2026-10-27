import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { ArrowLeft, ArrowUpRight, Check, Loader2, Mail } from "lucide-react";
import { Link, useLocation, useNavigate } from "react-router-dom";

import { api, HttpError, type AuthUser } from "@/lib/api";
import type { PublicConfig, SocialProvider } from "@shared/api";
import { BrandMark } from "@/components/BrandMark";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { AccountArtwork } from "./AccountArtwork";
import { ThemeButton } from "./ThemeButton";
import styles from "./auth.module.css";

type Mode = "signin" | "signup";

const DOCS_FALLBACK = "https://mindwire.sh/docs";
const GITHUB_FALLBACK = "https://github.com/oblien/mindwire";

export function AuthScreen({
  onAuthed,
  onPasswordReset,
}: {
  onAuthed: (user: AuthUser) => void;
  onPasswordReset?: () => void;
}) {
  const location = useLocation();
  const isRecovery = ["/forgot-password", "/reset-password"].includes(
    location.pathname,
  );
  const [mode, setMode] = useState<Mode>("signin");
  const [cfg, setCfg] = useState<PublicConfig | null>(null);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [social, setSocial] = useState<SocialProvider | null>(null);
  const [error, setError] = useState<string | null>(null);

  // The one pre-auth call: branding + which sign-in options to offer. Failure is non-fatal — the gate
  // simply falls back to email/password with default links.
  useEffect(() => {
    let alive = true;
    api.publicConfig().then(
      (c) => alive && setCfg(c),
      () => {},
    );
    return () => {
      alive = false;
    };
  }, []);

  const appName =
    !cfg?.appName || cfg.appName === "MindWire" ? "Mindwire" : cfg.appName;
  const socials = cfg?.socials ?? [];
  const docsUrl = cfg?.docsUrl ?? DOCS_FALLBACK;
  const githubUrl = cfg?.githubUrl ?? GITHUB_FALLBACK;
  const isSignup = mode === "signup";
  const locked = busy || social !== null;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    const identifier = email.trim();
    if (!identifier || !password) {
      setError("Email and password are required.");
      return;
    }
    if (isSignup && password.length < 8) {
      setError("Password must be at least 8 characters.");
      return;
    }
    setBusy(true);
    try {
      const accountEmail = identifier;
      const res = isSignup
        ? await api.account.signUp(
            accountEmail,
            password,
            name.trim() || accountEmail.split("@")[0]!,
          )
        : await api.account.signIn(accountEmail, password);
      onAuthed(res.user);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Authentication failed.");
    } finally {
      setBusy(false);
    }
  }

  // OAuth: ask the server for the provider's authorize URL, then hand the browser to it. On return the
  // session cookie is set and the app re-bootstraps as the signed-in user (no onAuthed needed here).
  async function onSocial(provider: SocialProvider) {
    setError(null);
    setSocial(provider);
    try {
      const res = await api.account.socialSignIn(
        provider,
        window.location.origin +
          window.location.pathname +
          window.location.search,
      );
      if (res.url) {
        window.location.href = res.url;
        return;
      }
      throw new Error("Couldn’t start sign-in.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Sign-in failed.");
      setSocial(null);
    }
  }

  function switchMode(next: Mode) {
    setMode(next);
    setError(null);
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <a href="https://mindwire.sh" aria-label="Mindwire home">
          <BrandMark size={26} />
        </a>
        <div className="flex items-center gap-3">
          <ThemeButton />
          <a href={docsUrl} className={styles.docsLink}>
            Docs
            <ArrowUpRight className="size-4" />
          </a>
        </div>
      </header>
      <main className={styles.main}>
        <section className={styles.welcome} aria-label="Your Mindwire account">
          <div>
            <span className={styles.eyebrow}>Your Mindwire account</span>
            <h1>
              YOUR WORK.
              <br />WITH YOU.
            </h1>
            <p>
              Your projects, connected devices, and personal Cloud Computers.
              All in one place.
            </p>
          </div>
          <AccountArtwork className={styles.art} />
          <a href="https://mindwire.sh/app">
            Explore the Mindwire app
            <ArrowUpRight className="size-4" />
          </a>
        </section>
        <section className={styles.formSection}>
          <div className={styles.form}>
            {isRecovery ? (
              <PasswordRecoveryForm
                key={location.pathname + location.search}
                emailDeliveryEnabled={cfg?.passwordResetEnabled}
                onPasswordReset={onPasswordReset}
              />
            ) : (
              <>
                <div className="mb-6 space-y-1.5">
                  <h2 className="text-[28px] font-medium leading-tight tracking-tight">
                    {isSignup
                      ? `Create your ${appName} account`
                      : `Sign in to ${appName}`}
                  </h2>
                  <p className="text-sm text-muted-foreground">
                    {isSignup
                      ? "Manage your devices and Cloud Computers with one account."
                      : "Welcome back — pick up where you left off."}
                  </p>
                </div>

                <div className="space-y-6">
                  {socials.length > 0 && (
                    <>
                      <div className="space-y-2">
                        {socials.includes("apple") && (
                          <SocialButton
                            label="Continue with Apple"
                            icon={<AppleIcon />}
                            busy={social === "apple"}
                            disabled={locked}
                            onClick={() => onSocial("apple")}
                            apple
                          />
                        )}
                        {socials.includes("github") && (
                          <SocialButton
                            label="Continue with GitHub"
                            icon={<GitHubIcon />}
                            busy={social === "github"}
                            disabled={locked}
                            onClick={() => onSocial("github")}
                          />
                        )}
                        {socials.includes("google") && (
                          <SocialButton
                            label="Continue with Google"
                            icon={<GoogleIcon />}
                            busy={social === "google"}
                            disabled={locked}
                            onClick={() => onSocial("google")}
                          />
                        )}
                      </div>
                      <Divider>or continue with email</Divider>
                    </>
                  )}

                  <form onSubmit={onSubmit} className="space-y-4">
                    {isSignup && (
                      <div className="space-y-1.5">
                        <Label htmlFor="name">Name</Label>
                        <Input
                          id="name"
                          className="h-12"
                          autoComplete="name"
                          value={name}
                          onChange={(e) => setName(e.target.value)}
                          placeholder="Ada Lovelace"
                          disabled={locked}
                        />
                      </div>
                    )}
                    <div className="space-y-1.5">
                      <Label htmlFor="email">Email</Label>
                      <Input
                        id="email"
                        className="h-12"
                        type="email"
                        autoComplete="email"
                        required
                        value={email}
                        onChange={(e) => setEmail(e.target.value)}
                        placeholder="you@example.com"
                        disabled={locked}
                      />
                    </div>
                    <div className="space-y-1.5">
                      <div className="flex items-center justify-between gap-3">
                        <Label htmlFor="password">Password</Label>
                        {!isSignup && (
                          <Link
                            to={recoveryPath(
                              "/forgot-password",
                              location.pathname + location.search,
                            )}
                            className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                          >
                            Forgot password?
                          </Link>
                        )}
                      </div>
                      <Input
                        id="password"
                        className="h-12"
                        type="password"
                        autoComplete={
                          isSignup ? "new-password" : "current-password"
                        }
                        required
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        placeholder={
                          isSignup ? "At least 8 characters" : "••••••••"
                        }
                        disabled={locked}
                      />
                    </div>

                    {error && (
                      <p
                        role="alert"
                        className="rounded-xl bg-destructive/8 p-3 text-sm text-destructive"
                      >
                        {error}
                      </p>
                    )}

                    <Button
                      type="submit"
                      className="h-12 w-full"
                      disabled={locked}
                    >
                      {busy && <Loader2 className="size-4 animate-spin" />}
                      {isSignup ? "Create account" : "Sign in"}
                    </Button>
                  </form>
                </div>

                <p className="mt-7 text-center text-sm text-muted-foreground">
                  {isSignup ? "Already have an account?" : "Need an account?"}{" "}
                  <button
                    type="button"
                    disabled={locked}
                    onClick={() => switchMode(isSignup ? "signin" : "signup")}
                    className="font-medium text-foreground underline-offset-4 hover:underline"
                  >
                    {isSignup ? "Sign in" : "Create one"}
                  </button>
                </p>
              </>
            )}
          </div>
        </section>
      </main>

      {/* Account service links. */}
      <footer className={styles.footer}>
        <div className="flex flex-wrap items-center justify-center gap-x-7 gap-y-2 px-5 pb-5 pt-4 text-sm text-muted-foreground lg:pt-5">
          <FooterLink href={docsUrl}>Docs</FooterLink>
          <FooterLink href={githubUrl}>
            <GitHubIcon className="size-3.5" />
            GitHub
          </FooterLink>
          <FooterLink href="https://mindwire.sh/cloud">Personal Cloud</FooterLink>
        </div>
      </footer>
    </div>
  );
}

/** Keep native device approval (including its user code) when returning from account recovery. */
function safeReturnPath(value: string | null): string {
  if (!value?.startsWith("/") || value.startsWith("//") || value.includes("\\"))
    return "/";
  const url = new URL(value, window.location.origin);
  if (
    url.origin !== window.location.origin ||
    ["/forgot-password", "/reset-password"].includes(url.pathname)
  )
    return "/";
  return url.pathname + url.search;
}

function recoveryPath(route: string, returnTo: string): string {
  const destination = safeReturnPath(returnTo);
  return destination === "/"
    ? route
    : `${route}?${new URLSearchParams({ returnTo: destination })}`;
}

function PasswordRecoveryForm({
  emailDeliveryEnabled,
  onPasswordReset,
}: {
  emailDeliveryEnabled?: boolean;
  onPasswordReset?: () => void;
}) {
  const location = useLocation();
  const navigate = useNavigate();
  const query = new URLSearchParams(location.search);
  const returnTo = safeReturnPath(query.get("returnTo"));
  const resetting = location.pathname === "/reset-password";
  const token = query.get("token");
  const alreadyReset =
    resetting && location.state?.passwordResetComplete === true;
  const [invalidLink, setInvalidLink] = useState(
    resetting && !alreadyReset && (!token || Boolean(query.get("error"))),
  );
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [complete, setComplete] = useState(alreadyReset);
  const [error, setError] = useState<string | null>(null);
  const unavailable = !resetting && emailDeliveryEnabled === false;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy || invalidLink || unavailable) return;
    setError(null);
    if (resetting && (password.length < 8 || password.length > 128)) {
      setError("Use between 8 and 128 characters for your new password.");
      return;
    }
    if (resetting && password !== confirmation) {
      setError("Your passwords don’t match.");
      return;
    }
    setBusy(true);
    try {
      if (resetting && token) {
        await api.account.resetPassword(token, password);
        setPassword("");
        setConfirmation("");
        // Only the reset action revokes Mindwire account sessions. No sign-out request is needed.
        onPasswordReset?.();
        // Replace the consumed token in browser history with the success page.
        navigate(recoveryPath("/reset-password", returnTo), {
          replace: true,
          state: { passwordResetComplete: true },
        });
        return;
      } else {
        const redirect = new URL(
          recoveryPath("/reset-password", returnTo),
          window.location.origin,
        );
        await api.account.requestPasswordReset(email.trim(), redirect.href);
      }
      setComplete(true);
    } catch (error) {
      if (error instanceof HttpError && error.code === "INVALID_TOKEN") {
        setInvalidLink(true);
      } else {
        setError(
          error instanceof Error
            ? error.message
            : "This request could not finish. Please retry.",
        );
      }
    } finally {
      setBusy(false);
    }
  }

  const title = complete
    ? resetting
      ? "Password updated"
      : "Check your email"
    : invalidLink
      ? "This link has expired"
      : resetting
        ? "Choose a new password"
        : "Forgot your password?";
  const description = complete
    ? resetting
      ? "Sign in with your new password to continue."
      : "If an account exists for that email, you’ll receive a reset link. Check your spam folder too."
    : invalidLink
      ? "Request a new link to reset your password."
      : resetting
        ? "Use at least 8 characters. You’ll sign in again on your connected devices."
        : "Enter your account email and we’ll send you a reset link.";

  return (
    <div className="space-y-6">
      {complete && (
        <div
          className="flex size-12 items-center justify-center rounded-2xl bg-muted"
          aria-hidden
        >
          {resetting ? (
            <Check className="size-6" />
          ) : (
            <Mail className="size-6" />
          )}
        </div>
      )}
      <div className="space-y-2" role={complete ? "status" : undefined}>
        <h2 className="text-[28px] font-medium leading-tight tracking-tight">
          {title}
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">
          {description}
        </p>
      </div>
      {invalidLink && !complete ? (
        <Button
          className="h-12 w-full"
          onClick={() =>
            navigate(recoveryPath("/forgot-password", returnTo), {
              replace: true,
            })
          }
        >
          Request a new link
        </Button>
      ) : complete ? (
        <div className="space-y-4">
          <Button
            className="h-12 w-full"
            onClick={() => navigate(returnTo, { replace: true })}
          >
            Back to sign in
          </Button>
          {!resetting && (
            <p className="text-center">
              <button
                type="button"
                className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                onClick={() => {
                  setComplete(false);
                  setError(null);
                }}
              >
                Use another email
              </button>
            </p>
          )}
        </div>
      ) : (
        <form onSubmit={submit} className="space-y-4">
          {resetting ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="new-password">New password</Label>
                <Input
                  id="new-password"
                  className="h-12"
                  type="password"
                  autoComplete="new-password"
                  autoFocus
                  minLength={8}
                  maxLength={128}
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="At least 8 characters"
                  disabled={busy}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="confirm-password">Confirm password</Label>
                <Input
                  id="confirm-password"
                  className="h-12"
                  type="password"
                  autoComplete="new-password"
                  minLength={8}
                  maxLength={128}
                  required
                  value={confirmation}
                  onChange={(e) => setConfirmation(e.target.value)}
                  placeholder="Repeat your new password"
                  disabled={busy}
                />
              </div>
            </>
          ) : (
            <div className="space-y-1.5">
              <Label htmlFor="recovery-email">Email</Label>
              <Input
                id="recovery-email"
                className="h-12"
                type="email"
                autoComplete="email"
                autoFocus
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@example.com"
                disabled={busy || unavailable}
              />
            </div>
          )}
          {(error || unavailable) && (
            <p
              role="alert"
              className="rounded-xl bg-destructive/8 p-3 text-sm text-destructive"
            >
              {error ||
                "Password reset email is not available yet. Please contact Mindwire support."}
            </p>
          )}
          <Button
            type="submit"
            className="h-12 w-full"
            disabled={busy || unavailable}
          >
            {busy && <Loader2 className="size-4 animate-spin" />}
            {resetting ? "Update password" : "Send reset link"}
          </Button>
        </form>
      )}
      {!complete && (
        <Link
          to={returnTo}
          className="flex items-center justify-center gap-2 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
          Back to sign in
        </Link>
      )}
    </div>
  );
}

function SocialButton({
  label,
  icon,
  busy,
  disabled,
  onClick,
  apple = false,
}: {
  label: string;
  icon: ReactNode;
  busy: boolean;
  disabled: boolean;
  onClick: () => void;
  apple?: boolean;
}) {
  return (
    <Button
      type="button"
      variant="outline"
      className={`h-12 w-full justify-center gap-2.5 ${apple ? "bg-foreground text-background hover:bg-foreground/90 hover:text-background" : ""}`}
      disabled={disabled}
      onClick={onClick}
    >
      {busy ? <Loader2 className="size-4 animate-spin" /> : icon}
      {label}
    </Button>
  );
}

function Divider({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-3">
      <span className="h-px flex-1 bg-border" />
      <span className="text-xs text-muted-foreground">{children}</span>
      <span className="h-px flex-1 bg-border" />
    </div>
  );
}

function FooterLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-1.5 transition-colors hover:text-foreground"
    >
      {children}
    </a>
  );
}

// Brand marks. GitHub's is genuinely monochrome, so it rides `currentColor` (flips with the theme like
// the rest of the ink). Google's is its official four-color "G" — a deliberate brand-color exception
// (like the agent brand marks), never recolored, so it stays instantly recognizable.
function AppleIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" fill="currentColor" aria-hidden>
      <path d="M17.05 12.04c-.03-2.66 2.17-3.94 2.27-4.01-1.24-1.81-3.17-2.06-3.85-2.09-1.64-.17-3.2.96-4.03.96-.83 0-2.11-.94-3.47-.91-1.79.03-3.44 1.04-4.36 2.64-1.86 3.23-.48 8.01 1.33 10.63.88 1.28 1.93 2.72 3.31 2.67 1.33-.05 1.83-.86 3.44-.86 1.61 0 2.06.86 3.47.83 1.43-.03 2.34-1.31 3.22-2.6 1.01-1.49 1.43-2.93 1.45-3.01-.03-.01-2.79-1.07-2.82-4.24zM14.47 4.4c.73-.89 1.22-2.12 1.09-3.35-1.05.04-2.32.7-3.07 1.58-.67.78-1.26 2.03-1.1 3.23 1.17.09 2.36-.6 3.08-1.46z" />
    </svg>
  );
}

function GitHubIcon({ className = "size-4" }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={className}
      fill="currentColor"
      aria-hidden
    >
      <path d="M12 .5C5.73.5.5 5.73.5 12a11.5 11.5 0 0 0 7.86 10.92c.58.1.79-.25.79-.56v-2c-3.2.7-3.88-1.37-3.88-1.37-.53-1.34-1.29-1.7-1.29-1.7-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.56-.29-5.25-1.28-5.25-5.7 0-1.26.45-2.29 1.19-3.1-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.18 1.18a11 11 0 0 1 5.8 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.84 1.19 3.1 0 4.43-2.69 5.41-5.26 5.69.41.36.78 1.08.78 2.18v3.23c0 .31.21.67.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.73 18.27.5 12 .5Z" />
    </svg>
  );
}

function GoogleIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        fill="#4285F4"
        d="M23.52 12.27c0-.79-.07-1.54-.2-2.27H12v4.51h6.47a5.53 5.53 0 0 1-2.4 3.63v3h3.88c2.27-2.09 3.57-5.17 3.57-8.87Z"
      />
      <path
        fill="#34A853"
        d="M12 24c3.24 0 5.96-1.08 7.95-2.91l-3.88-3c-1.08.72-2.45 1.16-4.07 1.16-3.13 0-5.78-2.11-6.73-4.96H1.29v3.09A12 12 0 0 0 12 24Z"
      />
      <path
        fill="#FBBC05"
        d="M5.27 14.29a7.2 7.2 0 0 1 0-4.58V6.62H1.29a12 12 0 0 0 0 10.76l3.98-3.09Z"
      />
      <path
        fill="#EA4335"
        d="M12 4.75c1.77 0 3.35.61 4.6 1.8l3.44-3.44A11.5 11.5 0 0 0 12 0 12 12 0 0 0 1.29 6.62l3.98 3.09C6.22 6.86 8.87 4.75 12 4.75Z"
      />
    </svg>
  );
}
