// Register a new daemon in the fleet. One dialog, five provider shapes — pick where the daemon runs:
// this host (`local`, the current machine), an already-running `remote` daemon, a box reached over
// `ssh`, a Docker container, or an Oblien sandbox. The two axes the user cares about — "control the
// current host" vs "control a remote server" — are `local` vs `remote`/`ssh`. Fill the provider's
// fields and it is verified/provisioned before being committed to the fleet. Only providers the server
// can actually offer are selectable.
import { useEffect, useState } from "react";
import {
  Plus,
  Loader2,
  Server,
  Container,
  Cloud,
  ArrowUpRight,
  MonitorCog,
  Terminal,
} from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useProvisionStream } from "@/lib/useProvisionStream";
import type {
  AddDaemonRequest,
  DaemonProvider,
  EnsureEvent,
  FleetView,
  OblienImage,
  ProviderAvailability,
} from "@shared/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

const DASHBOARD_URL = "https://oblien.com/dashboard";

/** Provider order in the picker: from "this machine" outward to a cloud sandbox. */
const PROVIDER_ORDER: DaemonProvider[] = [
  "local",
  "remote",
  "ssh",
  "docker",
  "oblien",
];

const PROVIDER_META: Record<
  DaemonProvider,
  { label: string; hint: string; icon: typeof Server }
> = {
  local: {
    label: "This host",
    hint: "Run mindwired on the current machine",
    icon: MonitorCog,
  },
  remote: {
    label: "Remote runtime",
    hint: "Connect to a running mindwired",
    icon: Server,
  },
  ssh: {
    label: "SSH server",
    hint: "Deploy mindwired over SSH",
    icon: Terminal,
  },
  docker: {
    label: "Docker container",
    hint: "Run mindwired in a container",
    icon: Container,
  },
  oblien: {
    label: "Oblien sandbox",
    hint: "Provision a fresh microVM",
    icon: Cloud,
  },
};

export function AddDaemonDialog({
  providers,
  onAdded,
}: {
  providers: ProviderAvailability | null;
  onAdded: (fleet: FleetView) => void;
}) {
  // Oblien keys live in the session, linked on demand right here — not at a login gate.
  const { status, setStatus } = useApp();
  const oblienLinked = Boolean(status.oblien);

  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const provision = useProvisionStream(() => {
    void api.fleet().then(onAdded).catch(() => {});
  });

  const [provider, setProvider] = useState<DaemonProvider>("oblien");
  const [label, setLabel] = useState("");
  const [agent, setAgent] = useState("");
  const [activate, setActivate] = useState(true);

  // remote
  const [daemonUrl, setDaemonUrl] = useState("");
  const [token, setToken] = useState("");

  // local (this host)
  const [localCwd, setLocalCwd] = useState("");

  // ssh
  const [sshHost, setSshHost] = useState("");
  const [sshPort, setSshPort] = useState("");
  const [sshUser, setSshUser] = useState("");
  const [sshAuthMode, setSshAuthMode] = useState<"key" | "password" | "agent">(
    "key",
  );
  const [sshPrivateKey, setSshPrivateKey] = useState("");
  const [sshPassphrase, setSshPassphrase] = useState("");
  const [sshPassword, setSshPassword] = useState("");
  const [sshAgentCwd, setSshAgentCwd] = useState("");
  const [sshDockerImage, setSshDockerImage] = useState("");

  // docker
  const [dockerMode, setDockerMode] = useState<"image" | "container">("image");
  const [image, setImage] = useState("");
  const [container, setContainer] = useState("");
  const [engineHost, setEngineHost] = useState("");

  // oblien
  const [oblienImage, setOblienImage] = useState("");
  const [oblienImages, setOblienImages] = useState<OblienImage[]>([]);
  const [loadingOblienImages, setLoadingOblienImages] = useState(false);
  const [oblienMode, setOblienMode] = useState<"new" | "existing">("new");
  const [cpus, setCpus] = useState("");
  const [memoryMb, setMemoryMb] = useState("");
  const [diskMb, setDiskMb] = useState("4096");
  const [workspaceId, setWorkspaceId] = useState("");
  // oblien account link (only when not already linked in the session)
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");

  // docker + oblien
  const [lifecycle, setLifecycle] = useState<"temporary" | "permanent">(
    "temporary",
  );

  // SaaS is designed around an isolated Oblien runtime. A minimal self-host install may not have the
  // optional peer, in which case fall back to the always-available remote connector.
  useEffect(() => {
    if (providers && !providers.oblien && provider === "oblien")
      setProvider("remote");
  }, [providers, provider]);

  // Sandboxes are typically the user's durable cloud runtime; SSH/Docker defaults remain temporary.
  useEffect(() => {
    setLifecycle(provider === "oblien" ? "permanent" : "temporary");
  }, [provider]);

  useEffect(() => {
    if (!open || provider !== "oblien" || !oblienLinked) return;
    setLoadingOblienImages(true);
    void api.oblienImages().then((images) => {
      setOblienImages(images);
      setOblienImage((selected) => (images.some((item) => item.image === selected) ? selected : (images[0]?.image ?? "")));
    }).catch((err) => toast.error(err instanceof Error ? err.message : "Could not load Oblien images.")).finally(() => setLoadingOblienImages(false));
  }, [open, provider, oblienLinked]);

  function reset() {
    setLabel("");
    setAgent("");
    setActivate(true);
    setDaemonUrl("");
    setToken("");
    setLocalCwd("");
    setSshHost("");
    setSshPort("");
    setSshUser("");
    setSshAuthMode("key");
    setSshPrivateKey("");
    setSshPassphrase("");
    setSshPassword("");
    setSshAgentCwd("");
    setSshDockerImage("");
    setDockerMode("image");
    setImage("");
    setContainer("");
    setEngineHost("");
    setOblienImage("");
    setOblienMode("new");
    setCpus("");
    setMemoryMb("");
    setDiskMb("4096");
    setWorkspaceId("");
    setClientId("");
    setClientSecret("");
    setLifecycle(provider === "oblien" ? "permanent" : "temporary");
  }

  async function unlinkOblien() {
    try {
      setStatus(await api.disconnectOblien());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not unlink");
    }
  }

  async function linkOblien() {
    if (!clientId.trim() || !clientSecret.trim()) {
      toast.error("Enter your Oblien Client ID and Secret.");
      return;
    }
    setSaving(true);
    try {
      setStatus(await api.connectOblien({ clientId: clientId.trim(), clientSecret: clientSecret.trim() }));
      toast.success("Oblien account connected");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not connect Oblien.");
    } finally {
      setSaving(false);
    }
  }

  function build(): AddDaemonRequest | null {
    const base = {
      ...(label.trim() ? { label: label.trim() } : {}),
      ...(agent.trim() ? { agent: agent.trim() } : {}),
      activate,
    };
    if (provider === "remote") {
      const url = daemonUrl.trim();
      if (!url) {
        toast.error("A runtime URL is required.");
        return null;
      }
      if (providers?.remoteTokenRequired && !token.trim()) {
        toast.error("A bearer token is required for a cloud remote runtime.");
        return null;
      }
      return {
        provider,
        daemonUrl: url,
        ...(token.trim() ? { token: token.trim() } : {}),
        ...base,
      };
    }
    if (provider === "local") {
      return {
        provider,
        ...(localCwd.trim() ? { cwd: localCwd.trim() } : {}),
        ...base,
      };
    }
    if (provider === "ssh") {
      const host = sshHost.trim();
      const username = sshUser.trim();
      if (!host) {
        toast.error("An SSH host is required.");
        return null;
      }
      if (!username) {
        toast.error("An SSH username is required.");
        return null;
      }
      if (sshAuthMode === "key" && !sshPrivateKey.trim()) {
        toast.error("Paste a private key, or switch to password / ssh-agent.");
        return null;
      }
      if (sshAuthMode === "password" && !sshPassword) {
        toast.error("Enter the SSH password, or switch auth method.");
        return null;
      }
      return {
        provider,
        host,
        ...(sshPort.trim() ? { port: Number(sshPort) } : {}),
        username,
        ...(sshAuthMode === "key" && sshPrivateKey.trim()
          ? { privateKey: sshPrivateKey }
          : {}),
        ...(sshAuthMode === "key" && sshPassphrase
          ? { passphrase: sshPassphrase }
          : {}),
        ...(sshAuthMode === "password" && sshPassword
          ? { password: sshPassword }
          : {}),
        ...(sshAgentCwd.trim() ? { agentCwd: sshAgentCwd.trim() } : {}),
        ...(sshDockerImage.trim()
          ? { dockerImage: sshDockerImage.trim() }
          : {}),
        lifecycle,
        ...base,
      };
    }
    if (provider === "docker") {
      const useContainer = dockerMode === "container";
      if (useContainer && !container.trim()) {
        toast.error("A container name or id is required.");
        return null;
      }
      return {
        provider,
        ...(useContainer
          ? { container: container.trim() }
          : image.trim()
            ? { image: image.trim() }
            : {}),
        ...(engineHost.trim() ? { engineHost: engineHost.trim() } : {}),
        lifecycle,
        ...base,
      };
    }
    if (oblienMode === "existing" && !workspaceId.trim()) {
      toast.error("Enter the Oblien workspace ID to connect.");
      return null;
    }
    if (oblienMode === "new" && !oblienImage) {
      toast.error("Choose an image from your Oblien catalog.");
      return null;
    }
    return {
      provider,
      ...(oblienMode === "new" && oblienImage ? { image: oblienImage } : {}),
      ...(oblienMode === "new" && cpus ? { cpus: Number(cpus) } : {}),
      ...(oblienMode === "new" && memoryMb ? { memoryMb: Number(memoryMb) } : {}),
      ...(oblienMode === "new" && diskMb ? { diskMb: Number(diskMb) } : {}),
      ...(oblienMode === "existing" ? { workspaceId: workspaceId.trim() } : {}),
      lifecycle,
      ...base,
    };
  }

  async function submit() {
    if (provider === "oblien" && !oblienLinked) {
      toast.error("Connect your Oblien account before choosing a runtime.");
      return;
    }
    const req = build();
    if (!req) return;

    setSaving(true);
    try {
      const ready = await provision.add(req);
      if (!ready) return;
      toast.success("Runtime connected and ready");
      reset();
      setOpen(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not add runtime");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) reset();
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus className="size-4" />
          Add runtime
        </Button>
      </DialogTrigger>
      <DialogContent
        onOpenAutoFocus={(event) => event.preventDefault()}
        className="grid h-[min(88dvh,760px)] w-[min(96vw,1120px)] max-w-[1120px] grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0"
      >
        <DialogHeader className="px-6 pb-5 pt-6 pr-14 sm:px-7 sm:pt-7">
          <DialogTitle>Add a runtime</DialogTitle>
          <DialogDescription>
            Connect or provision it now. Provisioned runtimes are saved immediately and become ready only after their health check succeeds.
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 overflow-y-auto px-6 pb-6 lg:overflow-hidden sm:px-7 sm:pb-7">
          <div className="grid min-h-full gap-5 lg:h-full lg:grid-cols-[280px_minmax(0,1fr)] lg:gap-x-8">
            <aside className="flex min-h-[500px] flex-col gap-4 lg:h-full lg:border-r lg:border-border lg:pr-8">
              {/* provider picker */}
              <div className="grid grid-cols-1 gap-2">
                {PROVIDER_ORDER.map((k) => {
                  const meta = PROVIDER_META[k];
                  const Icon = meta.icon;
                  const enabled = providers ? providers[k] : k === "remote";
                  const selected = provider === k;
                  const selfHostOnly =
                    !enabled &&
                    (k === "local" || k === "ssh" || k === "docker");
                  const button = (
                    <button
                      type="button"
                      disabled={!enabled}
                      onClick={() => setProvider(k)}
                      className={cn(
                        "flex w-full items-center gap-3 border p-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-40",
                        selected
                          ? "border-ink/30 bg-accent"
                          : "border-border hover:border-ink/25",
                      )}
                    >
                      <Icon className="size-4" />
                      <span className="text-xs font-medium">{meta.label}</span>
                    </button>
                  );
                  if (enabled) return <div key={k}>{button}</div>;
                  return (
                    <Tooltip key={k}>
                      <TooltipTrigger asChild>
                        <span>{button}</span>
                      </TooltipTrigger>
                      <TooltipContent>
                        {selfHostOnly
                          ? "Self-host MindWire on your server to use this runtime."
                          : "Not available on this deployment."}
                      </TooltipContent>
                    </Tooltip>
                  );
                })}
              </div>

              {providers &&
                (!providers.local || !providers.ssh || !providers.docker) && (
                  <p className="text-xs leading-relaxed text-muted-foreground">
                    SSH, Docker, and host runtimes are disabled in SaaS to
                    protect the shared control plane.{" "}
                    <a
                      className="underline underline-offset-4 hover:text-foreground"
                      href="https://github.com/oblien/mindwire/tree/main/packages/docker"
                      target="_blank"
                      rel="noreferrer"
                    >
                      Self-host MindWire on your server{" "}
                      <ArrowUpRight className="inline size-3" />
                    </a>
                  </p>
                )}

              <p className="text-xs leading-relaxed text-muted-foreground">
                Choose a runtime here. Connection and provisioning fields stay
                in the workspace on the right.
              </p>

              <div className="mt-auto space-y-4 border-t border-border pt-4">
                <label className="flex items-center gap-3 text-sm">
                  <Switch checked={activate} onCheckedChange={setActivate} />
                  Make this the active runtime
                </label>
                <DialogFooter className="flex-col gap-2 sm:flex-col sm:items-stretch sm:justify-start">
                  <Button
                    variant="ghost"
                    onClick={() => setOpen(false)}
                    disabled={saving}
                  >
                    Cancel
                  </Button>
                  <Button onClick={() => void submit()} disabled={saving || (provider === "oblien" && !oblienLinked)}>
                    {saving ? (
                      <Loader2 className="size-4 animate-spin" />
                    ) : (
                      <Plus className="size-4" />
                    )}
                    {provider === "remote"
                      ? "Verify & connect"
                      : provider === "oblien"
                        ? !oblienLinked
                          ? "Connect Oblien first"
                          : oblienMode === "existing"
                          ? "Check & ensure runtime"
                          : "Create & ensure runtime"
                        : "Ensure runtime"}
                  </Button>
                </DialogFooter>
              </div>
            </aside>

            <section className="min-w-0 space-y-5 lg:min-h-0 lg:overflow-y-auto lg:pr-2">
              {provider === "docker" && !providers?.docker && (
                <p className="text-xs text-muted-foreground">
                  Docker requires the optional{" "}
                  <span className="font-mono">dockerode</span> peer on the
                  server.
                </p>
              )}

              {/* provider fields */}
              {provider === "remote" && (
                <div className="space-y-4">
                  <Field label="Runtime URL" htmlFor="d-url">
                    <Input
                      id="d-url"
                      placeholder="https://runtime.example.com"
                      value={daemonUrl}
                      onChange={(e) => setDaemonUrl(e.target.value)}
                      autoComplete="off"
                      spellCheck={false}
                    />
                  </Field>
                  <Field
                    label={`Bearer token${providers?.remoteTokenRequired ? "" : " (optional)"}`}
                    htmlFor="d-token"
                    hint={
                      providers?.remoteTokenRequired
                        ? "Required for SaaS remote runtimes; held server-side and never returned to the browser."
                        : "Held server-side; never returned to the browser."
                    }
                  >
                    <Input
                      id="d-token"
                      type="password"
                      placeholder="Leave blank if unauthenticated"
                      value={token}
                      onChange={(e) => setToken(e.target.value)}
                      autoComplete="off"
                    />
                  </Field>
                </div>
              )}

              {provider === "local" && (
                <div className="space-y-4">
                  <p className="text-xs text-muted-foreground">
                    Runs an embedded{" "}
                    <span className="font-mono">mindwired</span> on{" "}
                    <span className="font-medium text-foreground">
                      this machine
                    </span>{" "}
                    — the host the console is deployed on. It spins up on first
                    use; there's nothing to provision.
                  </p>
                  <Field
                    label="Working directory (optional)"
                    htmlFor="lc-cwd"
                    hint="Where agents run on the host. Omit for the server's own working directory."
                  >
                    <Input
                      id="lc-cwd"
                      placeholder="/srv/workspace"
                      value={localCwd}
                      onChange={(e) => setLocalCwd(e.target.value)}
                      autoComplete="off"
                      spellCheck={false}
                    />
                  </Field>
                </div>
              )}

              {provider === "ssh" && (
                <div className="space-y-4">
                  <div className="grid gap-4 sm:grid-cols-[1fr_auto]">
                    <Field label="Host" htmlFor="ss-host">
                      <Input
                        id="ss-host"
                        placeholder="box.example.com"
                        value={sshHost}
                        onChange={(e) => setSshHost(e.target.value)}
                        autoComplete="off"
                        spellCheck={false}
                      />
                    </Field>
                    <Field label="Port" htmlFor="ss-port">
                      <Input
                        id="ss-port"
                        className="w-24"
                        inputMode="numeric"
                        placeholder="22"
                        value={sshPort}
                        onChange={(e) =>
                          setSshPort(e.target.value.replace(/[^0-9]/g, ""))
                        }
                      />
                    </Field>
                  </div>
                  <Field label="Username" htmlFor="ss-user">
                    <Input
                      id="ss-user"
                      placeholder="root"
                      value={sshUser}
                      onChange={(e) => setSshUser(e.target.value)}
                      autoComplete="off"
                      spellCheck={false}
                    />
                  </Field>

                  {/* Auth: exactly one of key / password / ssh-agent. Secrets are held server-side only. */}
                  <div className="space-y-2">
                    <Label>Authentication</Label>
                    <div className="grid grid-cols-3 gap-2">
                      <SegButton
                        active={sshAuthMode === "key"}
                        onClick={() => setSshAuthMode("key")}
                      >
                        Private key
                      </SegButton>
                      <SegButton
                        active={sshAuthMode === "password"}
                        onClick={() => setSshAuthMode("password")}
                      >
                        Password
                      </SegButton>
                      <SegButton
                        active={sshAuthMode === "agent"}
                        onClick={() => setSshAuthMode("agent")}
                      >
                        ssh-agent
                      </SegButton>
                    </div>
                  </div>

                  {sshAuthMode === "key" && (
                    <>
                      <Field
                        label="Private key (PEM)"
                        htmlFor="ss-key"
                        hint="Held server-side for this session; never returned to the browser."
                      >
                        <Textarea
                          id="ss-key"
                          rows={4}
                          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                          value={sshPrivateKey}
                          onChange={(e) => setSshPrivateKey(e.target.value)}
                          className="font-mono text-xs"
                          spellCheck={false}
                        />
                      </Field>
                      <Field label="Passphrase (optional)" htmlFor="ss-pass">
                        <Input
                          id="ss-pass"
                          type="password"
                          placeholder="For an encrypted key"
                          value={sshPassphrase}
                          onChange={(e) => setSshPassphrase(e.target.value)}
                          autoComplete="off"
                        />
                      </Field>
                    </>
                  )}
                  {sshAuthMode === "password" && (
                    <Field
                      label="Password"
                      htmlFor="ss-pw"
                      hint="Held server-side for this session; never returned to the browser."
                    >
                      <Input
                        id="ss-pw"
                        type="password"
                        placeholder="••••••••"
                        value={sshPassword}
                        onChange={(e) => setSshPassword(e.target.value)}
                        autoComplete="off"
                      />
                    </Field>
                  )}
                  {sshAuthMode === "agent" && (
                    <p className="text-xs text-muted-foreground">
                      Uses the server's{" "}
                      <span className="font-mono">ssh-agent</span> (
                      <span className="font-mono">$SSH_AUTH_SOCK</span>). No key
                      material is stored.
                    </p>
                  )}

                  <Field
                    label="Remote working directory (optional)"
                    htmlFor="ss-cwd"
                    hint="Where agents run on the remote. Omit for the remote default."
                  >
                    <Input
                      id="ss-cwd"
                      placeholder="/root"
                      value={sshAgentCwd}
                      onChange={(e) => setSshAgentCwd(e.target.value)}
                      autoComplete="off"
                      spellCheck={false}
                    />
                  </Field>
                  <Field
                    label="Run in a container (optional)"
                    htmlFor="ss-docker"
                    hint="Image to run the remote daemon inside a Docker container on the host."
                  >
                    <Input
                      id="ss-docker"
                      placeholder="e.g. ubuntu-24.04"
                      value={sshDockerImage}
                      onChange={(e) => setSshDockerImage(e.target.value)}
                      autoComplete="off"
                      spellCheck={false}
                    />
                  </Field>
                  <LifecycleSelect value={lifecycle} onChange={setLifecycle} />
                </div>
              )}

              {provider === "docker" && (
                <div className="space-y-4">
                  <div className="grid grid-cols-2 gap-2">
                    <SegButton
                      active={dockerMode === "image"}
                      onClick={() => setDockerMode("image")}
                    >
                      From image
                    </SegButton>
                    <SegButton
                      active={dockerMode === "container"}
                      onClick={() => setDockerMode("container")}
                    >
                      Attach container
                    </SegButton>
                  </div>
                  {dockerMode === "image" ? (
                    <Field
                      label="Image (optional)"
                      htmlFor="dk-image"
                      hint="Leave blank to pull the MindWire Runtime matching this Console's SDK version."
                    >
                      <Input
                        id="dk-image"
                        placeholder="MindWire Runtime (recommended)"
                        value={image}
                        onChange={(e) => setImage(e.target.value)}
                        autoComplete="off"
                        spellCheck={false}
                      />
                    </Field>
                  ) : (
                    <Field
                      label="Container name or id"
                      htmlFor="dk-container"
                      hint="Must already publish the runtime port."
                    >
                      <Input
                        id="dk-container"
                        placeholder="mindwired"
                        value={container}
                        onChange={(e) => setContainer(e.target.value)}
                        autoComplete="off"
                        spellCheck={false}
                      />
                    </Field>
                  )}
                  <Field
                    label="Engine host (optional)"
                    htmlFor="dk-engine"
                    hint="Remote Docker over TCP. Omit for the local socket."
                  >
                    <Input
                      id="dk-engine"
                      placeholder="tcp://10.0.0.5:2375"
                      value={engineHost}
                      onChange={(e) => setEngineHost(e.target.value)}
                      autoComplete="off"
                      spellCheck={false}
                    />
                  </Field>
                  <LifecycleSelect value={lifecycle} onChange={setLifecycle} />
                </div>
              )}

              {provider === "oblien" && (
                <div className="space-y-4">
                  {/* account link — Oblien creds live server-side; linked once, reused across daemons */}
                  {oblienLinked ? (
                    <div className="flex items-center justify-between border border-border bg-accent/40 px-3 py-2 text-xs">
                      <span className="flex items-center gap-2">
                        <Cloud className="size-3.5" />
                        Linked to Oblien
                        <span className="font-mono text-muted-foreground">
                          {status.oblien?.label}
                        </span>
                      </span>
                      <button
                        type="button"
                        onClick={() => void unlinkOblien()}
                        className="text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
                      >
                        Use a different account
                      </button>
                    </div>
                  ) : (
                    <div className="space-y-3 border border-border p-3">
                      <p className="text-xs text-muted-foreground">
                        Provisioning an Oblien sandbox needs your API keys.
                        They’re verified and held server-side for this session —
                        they never touch the browser.
                      </p>
                      <Field label="Client ID" htmlFor="ob-cid">
                        <Input
                          id="ob-cid"
                          placeholder="oba_…"
                          value={clientId}
                          onChange={(e) => setClientId(e.target.value)}
                          autoComplete="off"
                          spellCheck={false}
                        />
                      </Field>
                      <Field label="Client Secret" htmlFor="ob-secret">
                        <Input
                          id="ob-secret"
                          type="password"
                          placeholder="••••••••••••"
                          value={clientSecret}
                          onChange={(e) => setClientSecret(e.target.value)}
                          autoComplete="off"
                        />
                      </Field>
                      <a
                        href={DASHBOARD_URL}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
                      >
                        Get your keys at oblien.com/dashboard
                        <ArrowUpRight className="size-3.5" />
                      </a>
                      <Button type="button" variant="outline" className="w-full" disabled={saving} onClick={() => void linkOblien()}>
                        {saving && <Loader2 className="size-4 animate-spin" />}
                        Connect Oblien
                      </Button>
                    </div>
                  )}
                  {oblienLinked && (
                    <>
                  <div className="grid grid-cols-2 gap-2">
                    <SegButton active={oblienMode === "new"} onClick={() => setOblienMode("new")}>
                      Deploy new
                    </SegButton>
                    <SegButton active={oblienMode === "existing"} onClick={() => setOblienMode("existing")}>
                      Connect existing
                    </SegButton>
                  </div>
                  {oblienMode === "existing" ? (
                    <Field
                      label="Oblien workspace ID"
                      htmlFor="ob-ws"
                      hint="MindWire verifies access, checks the runtime, and only provisions it if the workspace is not already healthy."
                    >
                      <Input
                        id="ob-ws"
                        placeholder="ws_…"
                        value={workspaceId}
                        onChange={(e) => setWorkspaceId(e.target.value)}
                        autoComplete="off"
                        spellCheck={false}
                      />
                    </Field>
                  ) : (
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="Image" htmlFor="ob-image">
                        <Select value={oblienImage} onValueChange={setOblienImage} disabled={loadingOblienImages || oblienImages.length === 0}>
                          <SelectTrigger id="ob-image">
                            <SelectValue placeholder={loadingOblienImages ? "Loading your Oblien images…" : "No images available"} />
                        </SelectTrigger>
                        <SelectContent>
                          {oblienImages.map((item) => (
                            <SelectItem key={item.image} value={item.image}>
                              {item.name}
                            </SelectItem>
                          ))}
                          </SelectContent>
                        </Select>
                        {!loadingOblienImages && oblienImages.length === 0 && (
                          <p className="text-xs text-muted-foreground">No images are available to this Oblien account.</p>
                        )}
                    </Field>
                    <div className="sm:col-span-1">
                      <LifecycleSelect
                        value={lifecycle}
                        onChange={setLifecycle}
                      />
                    </div>
                    <Field label="vCPUs" htmlFor="ob-cpus">
                      <Input
                        id="ob-cpus"
                        inputMode="numeric"
                        placeholder="default"
                        value={cpus}
                        onChange={(e) =>
                          setCpus(e.target.value.replace(/[^0-9]/g, ""))
                        }
                      />
                    </Field>
                    <Field label="Memory (MB)" htmlFor="ob-mem">
                      <Input
                        id="ob-mem"
                        inputMode="numeric"
                        placeholder="default"
                        value={memoryMb}
                        onChange={(e) =>
                          setMemoryMb(e.target.value.replace(/[^0-9]/g, ""))
                        }
                      />
                    </Field>
                    <Field label="Disk (MB)" htmlFor="ob-disk" hint="4 GB default. Oblien otherwise defaults to 128 MB.">
                      <Input
                        id="ob-disk"
                        inputMode="numeric"
                        value={diskMb}
                        onChange={(e) => setDiskMb(e.target.value.replace(/[^0-9]/g, ""))}
                      />
                    </Field>
                  </div>
                  )}
                    </>
                  )}
                </div>
              )}

              {/* shared */}
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Label (optional)" htmlFor="d-label">
                  <Input
                    id="d-label"
                    placeholder={PROVIDER_META[provider].label}
                    value={label}
                    onChange={(e) => setLabel(e.target.value)}
                    autoComplete="off"
                  />
                </Field>
                <Field
                  label="Default agent (optional)"
                  htmlFor="d-agent"
                  hint="Adapter id, e.g. claude-code."
                >
                  <Input
                    id="d-agent"
                    placeholder="runtime default"
                    value={agent}
                    onChange={(e) => setAgent(e.target.value)}
                    autoComplete="off"
                    spellCheck={false}
                  />
                </Field>
              </div>
              {provision.status === "provisioning" && (
                <ProvisionProgress
                  provider={provider}
                  phase={provision.logs.at(-1)?.phase}
                  message={provision.logs.at(-1)?.message}
                />
              )}
              {provision.status === "error" && provision.error && (
                <div className="sticky bottom-0 z-10 -mx-1 border-t border-border bg-background/95 px-1 pt-3 backdrop-blur">
                  <p
                    className="border border-destructive/50 bg-destructive/5 p-3 text-xs leading-relaxed text-destructive"
                    role="alert"
                  >
                    {provision.error}
                  </p>
                </div>
              )}
            </section>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ProvisionProgress({
  provider,
  phase,
  message,
}: {
  provider: DaemonProvider;
  phase?: EnsureEvent["phase"];
  message?: string;
}) {
  const percent = progressPercent(phase);
  return (
    <div
      className="sticky bottom-0 z-10 -mx-1 border-t border-border bg-background/95 px-1 pt-3 backdrop-blur"
      aria-live="polite"
    >
      <div className="border border-border bg-accent/40 p-3 text-xs">
        <p className="flex items-center gap-2 font-medium">
          <Loader2 className="size-3.5 animate-spin" />
          {provider === "remote" ? "Verifying runtime" : "Preparing runtime"}
          <span className="ml-auto tabular-nums text-muted-foreground">{percent}%</span>
        </p>
        <div
          className="mt-2 h-1.5 overflow-hidden bg-border"
          role="progressbar"
          aria-label="Runtime provisioning progress"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={percent}
        >
          <div className="h-full bg-foreground transition-[width] duration-300" style={{ width: `${percent}%` }} />
        </div>
        <p className="mt-2 text-muted-foreground">{message ?? "Contacting runtime…"}</p>
      </div>
    </div>
  );
}

function progressPercent(phase?: EnsureEvent["phase"]): number {
  switch (phase) {
    case "connect": return 10;
    case "install": return 18;
    case "provision": return 28;
    case "pull": return 38;
    case "probe": return 48;
    case "download": return 62;
    case "upload": return 74;
    case "launch": return 88;
    case "ready":
    case "skip": return 100;
    default: return 5;
  }
}

function Field({
  label,
  htmlFor,
  hint,
  children,
}: {
  label: string;
  htmlFor: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}

function SegButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "border px-3 py-2 text-xs transition-colors",
        active
          ? "border-ink/30 bg-accent"
          : "border-border hover:border-ink/25",
      )}
    >
      {children}
    </button>
  );
}

function LifecycleSelect({
  value,
  onChange,
}: {
  value: "temporary" | "permanent";
  onChange: (v: "temporary" | "permanent") => void;
}) {
  return (
    <Field label="Lifecycle" htmlFor="d-lifecycle">
      <Select
        value={value}
        onValueChange={(v) => onChange(v as "temporary" | "permanent")}
      >
        <SelectTrigger id="d-lifecycle">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="temporary">
            Temporary (reaped on disconnect)
          </SelectItem>
          <SelectItem value="permanent">Permanent (persists)</SelectItem>
        </SelectContent>
      </Select>
    </Field>
  );
}
