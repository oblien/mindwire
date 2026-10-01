/** Public Oblien calculator. Only the active rates and account plans are quoted. */
export const workspacePricingEndpoint =
  "https://api.oblien.com/pricing/calculator";

type ResourceLimits = {
  cpus: number | null;
  memory_mb: number | null;
  disk_size_mb: number | null;
};
export type WorkspacePlan = {
  family: string;
  name: string;
  price: number;
  credits_per_cycle: number;
  max_workspace: ResourceLimits;
  running_pool: ResourceLimits;
};

export type WorkspacePricing = {
  creditsPerDollar: number;
  rateCardId: string;
  rates: {
    cpu_per_min: number;
    memory_per_gb_min: number;
    disk_per_gb: number;
    network_per_gb: number;
  };
  plans: WorkspacePlan[];
};

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function amount(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}

function limits(value: unknown): value is ResourceLimits {
  return (
    record(value) &&
    ["cpus", "memory_mb", "disk_size_mb"].every(
      (key) => value[key] === null || amount(value[key]),
    )
  );
}

// Fail closed on an API/schema error. A preview compute card must never become
// a fallback quote; the API currently includes both active and proposed rates.
export function parseWorkspacePricing(value: unknown): WorkspacePricing | null {
  if (
    !record(value) ||
    value.success !== true ||
    !amount(value.credits_per_dollar) ||
    value.credits_per_dollar === 0 ||
    typeof value.rate_card_id !== "string" ||
    !value.rate_card_id ||
    !record(value.rates) ||
    !Array.isArray(value.plans)
  )
    return null;

  const rates = value.rates;
  if (
    ![
      "cpu_per_min",
      "memory_per_gb_min",
      "disk_per_gb",
      "network_per_gb",
    ].every((key) => amount(rates[key]))
  )
    return null;

  const families = ["free", "hobby", "pro", "scale"];
  const plans: WorkspacePlan[] = [];
  for (const family of families) {
    const matches = value.plans.filter(
      (plan) => record(plan) && plan.family === family,
    );
    if (matches.length === 0) continue;
    if (matches.length !== 1) return null;
    const plan = matches[0];
    if (
      !record(plan) ||
      typeof plan.name !== "string" ||
      !plan.name.trim() ||
      plan.name.length > 80 ||
      !amount(plan.price) ||
      !amount(plan.credits_per_cycle) ||
      !limits(plan.max_workspace) ||
      !limits(plan.running_pool)
    )
      return null;
    plans.push({
      family,
      name: plan.name,
      price: plan.price,
      credits_per_cycle: plan.credits_per_cycle,
      max_workspace: plan.max_workspace,
      running_pool: plan.running_pool,
    });
  }
  if (!plans.length) return null;

  return {
    creditsPerDollar: value.credits_per_dollar,
    rateCardId: value.rate_card_id,
    rates: rates as WorkspacePricing["rates"],
    plans,
  };
}

export async function getWorkspacePricing(): Promise<WorkspacePricing | null> {
  try {
    const response = await fetch(workspacePricingEndpoint, {
      headers: { Accept: "application/json" },
      next: { revalidate: 3600 },
      signal: AbortSignal.timeout(5000),
    });
    if (!response.ok) return null;
    return parseWorkspacePricing(await response.json());
  } catch {
    return null;
  }
}

export function usd(value: number) {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 0,
    maximumFractionDigits: 4,
  }).format(value);
}

export function resource(value: number | null, unit: "vCPU" | "GiB") {
  if (value === null) return "No fixed cap";
  return `${new Intl.NumberFormat("en-US", { maximumFractionDigits: 2 }).format(unit === "GiB" ? value / 1024 : value)} ${unit}`;
}
