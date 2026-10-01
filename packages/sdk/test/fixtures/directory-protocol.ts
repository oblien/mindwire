import { createCipheriv, generateKeyPairSync, randomBytes, sign } from "node:crypto";
import type { ComputerDirectoryEnvelope } from "../../src/computer.js";
import { ComputerDirectoryFixture, directoryKeyId } from "./computer-directory.js";

export function keys() {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  const raw = publicKey.export({ type: "spki", format: "der" }).subarray(-32);
  return { privateKey, id: directoryKeyId(raw), publicKey: raw.toString("base64") };
}
export type Keys = ReturnType<typeof keys>;

export function envelope(key: Keys, domain: string, value: unknown): ComputerDirectoryEnvelope {
  const payload = Buffer.from(JSON.stringify(value)).toString("base64");
  return {
    publicKey: key.publicKey,
    payload,
    signature: sign(null, Buffer.from(`${domain}\n${payload}`), key.privateKey).toString("base64"),
  };
}

export async function request(
  fixture: ComputerDirectoryFixture,
  method: string,
  path: string,
  body?: unknown,
  headers: { [key: string]: string } = {},
) {
  const response = await fetch(fixture.url + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      ...headers,
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  return { status: response.status, data: (await response.json()) as any, headers: response.headers };
}

export function header(fixture: ComputerDirectoryFixture, key: Keys) {
  return {
    version: 1,
    audience: fixture.url,
    directoryId: key.id,
    computerId: "test-computer",
    issuedAt: fixture.now(),
    expiresAt: fixture.now() + 86_400,
  };
}
export async function enroll(
  fixture: ComputerDirectoryFixture,
  host: Keys,
  headers: { [key: string]: string } = {},
) {
  const challenge = await request(
    fixture,
    "POST",
    "/registrations/challenge",
    { directoryId: host.id },
    headers,
  );
  if (challenge.status !== 200) throw new Error(`Enrollment challenge failed (${challenge.status}).`);
  const proof = envelope(host, "mindwire-directory-enroll-v1", {
    ...header(fixture, host),
    expiresAt: fixture.now() + 300,
    challenge: challenge.data.challenge,
  });
  return request(fixture, "POST", "/registrations", proof, headers);
}
export function revoke(fixture: ComputerDirectoryFixture, host: Keys) {
  return request(
    fixture,
    "DELETE",
    "/registrations/" + host.id,
    envelope(host, "mindwire-directory-revoke-v1", {
      ...header(fixture, host),
      expiresAt: fixture.now() + 300,
    }),
  );
}
export function publication(
  fixture: ComputerDirectoryFixture,
  host: Keys,
  phones: Keys[],
  sequence: number,
  enabled = true,
) {
  const h = header(fixture, host);
  const readers = phones.map((phone) => {
    const nonce = randomBytes(12),
      cipher = createCipheriv("aes-256-gcm", randomBytes(32), nonce);
    cipher.setAAD(
      Buffer.from(`mindwire-directory-address-v1\n${fixture.url}\n${host.id}\n${phone.id}\n${sequence}`),
    );
    const ciphertext = Buffer.concat([
      nonce,
      cipher.update(
        JSON.stringify({ routes: [{ kind: "websocket", url: "wss://private-route.example/ssh" }] }),
      ),
      cipher.final(),
      cipher.getAuthTag(),
    ]).toString("base64");
    return {
      deviceId: phone.id,
      publicKey: phone.publicKey,
      record: envelope(host, "mindwire-directory-record-v1", {
        ...h,
        registryId: "registry",
        sequence,
        deviceId: phone.id,
        ciphertext,
      }),
    };
  });
  return envelope(host, "mindwire-directory-publish-v1", { ...h, sequence, enabled, readers });
}
export async function lookup(fixture: ComputerDirectoryFixture, host: Keys, phone: Keys) {
  const timestamp = fixture.now(),
    nonce = randomBytes(32).toString("base64url");
  const signature = sign(
    null,
    Buffer.from(
      `mindwire-directory-lookup-v1\n${fixture.url}\n${host.id}\n${phone.id}\n${timestamp}\n${nonce}`,
    ),
    phone.privateKey,
  ).toString("base64");
  return request(fixture, "POST", `/records/${host.id}/lookup`, {
    deviceId: phone.id,
    timestamp,
    nonce,
    signature,
  });
}
