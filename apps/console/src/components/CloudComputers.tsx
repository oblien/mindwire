import { useEffect, useState, type FormEvent } from "react";
import { Link, Navigate, useNavigate, useSearchParams } from "react-router-dom";
import { ArrowRight, Check, Cpu, Loader2, Monitor, Plus } from "lucide-react";
import { computerPlanFitsImage, type CloudComputer, type CloudImage } from "@shared/cloud";
import { useCloud, computerState } from "@/lib/cloud";
import { primaryComputerImages } from "@/lib/computer-images";
import { ComputerSystemPicker } from "./ComputerSystemPicker";
import { AccountArtwork } from "./AccountArtwork";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import {
  PageTitle,
  ErrorNotice,
  Loading,
  money,
  date,
  ConfirmAction,
} from "./portal/common";

export function ComputerRow({ computer }: { computer: CloudComputer }) {
  return (
    <Link
      to={`/computers/${computer.id}`}
      className="group flex flex-wrap items-center gap-4 border-b border-border/60 p-5 transition-colors last:border-0 hover:bg-secondary"
    >
      <span className="grid size-11 shrink-0 place-items-center rounded-2xl bg-[var(--mw-blue-soft)] text-[var(--mw-blue)]">
        <Monitor className="size-5" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{computer.name}</p>
        <p className="mt-1 text-xs text-muted-foreground">
          {computer.plan.name} · {computer.plan.vcpus} vCPU ·{" "}
          {computer.plan.memoryMb / 1024} GB RAM · {computer.plan.diskGb} GB
          storage
        </p>
      </div>
      <span className="rounded-full bg-secondary px-3 py-1.5 text-xs text-muted-foreground">
        {computerState(computer)}
      </span>
      <ArrowRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-1" />
    </Link>
  );
}
export function CloudComputers() {
  const cloud = useCloud();
  const [search] = useSearchParams();
  // Keep saved checkout returns and public pricing links working across the portal refactor.
  const computer = search.get("computer"),
    plan = search.get("plan");
  if (computer)
    return (
      <Navigate replace to={`/computers/${encodeURIComponent(computer)}`} />
    );
  if (plan)
    return (
      <Navigate
        replace
        to={`/computers/new?plan=${encodeURIComponent(plan)}`}
      />
    );
  return (
    <div className="space-y-7">
      <PageTitle
        title="Cloud Computers"
        description="Your managed computers, capacity and monthly plans. Connect from the Mindwire app to start working."
        action={
          <Button asChild>
            <Link to="/computers/new">
              <Plus />
              New computer
            </Link>
          </Button>
        }
      />
      {!cloud.loaded ? (
        cloud.error ? null : <Loading />
      ) : cloud.computers.length ? (
        <Card className="overflow-hidden">
          <CardContent className="p-0">
            {cloud.computers.map((c) => (
              <ComputerRow key={c.id} computer={c} />
            ))}
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardContent className="flex flex-col items-center gap-4 px-6 py-8 text-center">
            <AccountArtwork className="w-full max-w-[290px]" />
            <div>
              <h2 className="text-2xl font-medium tracking-tight">
                Your next computer, ready when you are
              </h2>
              <p className="mt-2 max-w-sm text-sm leading-relaxed text-muted-foreground">
                Choose a monthly plan for a managed Linux or Mac computer. Your
                own computers stay free to use in Mindwire.
              </p>
            </div>
            <Button asChild>
              <Link to="/computers/new">
                Choose a computer
                <ArrowRight />
              </Link>
            </Button>
          </CardContent>
        </Card>
      )}
      <p className="text-xs leading-relaxed text-muted-foreground">
        Personal computers and SSH connections are managed in the Mindwire app.
        They do not require a Cloud subscription.
      </p>
    </div>
  );
}
export function PlanCards({
  selected,
  onSelect,
  image,
  disabled,
}: {
  selected?: string;
  onSelect?: (id: string) => void;
  image?: CloudImage;
  disabled?: boolean;
}) {
  const { catalog } = useCloud();
  return (
    <div className="grid gap-4 min-[700px]:grid-cols-3">
      {catalog?.plans.map((plan, index) => (
        <Card
          key={plan.id}
          style={{ background: `var(--mw-${["peach", "blue", "mint"][index % 3]}-soft)` }}
          className={selected === plan.id ? "ring-2 ring-foreground ring-offset-4 ring-offset-background" : ""}
        >
          <CardContent className="space-y-6 p-6">
            <div className="flex items-center justify-between">
              <h3 className="font-medium">{plan.name}</h3>
              {selected === plan.id ? (
                <Check className="size-4" />
              ) : (
                <Cpu className="size-4 text-muted-foreground" />
              )}
            </div>
            <p className="text-4xl font-medium tracking-tight">
              {money(plan.monthlyCents)}
              <span className="ml-1 text-sm font-normal text-muted-foreground">
                /month
              </span>
            </p>
            <ul className="space-y-2 text-sm text-muted-foreground">
              <li>{plan.vcpus} shared vCPU</li>
              <li>{plan.memoryMb / 1024} GB RAM</li>
              <li>{plan.diskGb} GB storage</li>
            </ul>
            {onSelect ? (
              <Button
                type="button"
                variant={selected === plan.id ? "default" : "outline"}
                className="w-full"
                aria-pressed={selected === plan.id}
                disabled={disabled || (!!image && !computerPlanFitsImage(plan, image))}
                onClick={() => onSelect(plan.id)}
              >
                {image && !computerPlanFitsImage(plan, image) ? "Too small for this system" : selected === plan.id ? "Selected" : `Choose ${plan.name}`}
              </Button>
            ) : (
              <Button asChild variant="outline" className="w-full">
                <Link to={`/computers/new?plan=${plan.id}`}>
                  Choose {plan.name}
                </Link>
              </Button>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
export function NewComputer() {
  const cloud = useCloud(),
    navigate = useNavigate(),
    [search] = useSearchParams();
  const [name, setName] = useState("My computer"),
    [image, setImage] = useState(""),
    [plan, setPlan] = useState(search.get("plan") ?? "essential");
  const [key, setKey] = useState(() => crypto.randomUUID()),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [loadingImages, setLoadingImages] = useState(true),
    [retry, setRetry] = useState(0);
  useEffect(() => {
    if (!cloud.catalog?.available) {
      if (cloud.loaded) setLoadingImages(false);
      return;
    }
    let disposed = false;
    setLoadingImages(true);
    cloud
      .loadImages()
      .then((images) => {
        if (!disposed) {
          setImage((current) =>
            images.some((i) => i.id === current)
              ? current
              : (primaryComputerImages(images).linux?.id ?? ""),
          );
          setError("");
        }
      })
      .catch((error) => {
        if (!disposed) setError(error.message);
      })
      .finally(() => {
        if (!disposed) setLoadingImages(false);
      });
    return () => {
      disposed = true;
    };
  }, [cloud.catalog?.available, cloud.loaded, cloud.loadImages, retry]);
  const selectedImage = cloud.images.find((i) => i.id === image),
    selectedPlan = cloud.catalog?.plans.find((p) => p.id === plan);
  function selectImage(id: string) {
    setImage(id);
    setKey(crypto.randomUUID());
    const next = cloud.images.find((image) => image.id === id);
    if (next && selectedPlan && !computerPlanFitsImage(selectedPlan, next)) {
      const compatible = cloud.catalog?.plans.find((plan) => computerPlanFitsImage(plan, next));
      if (compatible) setPlan(compatible.id);
    }
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    let createdId: string | undefined;
    try {
      const created = await cloud.createComputer(name.trim(), image, plan, key);
      createdId = created.computer.id;
      const result = await cloud.action(createdId, "checkout");
      if (!result.computer?.checkout) navigate(`/computers/${createdId}`);
    } catch (error) {
      if (createdId) navigate(`/computers/${createdId}`);
      else setError((error as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="space-y-7">
      <PageTitle
        title="New Cloud Computer"
        description="Choose your system and monthly capacity. Review the payment before your computer is created."
        action={
          <Button asChild variant="ghost">
            <Link to="/computers">Cancel</Link>
          </Button>
        }
      />
      {error && (
        <ErrorNotice message={error} retry={() => setRetry((n) => n + 1)} />
      )}
      {!cloud.loaded || loadingImages ? (
        cloud.error && !cloud.loaded ? null : <Loading />
      ) : !cloud.catalog?.available ? (
        <ErrorNotice
          message="Cloud Computer purchases are not enabled yet. Your existing account and personal computers remain available."
          retry={() => void cloud.reload().catch(() => {})}
        />
      ) : (
        <form onSubmit={submit} className="space-y-7">
          <div className="grid gap-6 min-[1500px]:grid-cols-[minmax(0,1fr)_19rem]">
            <div className="space-y-7">
              <Card>
                <CardContent className="space-y-6 p-6">
                  <label className="block space-y-2 text-sm">
                    <span>Name</span>
                    <Input
                      value={name}
                      maxLength={100}
                      onChange={(e) => {
                        setName(e.target.value);
                        setKey(crypto.randomUUID());
                      }}
                      required
                      disabled={busy}
                    />
                  </label>
                  <ComputerSystemPicker images={cloud.images} plans={cloud.catalog?.plans ?? []}
                    value={image} onChange={selectImage} disabled={busy} />
                </CardContent>
              </Card>
              <div>
                <h2 className="mb-4 text-sm font-medium">Monthly capacity</h2>
                <PlanCards
                  selected={plan}
                  image={selectedImage}
                  disabled={busy || !selectedImage}
                  onSelect={(id) => {
                    setPlan(id);
                    setKey(crypto.randomUUID());
                  }}
                />
              </div>
              {selectedPlan && selectedImage && !computerPlanFitsImage(selectedPlan, selectedImage) && (
                <ErrorNotice message="Choose a larger plan for this system." />
              )}
            </div>
            <aside>
              <Card>
                <CardContent className="space-y-5 p-6">
                  <p className="text-sm font-medium text-muted-foreground">
                    Your computer
                  </p>
                  <p className="text-lg font-medium">
                    {name || "New computer"}
                  </p>
                  <div className="space-y-2 text-sm text-muted-foreground">
                    <p>{selectedImage?.label || selectedImage?.id}</p>
                    <p>
                      {selectedPlan?.name} · {selectedPlan?.vcpus} vCPU ·{" "}
                      {(selectedPlan?.memoryMb ?? 0) / 1024} GB RAM
                    </p>
                    <p>{selectedPlan?.diskGb} GB storage</p>
                  </div>
                  <div className="border-t border-border pt-5">
                    <p className="text-2xl font-semibold">
                      {money(selectedPlan?.monthlyCents ?? 0)}
                      <span className="text-sm font-normal text-muted-foreground">
                        {" "}
                        /month
                      </span>
                    </p>
                  </div>
                  <Button
                    type="submit"
                    className="w-full"
                    disabled={
                      busy ||
                      !name.trim() ||
                      !selectedImage ||
                      !selectedPlan ||
                      !computerPlanFitsImage(selectedPlan, selectedImage)
                    }
                  >
                    {busy ? <Loader2 className="animate-spin" /> : null}Continue
                    to checkout
                    <ArrowRight />
                  </Button>
                  <p className="text-xs leading-relaxed text-muted-foreground">
                    CPU, RAM and included storage are covered for your paid
                    month. Transfer and retained storage follow your
                    subscription terms.
                  </p>
                </CardContent>
              </Card>
            </aside>
          </div>
        </form>
      )}
    </div>
  );
}
export function Billing() {
  const cloud = useCloud(),
    [busy, setBusy] = useState<string | null>(null),
    [error, setError] = useState("");
  async function act(id: string, action: string) {
    setBusy(id);
    setError("");
    try {
      await cloud.action(id, action);
    } catch (error) {
      setError((error as Error).message);
    } finally {
      setBusy(null);
    }
  }
  return (
    <div className="space-y-7">
      <PageTitle
        title="Plans & billing"
        description="One subscription per Cloud Computer. Your personal computers and Mindwire account stay free."
      />
      {error && <ErrorNotice message={error} />}
      {cloud.loaded && !cloud.computers.length && <Card><CardContent className="flex flex-wrap items-center justify-between gap-5 p-6"><div><p className="text-base font-medium">Your own computer. Always free.</p><p className="mt-2 max-w-xl text-sm leading-relaxed text-muted-foreground">You have no Cloud subscriptions. Use Mindwire with your Mac or a server you already own, and add a Cloud Computer whenever you need one.</p></div><Button asChild variant="outline"><a href="https://mindwire.sh/download">Get Mindwire<ArrowRight /></a></Button></CardContent></Card>}
      {!!cloud.computers.length && (
        <section className="space-y-4">
          <h2 className="text-sm font-medium">Your subscriptions</h2>
          <Card>
            <CardContent className="divide-y divide-border p-0">
              {cloud.computers.map((c) => (
                <div
                  key={c.id}
                  className="flex flex-wrap items-center gap-4 p-5"
                >
                  <div className="min-w-0 flex-1">
                    <Link
                      to={`/computers/${c.id}`}
                      className="text-sm font-medium hover:underline"
                    >
                      {c.name}
                    </Link>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {c.plan.name} · {money(c.plan.monthlyCents)}/month ·{" "}
                      {c.subscription?.paidThrough
                        ? `${c.subscription.cancelAtPeriodEnd ? "Paid through" : "Renews"} ${date(c.subscription.paidThrough)}`
                        : computerState(c)}
                    </p>
                  </div>
                  {["draft", "checkout_pending", "payment_pending"].includes(
                    c.state,
                  ) ? (
                    <Button
                      size="sm"
                      disabled={busy !== null}
                      onClick={() => void act(c.id, "checkout")}
                    >
                      Continue checkout
                    </Button>
                  ) : (
                    <>
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={busy !== null}
                        onClick={() => void act(c.id, "billing-portal")}
                      >
                        Manage billing
                      </Button>
                      <ConfirmAction
                        disabled={busy !== null}
                        label={
                          c.subscription?.cancelAtPeriodEnd
                            ? "Resume renewal"
                            : "Stop renewal"
                        }
                        title={
                          c.subscription?.cancelAtPeriodEnd
                            ? "Resume this subscription?"
                            : "Stop the next renewal?"
                        }
                        description="Your current paid period and computer are preserved. This does not delete your computer or files."
                        onConfirm={() =>
                          act(
                            c.id,
                            `subscription/${c.subscription?.cancelAtPeriodEnd ? "resume" : "cancel"}`,
                          )
                        }
                      />
                    </>
                  )}
                </div>
              ))}
            </CardContent>
          </Card>
        </section>
      )}
      {!cloud.loaded ? (
        cloud.error ? null : <Loading />
      ) : (
        <section className="space-y-4">
          <h2 className="text-sm font-medium">Choose a Cloud Computer</h2>
          <PlanCards />
          <p className="text-xs leading-relaxed text-muted-foreground">
            Each plan includes shared CPU, RAM and storage for its paid month.
            Transfer and retained storage follow the subscription terms.
            Existing computers keep the plan purchased for them.
          </p>
        </section>
      )}
    </div>
  );
}
