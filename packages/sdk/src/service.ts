import type { Mindwire } from "./client.js";

export interface ServiceUpdateState {
  idle: boolean;
  updating: boolean;
  activeOperations: number;
}

export interface ServiceUpdateLease {
  id: string;
  /** Terminate only this leased process, not every mindwired process on the host. */
  pid: number;
  expiresAt: string;
}

/** Installer coordination; requires health.serviceUpdateVersion >= 1. */
export class ServiceApi {
  constructor(private readonly client: Mindwire) {}

  updateStatus(): Promise<ServiceUpdateState> {
    return this.client.http.request("GET", "/service/update");
  }

  /** Download first. Acquire immediately before replacement; busy work returns 409.
   * With serviceUpdateVersion >= 2, force authorizes interrupting active work.
   * It never overrides another installer's lease.
   * Admission stays closed until release, process exit, or the one-minute expiry. */
  acquireUpdate(options?: { force?: boolean }): Promise<ServiceUpdateLease> {
    return this.client.http.request("POST", "/service/update", options ? { body: options } : undefined);
  }

  releaseUpdate(id: string): Promise<{ ok: boolean }> {
    return this.client.http.request("DELETE", `/service/update/${encodeURIComponent(id)}`);
  }
}
