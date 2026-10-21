import { randomUUID } from "node:crypto";
import { CloudError } from "./types";

interface Failure {
  status: CloudError["status"];
  message: string;
  retryable: boolean;
}

// Only our own messages and known codes leave this boundary. Provider bodies can
// contain customer/payment data; never log or return their message/details fields.
const failures: Record<string, Failure> = {
  billing_redirect_not_allowed: { status: 503, retryable: false, message: "Cloud checkout is blocked because this console's return address is not enabled by the billing provider. Contact Mindwire support." },
  invalid_redirect_url: { status: 503, retryable: false, message: "Cloud checkout needs a valid HTTPS return address. Contact Mindwire support." },
  reseller_enterprise_required: { status: 503, retryable: false, message: "Cloud purchases are not enabled for Mindwire's provider account. Contact Mindwire support." },
  billing_provider_configuration_error: { status: 503, retryable: false, message: "The Cloud billing provider's payment configuration needs attention. Contact Mindwire support." },
  billing_database_collation_error: { status: 503, retryable: false, message: "Cloud billing is blocked by a database configuration error at the provider. Contact Mindwire support." },
  billing_storage_unavailable: { status: 503, retryable: true, message: "Cloud billing could not save this purchase. Retry the existing purchase shortly." },
  billing_provider_busy: { status: 503, retryable: true, message: "Cloud billing is busy. Retry the existing purchase shortly." },
  billing_provider_unavailable: { status: 503, retryable: true, message: "Cloud billing is temporarily unavailable. Retry the existing purchase shortly." },
  billing_provider_rejected: { status: 502, retryable: false, message: "The payment provider rejected this checkout. Contact Mindwire support." },
  billing_internal_error: { status: 502, retryable: false, message: "The Cloud provider could not complete this checkout. Contact Mindwire support." },
  billing_provider_record_missing: { status: 409, retryable: false, message: "This purchase's billing record needs recovery. Contact Mindwire support before starting another purchase." },
  billing_idempotency_conflict: { status: 409, retryable: false, message: "This purchase is already saved with different details. Contact Mindwire support to recover it." },
  billing_checkout_reconciliation_required: { status: 409, retryable: false, message: "This unfinished purchase needs recovery. Contact Mindwire support before starting another purchase." },
  checkout_expired: { status: 409, retryable: false, message: "This checkout has expired. Close this purchase before starting a new one." },
  capacity_checkout_expired: { status: 409, retryable: false, message: "This checkout has expired. Close this purchase before starting a new one." },
  invalid_offer: { status: 502, retryable: false, message: "The Cloud provider rejected this plan's billing configuration. Contact Mindwire support." },
  insufficient_redeemable_balance: { status: 503, retryable: false, message: "Cloud purchases are paused because Mindwire's provider funding needs attention. Contact Mindwire support." },
};

const authenticationCodes = new Set(["unauthorized", "missing_credentials", "invalid_credentials", "credentials_expired", "client_inactive"]);
const permissionCodes = new Set(["forbidden", "scope_denied", "permission_denied"]);
const identifier = (value: unknown) => typeof value === "string" && /^[A-Za-z0-9_-]{8,80}$/.test(value) ? value : undefined;

export function providerFailure(method: string, path: string, status: number, payload?: unknown): CloudError {
  const body = payload && typeof payload === "object" ? payload as Record<string, unknown> : {};
  const rawCode = typeof body.code === "string" ? body.code : typeof body.error === "string" ? body.error : "";
  const providerCode = rawCode.length <= 80 && /^[a-z][a-z0-9]*(?:_[a-z0-9]+)+$/.test(rawCode) ? rawCode : undefined;
  const known = Object.hasOwn(failures, rawCode);
  let code = known ? rawCode : "cloud_unavailable";
  let failure: Failure = known ? failures[rawCode] : {
    status: 503, retryable: true, message: "Cloud Computers are temporarily unavailable. Retry the existing operation shortly.",
  };
  if (!known) {
    if (status === 401 || authenticationCodes.has(rawCode)) {
      code = "cloud_provider_authentication";
      failure = { status: 503, retryable: false, message: "Mindwire's Cloud provider credentials were rejected. Contact Mindwire support." };
    } else if (status === 403 || permissionCodes.has(rawCode)) {
      code = "cloud_provider_permissions";
      failure = { status: 503, retryable: false, message: "Mindwire's Cloud provider account does not have permission for this operation. Contact Mindwire support." };
    } else if (status === 404) {
      code = "computer_not_found";
      failure = { status: 404, retryable: false, message: "The requested Cloud resource could not be found." };
    } else if (status === 409) {
      code = "cloud_conflict";
      failure = { status: 409, retryable: false, message: "This Cloud operation conflicts with its saved state. Refresh the existing computer or contact Mindwire support." };
    } else if (status === 429) {
      code = "cloud_rate_limited";
      failure = { status: 429, retryable: true, message: "Too many Cloud requests. Wait briefly before retrying." };
    } else if ((status >= 400 && status < 500) || status === 200) {
      code = "cloud_request_rejected";
      failure = { status: 502, retryable: false, message: "The Cloud provider rejected this computer request. Contact Mindwire support." };
    }
  }
  const details = body.details && typeof body.details === "object" ? body.details as Record<string, unknown> : {};
  if (details.retryable === false) failure = { ...failure, retryable: false };
  const reference = randomUUID();
  console.error("[cloud] provider request failed", JSON.stringify({
    reference, method, path: path.split("?")[0], status, code, providerCode,
    providerRequestId: identifier(body.requestId), providerReference: identifier(details.reference),
    retryable: failure.retryable,
  }));
  return new CloudError(failure.status, code, failure.message, { reference, retryable: failure.retryable });
}
