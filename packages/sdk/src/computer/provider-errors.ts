export type ProviderErrorCode = "provider_sign_in" | "provider_limit" | "provider_missing" | "provider_certificate" | "provider_configuration" | "provider_connection";

export class ProviderConnectionError extends Error {
  constructor(readonly code: ProviderErrorCode, message: string) { super(message); this.name = "ProviderConnectionError"; }
}

export function providerNeedsAction(code: string | undefined): boolean {
  return ["provider_sign_in", "provider_limit", "provider_missing", "provider_certificate", "provider_configuration"].includes(code ?? "");
}

/** Inspect transport causes, but never forward their raw text or headers. */
export function hasCertificateError(error: unknown): boolean {
  const seen = new Set<unknown>();
  for (let current = error; current && typeof current === "object" && seen.size < 8 && !seen.has(current);) {
    seen.add(current);
    const value = current as { message?: string; code?: string; cause?: unknown };
    if (["CERT_HAS_EXPIRED", "CERT_NOT_YET_VALID", "ERR_TLS_CERT_ALTNAME_INVALID", "DEPTH_ZERO_SELF_SIGNED_CERT",
      "SELF_SIGNED_CERT_IN_CHAIN", "UNABLE_TO_VERIFY_LEAF_SIGNATURE", "UNABLE_TO_GET_ISSUER_CERT_LOCALLY", "CERT_REVOKED"].includes(value.code ?? "")
      || /certificate (?:has expired|expired|verify failed)|self[- ]signed certificate|CERT_HAS_EXPIRED/i.test(value.message ?? "")) return true;
    current = value.cause;
  }
  return false;
}

export function providerFailure(error: unknown, provider: string): ProviderConnectionError {
  if (error instanceof ProviderConnectionError) return error;
  const value = error as { status?: number; code?: string; message?: string } | undefined;
  const setup = `mindwire connection ${provider.toLowerCase()}`;
  if (hasCertificateError(error)) return new ProviderConnectionError("provider_certificate",
    `${provider}'s TLS certificate could not be verified. The tunnel provider must fix its certificate; also check this computer's date and time. Your saved phone keys are unchanged.`);
  if (value?.code === "provider_sign_in" || [401, 403].includes(value?.status ?? 0)
    || /unexpected server response: (?:401|403)\b/i.test(value?.message ?? "")) {
    return new ProviderConnectionError("provider_sign_in", `Sign in to ${provider} again with ${setup}. Your saved phone keys are unchanged.`);
  }
  if (value?.status === 402) return new ProviderConnectionError("provider_limit", `${provider}'s tunnel allowance was reached. Check your plan in its dashboard or choose another provider.`);
  if (value?.status === 404) return new ProviderConnectionError("provider_missing", `This computer's ${provider} tunnel was removed. Run ${setup} to configure it again.`);
  if (value?.status === 400 || value?.code === "provider_configuration") return new ProviderConnectionError("provider_configuration", `${provider}'s connection settings are incomplete or invalid. Run ${setup} to configure it again.`);
  return new ProviderConnectionError("provider_connection", `${provider} couldn't connect. Check internet access and the provider account, then run ${setup}.`);
}
