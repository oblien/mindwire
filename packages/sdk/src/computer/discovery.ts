import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import type { Mindwire } from "../client.js";
import type { ComputerDiscoveryStatus } from "../computer.js";
import { acquireProcessLock } from "./lock.js";
import type { SetupPrompt } from "./setup-prompt.js";
import { directoryRequest, DirectoryRequestError } from "./directory-http.js";

export const DIRECTORY_API_PATH = "/api/computer-directory/v1";
export const DIRECTORY_URL = `https://console.mindwire.sh${DIRECTORY_API_PATH}`;

export function directoryURL(value: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("Enter the directory's HTTPS API URL.");
  }
  if (
    value.length > 2048 ||
    /[\s\x00]/.test(value) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    (url.protocol !== "https:" &&
      !(url.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)))
  ) {
    throw new Error("Use an HTTPS directory URL without credentials, a query, or a fragment.");
  }
  if (url.pathname === "/") url.pathname = DIRECTORY_API_PATH;
  if (url.pathname.endsWith("/") || url.pathname.includes("//") || url.pathname.includes("%")) {
    throw new Error("Use the exact directory API URL without a trailing slash.");
  }
  return url.href;
}

export async function enableAddressDiscovery(options: {
  directory: string;
  computer: Mindwire;
  prompt: SetupPrompt;
  url?: string;
}): Promise<ComputerDiscoveryStatus> {
  const lock = await acquireProcessLock(join(options.directory, "discovery-setup.lock"));
  try {
    let status: ComputerDiscoveryStatus;
    try {
      status = await options.computer.computer.discovery();
    } catch (error) {
      if ((error as { status?: number }).status === 404)
        throw new Error("Update the Mindwire service before enabling automatic address recovery.");
      throw error;
    }
    // Preserve a saved private directory across upgrades. Credentials belonging
    // to another provider are never read by this flow.
    const url = directoryURL(
      options.url ?? process.env.MINDWIRE_DIRECTORY_URL ?? status.url ?? DIRECTORY_URL,
    );
    if (
      status.url &&
      status.url !== url &&
      (status.enabled || (status.sequence ?? 0) !== (status.publishedSequence ?? 0))
    ) {
      throw new Error(
        "Disable the current directory with mindwire discovery disable and wait for its withdrawal before changing the directory URL.",
      );
    }
    if (!status.enabled || status.url !== url || status.error?.includes("enrollment")) {
      options.prompt.print(`Enabling encrypted address recovery with ${new URL(url).host}…`);
      try {
        const challenge = await directoryRequest<{ challenge: string }>(url, "/registrations/challenge", {
          directoryId: status.directoryId,
        });
        if (
          typeof challenge?.challenge !== "string" ||
          challenge.challenge.length < 16 ||
          challenge.challenge.length > 2048
        ) {
          throw new Error("The address directory returned an invalid enrollment challenge.");
        }
        const proof = await options.computer.computer.directoryEnrollment(url, challenge.challenge);
        const enrolled = await directoryRequest<{ directoryId: string }>(url, "/registrations", proof);
        if (enrolled?.directoryId !== status.directoryId)
          throw new Error("The directory acknowledged a different computer.");
      } catch (error) {
        if (error instanceof DirectoryRequestError) {
          if (error.status === 404 || error.status === 501)
            throw new Error(
              "The address directory isn't available at this URL. Your existing connection still works; retry after the backend is deployed.",
            );
          if (error.status === 409)
            throw new Error(
              "This computer's directory registration is revoked or conflicts with its saved identity. Contact the directory operator.",
            );
          if (error.code === "account_required")
            throw new Error(
              "This directory still requires an account. Update its Mindwire backend to enable key-based registration, or choose another directory with --directory-url.",
            );
          if (error.status === 401 || error.status === 403)
            throw new Error(
              "The directory couldn't verify this computer's key or challenge. Retry enrollment.",
            );
          if (error.status === 429)
            throw new Error(
              "The directory's registration limit was reached. Retry later or use another directory.",
            );
          if (error.status === 503)
            throw new Error(
              "The directory is temporarily unavailable or at capacity. Your existing connection still works.",
            );
        }
        throw error;
      }
      status = await options.computer.computer.configureDiscovery(true, url);
    }
    const deadline = Date.now() + 15_000;
    while (Date.now() < deadline) {
      status = await options.computer.computer.discovery();
      if (status.enabled && status.sequence && status.publishedSequence === status.sequence) return status;
      if (status.error?.includes("enrollment") || status.error?.includes("older revision"))
        throw new Error(status.error);
      await delay(250);
    }
    throw new Error(
      "Address recovery is configured and will retry in the background. Run mindwire discovery status to check publication.",
    );
  } finally {
    await lock?.close();
  }
}

export function discoveryDescription(status: ComputerDiscoveryStatus | undefined): string {
  if (!status?.enabled) return "Automatic address recovery is off. Enable it with mindwire discovery enable.";
  if (status.error) return status.error;
  return status.sequence && status.publishedSequence === status.sequence
    ? "Encrypted address recovery is on. Saved phones can find this computer when its tunnel address changes."
    : "Publishing the computer's encrypted address…";
}
