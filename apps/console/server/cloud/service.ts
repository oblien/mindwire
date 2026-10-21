import type { BillingOffer } from "./provider";
import { COMPUTER_PLANS, computerPlanFitsImage } from "../../shared/cloud";
import { env } from "../env";
import { CloudProvider } from "./provider";
import { ComputerStore } from "./store";
import { CloudError, contract, type ComputerRow } from "./types";
import { monthlyComputerImage } from "./images";

export class ComputerService {
  constructor(readonly store = new ComputerStore(), readonly provider = new CloudProvider()) {}

  async create(owner: string, input: { idempotencyKey: string; name: string; planId: string; imageId: string }) {
    const plan = COMPUTER_PLANS.find(p => p.id === input.planId);
    if (!plan || !/^[A-Za-z0-9_-]{16,128}$/.test(input.idempotencyKey) || !input.name.trim() || input.name.length > 100 || input.imageId.length > 128)
      throw new CloudError(400, "invalid_computer", "Choose a monthly plan, computer name and supported image.");
    // Validate the provider's catalog before opening payment. No unpriced Mac/GPU
    // hardware, arbitrary namespace, extra VM or hidden config can enter checkout.
    const images = await this.provider.master().workspaces.images.list();
    const image = images.images.find(entry => entry.id === input.imageId);
    if (!image || !monthlyComputerImage(image))
      throw new CloudError(400, "unsupported_image", "Choose a supported Cloud Computer image.");
    if (!computerPlanFitsImage(plan, image))
      throw new CloudError(400, "computer_too_small", "Choose a larger monthly computer for this image.");
    const request = { image: image.image, ...(image.preset ? { preset: image.preset } : {}), mode: "permanent", cpus: plan.vcpus, memory_mb: plan.memoryMb,
      disk_size_mb: plan.diskGb * 1024, config: { ...image.vm_defaults } };
    return this.store.create(owner, input.idempotencyKey, input.name.trim(), plan, image.image, request);
  }

  async checkout(owner: string, id: string, customer: { email: string; name?: string }) {
    return this.store.locked(owner, id, async (row, save) => {
      if (!["draft", "checkout_pending"].includes(row.state)) return row;
      const saved = contract(row);
      if (!saved.checkoutRequest) {
        const offer = await this.provider.offer(saved.plan, saved.version);
        await this.provider.ensureNamespace(row.namespace, row.name);
        saved.checkoutRequest = { namespace: row.namespace, kind: "subscription", offer,
          billingInterval: "monthly", idempotencyKey: `mindwire-computer:${row.id}:checkout:v1`,
          customer, metadata: { product: "mindwire-computer", computer: row.id },
          successUrl: new URL(`/computers?computer=${encodeURIComponent(row.id)}`, env.baseUrl).href,
          cancelUrl: new URL(`/computers?computer=${encodeURIComponent(row.id)}`, env.baseUrl).href };
        row.contract = JSON.stringify(saved);
        row.state = "checkout_pending";
        // Persist the exact provider request BEFORE dispatch. A timeout is an
        // uncertain result, never permission to start another subscription.
        await save(row);
      }
      const checkout = await this.provider.checkout(saved.checkoutRequest);
      row.checkout_id = checkout.checkoutId; row.checkout_url = checkout.url;
      row.state = "payment_pending"; row.error = null;
      await save(row);
      return row;
    });
  }

  /** Used by browser polling, native polling and background reconciliation alike. */
  async reconcile(owner: string, id: string): Promise<ComputerRow> {
    const before = await this.store.get(owner, id);
    if (before.state === "checkout_pending") {
      const request = contract(before).checkoutRequest;
      const customer = request?.customer as { email: string; name?: string } | undefined;
      if (!customer?.email) throw new CloudError(409, "contract_missing", "The saved checkout needs recovery by Mindwire support.");
      await this.checkout(owner, id, customer);
    }
    return this.store.locked(owner, id, async (row, save) => {
      if (["draft", "checkout_pending", "deleting", "deleted"].includes(row.state)) return row;
      const saved = contract(row), offer = saved.checkoutRequest?.offer as BillingOffer | undefined;
      if (!offer) throw new CloudError(409, "contract_missing", "This computer needs support to recover its saved subscription.");
      const confirmed = await this.provider.paid(row.namespace, offer);
      if (confirmed.subscription) { saved.subscription = confirmed.subscription; row.contract = JSON.stringify(saved); await save(row); }
      if (row.error) { row.error = null; await save(row); }
      if (!confirmed.paid) return row;
      if (row.workspace_id) {
        if (row.state === "provisioning") {
          const workspace = await (await this.provider.scoped(row.namespace)).workspaces.get(row.workspace_id);
          if (workspace.ready) { row.state = "ready"; await save(row); }
        }
        return row;
      }
      row.state = "paid"; row.error = null; await save(row);
      // Creation retries always reuse the same provider key, including a lost
      // reply or a replica taking over after a process crash.
      const workspace = await this.provider.provision(row.namespace, { ...saved.request, name: row.name, idempotency_key: `mindwire-computer:${row.id}:create:v1` });
      if (!workspace.id || workspace.namespace !== row.namespace)
        throw new CloudError(502, "cloud_scope_mismatch", "Cloud returned a different computer.");
      row.workspace_id = workspace.id; row.state = workspace.ready ? "ready" : "provisioning";
      await save(row);
      return row;
    });
  }

  async cancelCheckout(owner: string, id: string) {
    return this.store.locked(owner, id, async (row, save) => {
      if (row.state === "draft") { row.state = "deleted"; await save(row); return row; }
      if (row.state === "checkout_pending" && !row.checkout_id) {
        const saved = contract(row);
        if (!saved.checkoutRequest) throw new CloudError(409, "checkout_pending", "The saved purchase needs recovery before it can be canceled.");
        const checkout = await this.provider.checkout(saved.checkoutRequest);
        row.checkout_id = checkout.checkoutId; row.checkout_url = checkout.url;
        row.state = "payment_pending"; await save(row);
      }
      if (!["payment_pending", "checkout_pending"].includes(row.state) || !row.checkout_id)
        throw new CloudError(409, "checkout_pending", "Check the saved checkout before starting another purchase.");
      const result = await this.provider.master()._http.request<{ namespace: string; checkout: { id: string; status: string; paymentStatus: string } }>({
        method: "POST", path: `/billing/checkout/${encodeURIComponent(row.checkout_id)}/cancel`,
        body: { namespace: row.namespace, idempotencyKey: `mindwire-computer:${row.id}:cancel-checkout` },
      });
      if (result.namespace !== row.namespace || result.checkout.id !== row.checkout_id || result.checkout.status !== "expired" || result.checkout.paymentStatus !== "unpaid")
        throw new CloudError(409, "checkout_paid_or_pending", "Payment may already have completed. Refresh this computer instead of starting another purchase.");
      // Keep the closed purchase as a tombstone. A new plan gets a new computer
      // and key, never a reused expired Stripe session.
      row.state = "deleted"; row.checkout_url = null; await save(row); return row;
    });
  }

  async subscription(owner: string, id: string, action: "cancel" | "resume") {
    return this.store.locked(owner, id, async (row, save) => {
      if (["deleted", "deleting", "draft", "checkout_pending", "payment_pending"].includes(row.state))
        throw new CloudError(409, "subscription_unavailable", "This computer does not have an active monthly subscription.");
      const billing = this.provider.master().billing;
      const result = action === "cancel" ? await billing.cancelSubscription(row.namespace) : await billing.resumeSubscription(row.namespace);
      if (result.namespace !== row.namespace || !result.subscription)
        throw new CloudError(502, "cloud_scope_mismatch", "Cloud returned a different computer subscription.");
      const saved = contract(row), s = result.subscription;
      saved.subscription = { status: s.status, paidThrough: s.periodEnd, cancelAtPeriodEnd: s.cancelAtPeriodEnd };
      row.contract = JSON.stringify(saved); row.error = null; await save(row); return row;
    });
  }
}
