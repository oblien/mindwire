import type { Mindwire } from "./client.js";

export type ComputerRoute = { kind: "ssh"; host: string; port: number } | { kind: "websocket"; url: string };
export interface ComputerInvitation {
  version: 1; computerId: string; name: string; fingerprint: string; routes: ComputerRoute[];
  pairingId: string; secret: string; expiresAt: string;
}
export interface ComputerDevice { id: string; name: string; publicKey: string; createdAt: string; revoked: boolean }
export interface ComputerPairRequest { id: string; name: string; publicKey: string }
export interface ComputerPairing {
  id: string; expiresAt: string; status: "waiting" | "pending" | "approved" | "rejected";
  request?: ComputerPairRequest;
  completed?: boolean;
}
export interface ComputerUpdate {
  id: string; version: string; updatedAt: string; error?: string;
  status: "idle" | "queued" | "downloading" | "waiting" | "restarting" | "complete" | "failed";
}
export interface ComputerInfo {
  version: number; computerId: string; registryId: string; fingerprint: string; routes: ComputerRoute[];
  pid: number; apiAddress: string; sshPort: number; websocketPort: number;
  portForwardingVersion?: number;
}
export interface ComputerForwardRequest { id: string; deviceId: string; port: number }
export interface ComputerForward extends ComputerForwardRequest { expiresAt: string }
/** Available only in computer mode. All calls use the existing authenticated transport. */
export class ComputerApi {
  constructor(private readonly client: Mindwire) {}
  info(): Promise<ComputerInfo> { return this.client.http.request("GET", "/computer"); }
  setRoutes(routes: ComputerRoute[]): Promise<ComputerInfo> {
    return this.client.http.request("PUT", "/computer/routes", { body: { routes } });
  }
  updateStatus(): Promise<ComputerUpdate> { return this.client.http.request("GET", "/computer/update"); }
  requestUpdate(version: string): Promise<ComputerUpdate> {
    return this.client.http.request("POST", "/computer/update", { body: { version } });
  }
  invite(routes: ComputerRoute[]): Promise<ComputerInvitation> {
    return this.client.http.request("POST", "/computer/pairings", { body: { routes } });
  }
  pairing(id: string): Promise<ComputerPairing> { return this.client.http.request("GET", `/computer/pairings/${encodeURIComponent(id)}`); }
  completePairing(id: string, requestId: string): Promise<{ ok: boolean }> {
    return this.client.http.request("POST", `/computer/pairings/${encodeURIComponent(id)}/complete`, { body: { requestId } });
  }
  decide(id: string, requestId: string, approve: boolean): Promise<{ ok: boolean; device: ComputerDevice }> {
    return this.client.http.request("POST", `/computer/pairings/${encodeURIComponent(id)}/decision`, { body: { requestId, approve } });
  }
  devices(): Promise<ComputerDevice[]> { return this.client.http.request("GET", "/computer/devices"); }
  revoke(id: string): Promise<{ ok: boolean }> { return this.client.http.request("DELETE", `/computer/devices/${encodeURIComponent(id)}`); }
  forwards(): Promise<ComputerForward[]> { return this.client.http.request("GET", "/computer/forwards"); }
  /** Authorize a loopback port for this paired device. Renew the same ID every 30s;
   * grants expire after two minutes. Carry traffic using SSH direct-tcpip. */
  forward(request: ComputerForwardRequest): Promise<ComputerForward> {
    return this.client.http.request("POST", "/computer/forwards", { body: request });
  }
  closeForward(id: string): Promise<{ ok: boolean }> {
    return this.client.http.request("DELETE", `/computer/forwards/${encodeURIComponent(id)}`);
  }
}

/** The mobile scanner accepts raw JSON. Avoid base64 overhead in QR codes without
 * dropping routes, shortening the host pin or changing the pairing protocol.
 * This includes a short-lived secret; never log it to shared service logs. */
export function computerPairingQRData(invitation: ComputerInvitation): string {
  return JSON.stringify(invitation);
}

/** A URI carries an invitation, not permanent credentials. Never log it to shared service logs. */
export function computerPairingURI(invitation: ComputerInvitation): string {
  // UTF-8/base64url works in browsers too; no Node-only dependency in the protocol layer.
  const bytes = new TextEncoder().encode(computerPairingQRData(invitation));
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  const encoded = btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return `mindwire://pair?v=1#${encoded}`;
}
