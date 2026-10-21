import { Oblien } from "oblien";
import type { WorkspaceCreateParams } from "oblien";
export type CheckoutParams = Parameters<Oblien["billing"]["checkout"]>[0];
export type BillingOffer = NonNullable<CheckoutParams["offer"]>;
import { env } from "../env";
import { CloudError } from "./types";
import { providerFailure } from "./provider-errors";

export function providerBase(value = process.env.OBLIEN_BASE_URL ?? "https://api.oblien.com"): string {
  const url = new URL(value);
  if (url.username || url.password || url.search || url.hash || url.pathname !== "/" ||
      !(url.protocol === "https:" || (url.protocol === "http:" && ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname))))
    throw new Error("Cloud provider requires an HTTPS origin");
  return url.origin;
}

/** All provider requests are bounded and refuse redirects before sending secrets. */
export function providerClient(auth: { token: string | (() => Promise<string>) } | { clientId: string; clientSecret: string }) {
  const baseUrl = providerBase();
  const client = new Oblien({ ...("token" in auth ? { token: "" } : auth), baseUrl });
  client._http.request = async <T>(input: Parameters<Oblien["_http"]["request"]>[0]): Promise<T> => {
    if (!input.path.startsWith("/") || input.path.startsWith("//") || input.path.includes("\\")) throw new Error("Invalid provider path");
    const url = new URL(baseUrl + input.path);
    for (const [key, value] of Object.entries(input.query ?? {})) if (value !== undefined) url.searchParams.set(key, String(value));
    const headers: Record<string, string> = { Accept: "application/json", "Content-Type": "application/json" };
    if ("token" in auth) headers.Authorization = `Bearer ${typeof auth.token === "function" ? await auth.token() : auth.token}`;
    else { headers["X-Client-ID"] = auth.clientId; headers["X-Client-Secret"] = auth.clientSecret; }
    let response: Response;
    try {
      response = await fetch(url, { method: input.method, headers, body: input.body === undefined ? undefined : JSON.stringify(input.body),
        signal: AbortSignal.timeout(20000), redirect: "error" });
    } catch { throw providerFailure(input.method, input.path, 0); }
    const payload = await response.json().catch(() => null);
    if (!response.ok || (payload as { success?: unknown } | null)?.success === false) {
      throw providerFailure(input.method, input.path, response.status, payload);
    }
    if (payload === null && response.status !== 204) throw providerFailure(input.method, input.path, 502);
    return payload as T;
  };
  return client;
}

const namespaceGrants = new Map<string, { token: string; expires: number }>();
const pendingGrants = new Map<string, Promise<string>>();

export class CloudProvider {
  configured(): boolean { return env.mode === "cloud" && !!process.env.OBLIEN_CLIENT_ID && !!process.env.OBLIEN_CLIENT_SECRET; }
  master() {
    if (!this.configured()) throw new CloudError(503, "cloud_not_configured", "Cloud Computers are not available on this Console.");
    return providerClient({ clientId: process.env.OBLIEN_CLIENT_ID!, clientSecret: process.env.OBLIEN_CLIENT_SECRET! });
  }
  async scoped(namespace: string) {
    return providerClient({ token: () => this.scopedToken(namespace) });
  }
  async scopedToken(namespace: string): Promise<string> {
    const key = `${providerBase()}:${process.env.OBLIEN_CLIENT_ID}:${namespace}`;
    const cached = namespaceGrants.get(key);
    if (cached && cached.expires > Date.now() + 30000) return cached.token;
    const pending = pendingGrants.get(key);
    if (pending) return pending;
    const issuance = (async () => {
      const grant = await this.master().tokens.create({ scope: "namespace", namespace, ttl: 300 });
      if (grant.scope !== "namespace" || typeof grant.token !== "string" || !grant.token || !(Date.parse(grant.expiresAt) > Date.now()))
        throw new CloudError(502, "cloud_scope_mismatch", "Cloud returned an invalid computer credential.");
      if (namespaceGrants.size >= 1000) namespaceGrants.delete(namespaceGrants.keys().next().value!);
      namespaceGrants.set(key, { token: grant.token, expires: Math.min(Date.now() + 300000, Date.parse(grant.expiresAt)) });
      return grant.token;
    })();
    pendingGrants.set(key, issuance);
    try { return await issuance; } finally { pendingGrants.delete(key); }
  }
  async ensureNamespace(namespace: string, name: string) {
    const result = await this.master().namespaces.ensure({ slug: namespace, name: `Mindwire ${name}`,
      resource_limits: { max_workspaces: 0, max_vcpus: 0, max_ram_mb: 0, max_disk_gb: 0 } });
    if (result.data?.slug !== namespace) throw new CloudError(502, "cloud_scope_mismatch", "Cloud returned a different computer namespace.");
  }
  async offer(plan: { id: string; name: string; monthlyCents: number; vcpus: number; memoryMb: number; diskGb: number }, version: string): Promise<BillingOffer> {
    const catalog = await this.master().billing.capacityCatalog();
    const rate = catalog.tariff?.monthly;
    const cost = rate && Math.max(rate.minimumCents, Math.ceil(plan.vcpus * rate.vcpuCents + plan.memoryMb / 1024 * rate.memoryGiBCents + plan.diskGb * rate.diskGiBCents));
    if (!cost || !Number.isFinite(cost) || cost > plan.monthlyCents || !catalog.paymentSources?.monthly?.includes("stripe"))
      throw new CloudError(503, "pricing_unavailable", "This monthly computer is not available for purchase yet.");
    return { reference: `${version}:${plan.id}`, name: `Mindwire Cloud Computer · ${plan.name}`, unitAmount: plan.monthlyCents,
      currency: "usd", billingMode: "monthly", credits: 0, tariffId: catalog.tariffId,
      capacity: { vcpus: plan.vcpus, memoryMb: plan.memoryMb, diskGb: plan.diskGb, workspaces: 1 },
      resourceLimits: { max_workspaces: 1, max_vcpus: plan.vcpus, max_ram_mb: plan.memoryMb, max_disk_gb: plan.diskGb,
        max_total_vcpus: plan.vcpus, max_total_ram_mb: plan.memoryMb, max_total_disk_gb: plan.diskGb } };
  }
  async checkout(input: Record<string, unknown>) {
    const result = await this.master().billing.checkout(input as unknown as CheckoutParams);
    if (!result.checkoutId || !result.url) throw new CloudError(502, "invalid_checkout", "Cloud returned an invalid checkout.");
    hostedUrl(result.url, ["checkout.stripe.com", "api.oblien.com"]);
    return result;
  }
  async paid(namespace: string, expected: BillingOffer) {
    const [entitlement, subscription] = await Promise.all([this.master().billing.entitlement(namespace), this.master().billing.subscription(namespace)]);
    if (entitlement.namespace !== namespace || subscription.namespace !== namespace)
      throw new CloudError(502, "cloud_scope_mismatch", "Cloud returned a different computer subscription.");
    const e = entitlement, s = subscription.subscription;
    const a = e.capacity?.capacity, b = expected.capacity;
    const paid = !!s && ["active", "trialing", "past_due"].includes(s.status) && ["active", "past_due"].includes(e.status) &&
      e.billingMode === "monthly" && e.computeCovered === true && !!e.periodEnd && Date.parse(e.periodEnd) > Date.now() &&
      !!a && !!b && a.vcpus === b.vcpus && a.memoryMb === b.memoryMb && a.diskGb === b.diskGb && a.workspaces === 1 &&
      s.offer?.reference === expected.reference && s.offer?.unitAmount === expected.unitAmount;
    return { paid, subscription: s ? { status: s.status, paidThrough: s.periodEnd, cancelAtPeriodEnd: s.cancelAtPeriodEnd } : undefined };
  }
  async provision(namespace: string, request: Record<string, unknown>) {
    return (await this.scoped(namespace)).workspaces.create({ ...request, namespace, wait_ready: false } as WorkspaceCreateParams);
  }
}

export function hostedUrl(value: string, hosts: string[]): string {
  const url = new URL(value);
  if (url.protocol !== "https:" || !hosts.includes(url.hostname) || url.port || url.username || url.password)
    throw new CloudError(502, "invalid_checkout", "Cloud returned an invalid payment address.");
  if (url.hostname === "api.oblien.com" && !(url.pathname === "/billing/pay" && !url.search && /^#[A-Za-z0-9_-]{43}$/.test(url.hash)))
    throw new CloudError(502, "invalid_checkout", "Cloud returned an invalid payment address.");
  return value;
}
