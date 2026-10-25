import { readFileSync } from "node:fs";
import { cert, getApps, initializeApp } from "firebase-admin/app";
import { getMessaging, type Messaging } from "firebase-admin/messaging";
import { PushError, type DeliveryResult, type PushMessage, type PushTransport } from "./types";

const DEAD_TOKENS = new Set(["messaging/registration-token-not-registered", "messaging/invalid-registration-token"]);

function errorCode(error: unknown): string {
  return error && typeof error === "object" && "code" in error && typeof error.code === "string"
    ? error.code : "messaging/unknown-error";
}

/** Explicit deployment secret only; no credential embedded in source or image. */
export class FirebasePush implements PushTransport {
  private messaging?: Messaging;

  private client(): Messaging {
    if (this.messaging) return this.messaging;
    const json = process.env.FCM_SERVICE_ACCOUNT_JSON;
    const file = process.env.FCM_SERVICE_ACCOUNT_PATH;
    if (!json && !file) throw new PushError(503, "push_not_configured");
    const name = "mindwire-push";
    try {
      const app = getApps().find(app => app.name === name)
        ?? initializeApp({ credential: cert(JSON.parse(json || readFileSync(file!, "utf8"))) }, name);
      return this.messaging = getMessaging(app);
    } catch {
      // JSON parse/SDK errors can include private credential material.
      throw new PushError(503, "push_credentials_invalid");
    }
  }

  async validate(token: string): Promise<{ valid: boolean; reason?: string }> {
    try {
      await this.client().send({ token, data: { __validate: "1" } }, true);
      return { valid: true };
    } catch (error) {
      if (error instanceof PushError) return { valid: true, reason: error.code };
      const code = errorCode(error);
      // Registration can survive a transient provider outage; delivery never
      // reports success when credentials or the transport are unavailable.
      return { valid: !DEAD_TOKENS.has(code), reason: code };
    }
  }

  async send(tokens: string[], message: PushMessage): Promise<DeliveryResult> {
    const result: DeliveryResult = { delivered: 0, failed: 0, invalidTokens: [], errors: [] };
    const client = this.client();
    for (let offset = 0; offset < tokens.length; offset += 500) {
      const batch = tokens.slice(offset, offset + 500);
      try {
        const response = await client.sendEachForMulticast({
          tokens: batch,
          notification: { title: message.title, body: message.body },
          data: message.data,
          // Keep the existing Apple notification extension/avatar behavior.
          apns: { payload: { aps: { mutableContent: true } } },
        });
        response.responses.forEach((response, index) => {
          if (response.success) result.delivered++;
          else {
            result.failed++;
            const code = errorCode(response.error);
            if (DEAD_TOKENS.has(code)) result.invalidTokens.push(batch[index]);
            this.record(result, code);
          }
        });
      } catch (error) {
        result.failed += batch.length;
        this.record(result, errorCode(error), batch.length);
      }
    }
    return result;
  }

  private record(result: DeliveryResult, code: string, count = 1): void {
    // Firebase messages may contain registration tokens or upstream bodies.
    // Return only a known structured code, never provider message text.
    const safeCode = /^messaging\/[a-z-]+$/.test(code) ? code : "messaging/unknown-error";
    const existing = result.errors.find(error => error.code === safeCode);
    if (existing) existing.count += count;
    else result.errors.push({ code: safeCode, message: safeCode, count });
  }
}
