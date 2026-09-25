// Shared verified executable cache for the daemon and optional connection helpers.
// Node modules stay lazy so importing the SDK in a browser remains supported.
import { MindwireError } from "./errors.js";

export async function binaryCacheDirectory(platform = process.platform): Promise<string> {
  const path = await import("node:path");
  const { homedir } = await import("node:os");
  const homeDirectory = homedir();
  return platform === "darwin" ? path.join(homeDirectory, "Library", "Caches", "mindwire")
    : platform === "win32" ? path.join(process.env.LOCALAPPDATA ?? homeDirectory, "mindwire", "Cache")
    : path.join(process.env.XDG_CACHE_HOME ?? path.join(homeDirectory, ".cache"), "mindwire");
}

export async function executableChecksum(bytes: Uint8Array): Promise<string> {
  const { createHash } = await import("node:crypto");
  return createHash("sha256").update(bytes).digest("hex");
}

export async function verifyReleaseBytes(bytes: Uint8Array, expected: string, name: string): Promise<void> {
  if (!/^[a-f0-9]{64}$/i.test(expected) || await executableChecksum(bytes) !== expected.toLowerCase()) {
    throw new MindwireError(`mindwire: checksum verification failed for ${name}`);
  }
}

export async function cachedExecutable(bin: string): Promise<string | undefined> {
  const fs = await import("node:fs/promises");
  try {
    const [bytes, checksum] = await Promise.all([fs.readFile(bin), fs.readFile(`${bin}.sha256`, "utf8")]);
    await verifyReleaseBytes(bytes, checksum.trim(), bin);
    return bin;
  } catch { return undefined; }
}

export async function cacheExecutable(bin: string, bytes: Uint8Array, expected: string): Promise<string> {
  await verifyReleaseBytes(bytes, expected, bin);
  const fs = await import("node:fs/promises");
  const { dirname } = await import("node:path");
  const { randomUUID } = await import("node:crypto");
  await fs.mkdir(dirname(bin), { recursive: true, mode: 0o700 });
  // Unique temporary names also cover concurrent acquisitions in the same process.
  const temp = `${bin}.${randomUUID()}.tmp`;
  try {
    await fs.writeFile(temp, bytes, { mode: 0o755, flag: "wx" });
    await fs.writeFile(`${temp}.sha256`, `${expected.toLowerCase()}\n`, { mode: 0o600, flag: "wx" });
    await fs.rename(temp, bin);
    await fs.rename(`${temp}.sha256`, `${bin}.sha256`);
    return bin;
  } finally {
    await fs.rm(temp, { force: true });
    await fs.rm(`${temp}.sha256`, { force: true });
  }
}
