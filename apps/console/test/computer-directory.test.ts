import { expect, test } from "bun:test";
import { ComputerDirectoryFixture } from "../../../packages/sdk/test/fixtures/computer-directory";
import {
  enroll,
  envelope,
  header,
  keys,
  lookup,
  publication,
  request,
  revoke,
} from "../../../packages/sdk/test/fixtures/directory-protocol";

import { sourceNetwork } from "../server/computer-directory/admission";
import { envelopeDigest } from "../server/computer-directory/protocol";

test("computers enroll using keys without accounts, enumeration or workspace sessions", async () => {
  const f = await new ComputerDirectoryFixture().start();
  try {
    const host = keys();
    expect(await f.control("accountCount")).toBe(0);
    expect((await enroll(f, host)).status).toBe(201);
    expect((await enroll(f, host)).status).toBe(200);
    expect((await request(f, "GET", "/registrations")).status).toBe(404);
    expect((await request(f, "GET", "/unknown")).status).toBe(404);
    await f.restart();
    expect((await enroll(f, host)).status).toBe(200);
    expect(await f.control("accountCount")).toBe(0);
    expect(f.requests.every((r) => !r.authorized && !r.cookie && !r.path.startsWith("/api/account"))).toBe(
      true,
    );
    const sibling = await fetch(f.url + "-other");
    expect(sibling.status).toBe(401);
    await sibling.body?.cancel();
  } finally {
    await f.close();
  }
}, 20_000);

test("enrollment challenges bind the computer, origin, network and expiry and survive restart", async () => {
  const f = await new ComputerDirectoryFixture({ trustedProxies: "127.0.0.1/32,::1/128" }).start();
  try {
    const host = keys(),
      other = keys();
    const network = { "X-Forwarded-For": "203.0.113.17" };
    const c = await request(f, "POST", "/registrations/challenge", { directoryId: host.id }, network);
    expect(c.status).toBe(200);
    const signed = (key = host, changes = {}) =>
      envelope(key, "mindwire-directory-enroll-v1", {
        ...header(f, key),
        expiresAt: f.now() + 300,
        challenge: c.data.challenge,
        ...changes,
      });
    expect((await request(f, "POST", "/registrations", signed(other), network)).data.error.code).toBe(
      "invalid_challenge",
    );
    expect(
      (await request(f, "POST", "/registrations", signed(), { "X-Forwarded-For": "203.0.113.18" })).data.error
        .code,
    ).toBe("invalid_challenge");
    expect(
      (
        await request(
          f,
          "POST",
          "/registrations",
          signed(host, { audience: "https://wrong.example/api/computer-directory/v1" }),
          network,
        )
      ).status,
    ).toBe(400);
    expect(
      (
        await request(
          f,
          "POST",
          "/registrations",
          signed(host, { challenge: c.data.challenge + "x" }),
          network,
        )
      ).status,
    ).toBe(403);
    const expired = await f.control<{ challenge: string }>("challenge", {
      directoryId: host.id,
      address: "203.0.113.17",
      timestamp: f.now() - 301,
    });
    expect(
      (await request(f, "POST", "/registrations", signed(host, { challenge: expired.challenge }), network))
        .status,
    ).toBe(403);
    const proof = signed();
    await f.restart();
    expect((await request(f, "POST", "/registrations", proof, network)).status).toBe(201);
    expect((await request(f, "POST", "/registrations", proof, network)).status).toBe(200);
    expect(
      (
        await request(
          f,
          "POST",
          "/registrations",
          signed(host, { computerId: "different-computer" }),
          network,
        )
      ).status,
    ).toBe(409);
  } finally {
    await f.close();
  }
}, 20_000);

test("only the host key can change or revoke a record; website login stays independent", async () => {
  const f = await new ComputerDirectoryFixture().start();
  try {
    const host = keys(),
      phone = keys(),
      stranger = keys();
    await enroll(f, host);
    const good = publication(f, host, [phone], 1);
    await request(f, "PUT", "/records/" + host.id, good);
    const account = await f.newAccount();
    const cookie = { Cookie: account.cookie, Origin: new URL(f.url).origin };
    expect(await f.control("hasFleet", account.id)).toBe(false);
    expect((await request(f, "GET", "/registrations", undefined, cookie)).status).toBe(404);
    expect((await request(f, "DELETE", "/registrations/" + host.id, {}, cookie)).status).toBe(401);
    const forged = envelope(stranger, "mindwire-directory-enroll-v1", {
      ...header(f, host),
      expiresAt: f.now() + 300,
      challenge: "fake",
    });
    expect((await request(f, "POST", "/registrations", forged, cookie)).status).toBe(400);
    expect(
      (await request(f, "PUT", "/records/" + host.id, publication(f, stranger, [stranger], 2), cookie))
        .status,
    ).toBe(400);
    const wrongRevoke = envelope(stranger, "mindwire-directory-revoke-v1", {
      ...header(f, stranger),
      expiresAt: f.now() + 300,
    });
    expect((await request(f, "DELETE", "/registrations/" + host.id, wrongRevoke, cookie)).status).toBe(403);
    expect((await lookup(f, host, { ...phone, privateKey: stranger.privateKey })).status).toBe(401);
    expect((await lookup(f, host, phone)).status).toBe(200);
    expect(await f.control("hasFleet", account.id)).toBe(false);
    const session = await f.accountRequest("/get-session", "GET", undefined, account);
    expect(((await session.json()) as any)?.user?.id === account.id).toBe(true);
    const protectedURL = new URL(f.url).origin + "/api/session";
    const anonymous = await fetch(protectedURL);
    expect(anonymous.status).toBe(401);
    await anonymous.body?.cancel();
    const loggedIn = await fetch(protectedURL, { headers: cookie });
    expect(loggedIn.status).toBe(200);
    await loggedIn.body?.cancel();
    await f.control("deleteUser", account.id);
    await f.control("prune");
    expect((await lookup(f, host, phone)).status).toBe(200);
    expect((await request(f, "PUT", "/records/" + host.id, publication(f, host, [phone], 2))).status).toBe(
      200,
    );
    expect((await revoke(f, host)).status).toBe(200);
  } finally {
    await f.close();
  }
}, 20_000);

test("signed address updates are atomic across lost receipts, concurrent writers and restart", async () => {
  const f = await new ComputerDirectoryFixture().start();
  try {
    const host = keys(),
      phone = keys(),
      removed = keys();
    expect((await enroll(f, host)).status).toBe(201);
    const one = publication(f, host, [phone, removed], 1);
    f.dropPublishReceipts = 1;
    expect((await request(f, "PUT", "/records/" + host.id, one)).status).toBe(503);
    expect((await request(f, "PUT", "/records/" + host.id, one)).status).toBe(200);
    expect(f.publishDigests[0]).toBe(f.publishDigests[1]);
    expect((await lookup(f, host, phone)).status).toBe(200);
    expect((await lookup(f, host, keys())).status).toBe(403);
    expect((await request(f, "PUT", "/records/" + host.id, publication(f, host, [phone], 1))).status).toBe(
      409,
    );
    const two = publication(f, host, [phone, removed], 2),
      three = publication(f, host, [phone], 3);
    await Promise.all([
      request(f, "PUT", "/records/" + host.id, two),
      request(f, "PUT", "/records/" + host.id, three),
    ]);
    await f.restart();
    const result = await lookup(f, host, phone);
    expect(result.status).toBe(200);
    expect(JSON.parse(Buffer.from(result.data.payload, "base64").toString()).sequence).toBe(3);
    expect(JSON.stringify(result.data)).not.toContain("private-route.example");
    expect((await lookup(f, host, removed)).status).toBe(403);
    expect((await request(f, "PUT", "/records/" + host.id, one)).status).toBe(409);
    expect((await request(f, "PUT", "/records/" + host.id, three)).status).toBe(200);
  } finally {
    await f.close();
  }
}, 15_000);

test("expiry clears ciphertext while disable and revocation preserve rollback tombstones", async () => {
  const f = await new ComputerDirectoryFixture().start();
  try {
    const host = keys(),
      phone = keys();
    await enroll(f, host);
    const one = publication(f, host, [phone], 1);
    await request(f, "PUT", "/records/" + host.id, one);
    await f.control("expirePublication", host.id);
    expect((await lookup(f, host, phone)).data.error.code).toBe("record_expired");
    await f.control("prune");
    expect(await f.control("row", host.id)).toEqual({ sequence: 1, hasPublication: false, revoked: false });
    expect((await request(f, "PUT", "/records/" + host.id, one)).status).toBe(200);
    expect((await lookup(f, host, phone)).status).toBe(404);
    expect((await request(f, "PUT", "/records/" + host.id, publication(f, host, [phone], 2))).status).toBe(
      200,
    );
    expect((await lookup(f, host, phone)).status).toBe(200);
    expect((await request(f, "PUT", "/records/" + host.id, publication(f, host, [], 3, false))).status).toBe(
      200,
    );
    expect((await lookup(f, host, phone)).status).toBe(403);
    expect((await request(f, "PUT", "/records/" + host.id, publication(f, host, [phone], 4))).status).toBe(
      200,
    );
    expect((await revoke(f, host)).status).toBe(200);
    await f.restart();
    expect((await lookup(f, host, phone)).data.error.code).toBe("revoked");
    expect((await request(f, "PUT", "/records/" + host.id, publication(f, host, [phone], 5))).status).toBe(
      403,
    );
    expect((await enroll(f, host)).status).toBe(409);
    expect(await f.control("row", host.id)).toEqual({ sequence: 4, hasPublication: false, revoked: true });
  } finally {
    await f.close();
  }
}, 15_000);

test("parallel enrollment has a durable network quota; retries and existing records remain usable", async () => {
  const f = await new ComputerDirectoryFixture({ enrollmentsPerDay: 2 }).start();
  try {
    const hosts = Array.from({ length: 5 }, () => keys());
    const results = await Promise.all(hosts.map((host) => enroll(f, host)));
    expect(results.filter((r) => r.status === 201).length).toBe(2);
    expect(results.filter((r) => r.status === 429 && r.data.error.code === "enrollment_limit").length).toBe(
      3,
    );
    const host = hosts[results.findIndex((r) => r.status === 201)]!,
      phone = keys();
    expect((await enroll(f, host)).status).toBe(200);
    const budget = await f.control<{ source: string; count: number }[]>("enrollmentBudget");
    expect(budget).toHaveLength(1);
    expect(budget[0]!.count).toBe(2);
    expect(budget[0]!.source).not.toContain("127.0.0.1");
    await f.restart();
    expect((await enroll(f, keys(), { "X-Forwarded-For": "203.0.113.1" })).status).toBe(429);
    expect((await enroll(f, host)).status).toBe(200);
    expect((await request(f, "PUT", "/records/" + host.id, publication(f, host, [phone], 1))).status).toBe(
      200,
    );
    expect((await lookup(f, host, phone)).status).toBe(200);
    await f.control("prune", f.now() + 86_400);
    expect(await f.control("enrollmentBudget")).toEqual([]);
  } finally {
    await f.close();
  }
}, 20_000);

test("global capacity is atomic, survives restart and cannot be bypassed by revocation", async () => {
  const f = await new ComputerDirectoryFixture({ maxRegistrations: 2 }).start();
  try {
    const hosts = Array.from({ length: 5 }, () => keys());
    const results = await Promise.all(hosts.map((host) => enroll(f, host)));
    expect(results.filter((r) => r.status === 201).length).toBe(2);
    expect(
      results.filter((r) => r.status === 503 && r.data.error.code === "registration_capacity").length,
    ).toBe(3);
    const host = hosts[results.findIndex((r) => r.status === 201)]!;
    await f.restart();
    expect((await enroll(f, host)).status).toBe(200);
    expect((await enroll(f, keys())).status).toBe(503);
    await revoke(f, host);
    expect((await enroll(f, keys())).status).toBe(503);
    expect((await f.control<{ count: number }[]>("enrollmentBudget"))[0]!.count).toBe(2);
  } finally {
    await f.close();
  }
}, 20_000);

test("trusted proxy admission groups IPv6 privacy addresses by network", async () => {
  const f = await new ComputerDirectoryFixture({
    enrollmentsPerDay: 1,
    trustedProxies: "127.0.0.1/32,::1/128",
  }).start();
  try {
    expect((await enroll(f, keys(), { "X-Forwarded-For": "2001:db8:1:2::1" })).status).toBe(201);
    expect((await enroll(f, keys(), { "X-Forwarded-For": "2001:db8:1:2::9999" })).status).toBe(429);
    expect((await enroll(f, keys(), { "X-Forwarded-For": "2001:db8:1:3::1" })).status).toBe(201);
    // Ignore attacker-supplied leftmost hops beyond the first untrusted proxy.
    expect((await enroll(f, keys(), { "X-Forwarded-For": "203.0.113.9, 2001:db8:1:2::5" })).status).toBe(429);
  } finally {
    await f.close();
  }
}, 15_000);

test("network normalization treats mapped IPv4 and equivalent IPv6 addresses consistently", () => {
  expect(sourceNetwork("127.0.0.1")).toBe("127.0.0.1");
  expect(sourceNetwork("::ffff:127.0.0.1")).toBe("127.0.0.1");
  expect(sourceNetwork("0:0:0:0:0:ffff:7f00:0001")).toBe("127.0.0.1");
  expect(sourceNetwork("2001:DB8:1:2:0:0:0:1")).toBe("2001:0db8:0001:0002::/64");
  expect(sourceNetwork("2001:db8:1:2::ffff")).toBe(sourceNetwork("2001:db8:1:2::1"));
  expect(sourceNetwork("::1")).toBe("0000:0000:0000:0000::/64");
  expect(sourceNetwork("fe80::1%en0")).toBe(sourceNetwork("fe80::1234%en0"));
  expect(sourceNetwork("invalid")).toBe("unknown");
});

test("the forward migration preserves old keys, ciphertext, revisions and revoked identities", async () => {
  const host = keys(),
    phone = keys(),
    revoked = keys();
  let oldPublication: ReturnType<typeof publication>;
  const f = await new ComputerDirectoryFixture({
    seedLegacy: (fixture) => {
      oldPublication = publication(fixture, host, [phone], 7);
      return [
        {
          directoryId: host.id,
          computerId: "test-computer",
          publicKey: host.publicKey,
          ownerId: "old-website-account",
          revoked: 0,
          sequence: 7,
          enabled: 1,
          expiresAt: fixture.now() + 86_400,
          createdAt: fixture.now() - 86_400,
          digest: envelopeDigest(oldPublication),
          publication: Buffer.from(oldPublication.payload, "base64").toString(),
        },
        {
          directoryId: revoked.id,
          computerId: "test-computer",
          publicKey: revoked.publicKey,
          ownerId: "deleted-account",
          revoked: 1,
          sequence: 9,
          enabled: 0,
          expiresAt: 0,
          createdAt: 1,
          digest: "revoked-digest",
          publication: null,
        },
      ];
    },
  }).start();
  try {
    expect((await enroll(f, host)).status).toBe(200);
    expect((await lookup(f, host, phone)).data).toEqual(
      JSON.parse(Buffer.from(oldPublication!.payload, "base64").toString()).readers[0].record,
    );
    expect((await request(f, "PUT", "/records/" + host.id, oldPublication!)).status).toBe(200);
    expect((await request(f, "PUT", "/records/" + host.id, publication(f, host, [phone], 6))).status).toBe(
      409,
    );
    expect((await enroll(f, revoked)).status).toBe(409);
    expect(await f.control("row", revoked.id)).toEqual({ sequence: 9, hasPublication: false, revoked: true });
    expect(await f.control("accountCount")).toBe(0);
    expect(await f.control("enrollmentBudget")).toEqual([]);
    await f.restart();
    expect((await request(f, "PUT", "/records/" + host.id, publication(f, host, [phone], 8))).status).toBe(
      200,
    );
    expect((await lookup(f, host, phone)).status).toBe(200);
  } finally {
    await f.close();
  }
}, 20_000);

test("untrusted payloads are bounded and cannot substitute signatures, audiences or readers", async () => {
  const f = await new ComputerDirectoryFixture().start();
  try {
    const host = keys(),
      phone = keys();
    await enroll(f, host);
    const good = publication(f, host, [phone], 1);
    expect(
      (await request(f, "PUT", "/records/" + host.id, { ...good, signature: good.signature.slice(4) }))
        .status,
    ).toBe(401);
    const value = JSON.parse(Buffer.from(good.payload, "base64").toString());
    for (const changes of [
      { audience: "https://another.example/api/computer-directory/v1" },
      { sequence: Number.MAX_SAFE_INTEGER + 1 },
      { expiresAt: f.now() - 1 },
      { issuedAt: f.now() + 301 },
      { readers: [...value.readers, ...value.readers] },
    ]) {
      const rejected = envelope(host, "mindwire-directory-publish-v1", { ...value, ...changes });
      expect((await request(f, "PUT", "/records/" + host.id, rejected)).status).toBe(400);
    }
    const wrongReader = envelope(host, "mindwire-directory-publish-v1", {
      ...value,
      readers: [{ ...value.readers[0], publicKey: keys().publicKey }],
    });
    expect((await request(f, "PUT", "/records/" + host.id, wrongReader)).status).toBe(400);
    expect(
      (await request(f, "PUT", "/records/" + host.id, { payload: "a".repeat(1024 * 1024 + 1) })).status,
    ).toBe(413);
    const wrongType = await fetch(f.url + "/registrations/challenge", {
      method: "POST",
      headers: { "Content-Type": "text/plain" },
      body: "{}",
    });
    expect({ status: wrongType.status, body: await wrongType.text() }).toEqual({
      status: 415,
      body: expect.stringContaining('"code":"json_required"'),
    });
    expect((await request(f, "PUT", "/records/" + host.id, good)).status).toBe(200);
    const read = await lookup(f, host, phone);
    expect(read.status).toBe(200);
    expect(read.headers.get("cache-control")).toBe("no-store");
  } finally {
    await f.close();
  }
}, 15_000);
