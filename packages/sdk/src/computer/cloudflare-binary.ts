import { execFile } from "node:child_process";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import * as path from "node:path";
import { promisify } from "node:util";
import { binaryCacheDirectory, cachedExecutable, cacheExecutable, executableChecksum, verifyReleaseBytes } from "../binary-cache.js";

// The bridge was tested against this release. The helper never updates itself or
// changes a system installation; a newer Mindwire release can advance this pin.
export const CLOUDFLARED_VERSION = "2026.9.3";
const execute = promisify(execFile);

export async function ensureCloudflareBinary(options: {
  cacheDir?: string; platform?: string; arch?: string; fetch?: typeof fetch;
  onProgress?: (message: string) => void | Promise<void>;
} = {}): Promise<string> {
  const platform = options.platform ?? process.platform, arch = options.arch ?? process.arch;
  const cpu = arch === "x64" ? "amd64" : arch === "arm64" ? "arm64" : "";
  if (!cpu || !["darwin", "linux", "win32"].includes(platform) || platform === "win32" && cpu !== "amd64") {
    throw new Error("Install cloudflared for this computer, or select another connection provider.");
  }
  const asset = `cloudflared-${platform === "win32" ? "windows" : platform}-${cpu}${platform === "darwin" ? ".tgz" : platform === "win32" ? ".exe" : ""}`;
  const bin = path.join(options.cacheDir ?? await binaryCacheDirectory(), "cloudflared", CLOUDFLARED_VERSION,
    `${platform}-${arch}`, platform === "win32" ? "cloudflared.exe" : "cloudflared");
  const cached = await cachedExecutable(bin);
  if (cached) return cached;
  await options.onProgress?.("Downloading the secure connection helper…");
  const request = options.fetch ?? fetch;
  const release = await request(`https://api.github.com/repos/cloudflare/cloudflared/releases/tags/${CLOUDFLARED_VERSION}`,
    { headers: { Accept: "application/vnd.github+json" }, signal: AbortSignal.timeout(20_000) });
  if (!release.ok) throw new Error("Couldn't download the connection helper. Retry, install cloudflared yourself, or choose another provider.");
  const metadata = await release.json() as { assets?: { name: string; digest?: string; browser_download_url: string }[] };
  const item = metadata.assets?.find(value => value.name === asset);
  const checksum = item?.digest?.match(/^sha256:([a-f0-9]{64})$/i)?.[1];
  const expectedURL = `https://github.com/cloudflare/cloudflared/releases/download/${CLOUDFLARED_VERSION}/${asset}`;
  if (!checksum || item?.browser_download_url !== expectedURL) throw new Error("The connection helper has no verified release asset for this computer.");
  const downloaded = await request(expectedURL, { signal: AbortSignal.timeout(120_000) });
  if (!downloaded.ok) throw new Error("Couldn't download the connection helper. Check your internet connection and retry.");
  const archive = new Uint8Array(await downloaded.arrayBuffer());
  await verifyReleaseBytes(archive, checksum, asset);
  let executable = archive;
  if (platform === "darwin") {
    const temporary = await mkdtemp(path.join(tmpdir(), "mindwire-cloudflared-"));
    try {
      const archivePath = path.join(temporary, asset);
      await writeFile(archivePath, archive, { mode: 0o600 });
      // Read only this exact archive member to stdout. No archive paths are
      // extracted into the filesystem and no downloaded script is executed.
      const result = await execute("/usr/bin/tar", ["-xzOf", archivePath, "cloudflared"],
        { encoding: "buffer", maxBuffer: 128 * 1024 * 1024, timeout: 30_000 });
      executable = new Uint8Array(result.stdout);
    } finally { await rm(temporary, { recursive: true, force: true }); }
  }
  return cacheExecutable(bin, executable, await executableChecksum(executable));
}

export async function cloudflareCommand(options: Parameters<typeof ensureCloudflareBinary>[0]): Promise<string> {
  try {
    await execute("cloudflared", ["--version"], { timeout: 3000, windowsHide: true });
    return "cloudflared";
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw new Error("The installed cloudflared could not start. Repair it or choose another connection provider.");
    return ensureCloudflareBinary(options);
  }
}
