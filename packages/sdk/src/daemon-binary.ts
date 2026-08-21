// Version-pinned daemon acquisition. The npm package stays TypeScript-only: when a server-side target
// needs a daemon, it downloads the binary matching this SDK's version from the GitHub Release, verifies
// the published SHA-256, and caches it outside node_modules for later offline use.
import { MindwireError } from "./errors.js";
import { SDK_VERSION } from "./version.js";

export type DaemonPlatform = "darwin" | "linux" | "win32";
export type DaemonArch = "x64" | "arm64";

export interface EnsureDaemonBinaryOptions {
  version?: string;
  platform?: DaemonPlatform;
  arch?: DaemonArch;
  cacheDir?: string;
  /** Override for tests or a private GitHub Releases mirror. */
  releaseBaseUrl?: string;
  fetch?: typeof fetch;
}

function supported(platform: string, arch: string): asserts platform is DaemonPlatform & string {
  if (!(["darwin", "linux", "win32"] as string[]).includes(platform) || !(["x64", "arm64"] as string[]).includes(arch)) {
    throw new MindwireError(`mindwire: no daemon release for ${platform}-${arch}`);
  }
}

function assetName(version: string, platform: DaemonPlatform, arch: DaemonArch): string {
  const releasePlatform = platform === "win32" ? "windows" : platform;
  const releaseArch = arch === "x64" ? "amd64" : arch;
  return `mindwired-v${version}-${releasePlatform}-${releaseArch}${platform === "win32" ? ".exe" : ""}`;
}

function checksumFor(text: string, asset: string): string | undefined {
  const line = text.split("\n").find((l) => l.trim().endsWith(` ${asset}`) || l.trim().endsWith(`  ${asset}`));
  const hash = line?.trim().split(/\s+/)[0];
  return hash && /^[a-f0-9]{64}$/i.test(hash) ? hash.toLowerCase() : undefined;
}

/** Ensure the SDK-matched daemon binary is present locally and return its executable path. */
export async function ensureDaemonBinary(opts: EnsureDaemonBinaryOptions = {}): Promise<string> {
  const proc = globalThis as typeof globalThis & { process?: { platform?: string; arch?: string; env?: Record<string, string | undefined> } };
  const platform = opts.platform ?? proc.process?.platform;
  const arch = opts.arch ?? proc.process?.arch;
  supported(platform ?? "unknown", arch ?? "unknown");
  const daemonPlatform = platform as DaemonPlatform;
  const daemonArch = arch as DaemonArch;
  const version = opts.version ?? SDK_VERSION;
  if (!/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(version)) {
    throw new MindwireError(`mindwire: cannot download daemon for non-release SDK version ${version}`);
  }

  const fs = await import("node:fs/promises");
  const path = await import("node:path");
  const os = await import("node:os");
  const crypto = await import("node:crypto");
  const home = os.homedir();
  const defaultCache = daemonPlatform === "darwin"
    ? path.join(home, "Library", "Caches", "mindwire")
    : daemonPlatform === "win32"
      ? path.join(proc.process?.env?.LOCALAPPDATA ?? home, "mindwire", "Cache")
      : path.join(proc.process?.env?.XDG_CACHE_HOME ?? path.join(home, ".cache"), "mindwire");
  const dir = path.join(opts.cacheDir ?? defaultCache, version, `${daemonPlatform}-${daemonArch}`);
  const asset = assetName(version, daemonPlatform, daemonArch);
  const bin = path.join(dir, asset);
  const shaFile = `${bin}.sha256`;

  try {
    const [bytes, expected] = await Promise.all([fs.readFile(bin), fs.readFile(shaFile, "utf8")]);
    if (crypto.createHash("sha256").update(bytes).digest("hex") === expected.trim()) return bin;
  } catch {
    // Missing or incomplete cache entries are replaced atomically below.
  }

  const base = (opts.releaseBaseUrl ?? proc.process?.env?.MINDWIRE_RELEASE_BASE_URL ?? "https://github.com/oblien/mindwire/releases/download").replace(/\/$/, "");
  const release = `${base}/v${version}`;
  const request = opts.fetch ?? globalThis.fetch;
  if (!request) throw new MindwireError("mindwire: fetch is unavailable; set daemonBin or MINDWIRE_DAEMON");
  const [checksums, binary] = await Promise.all([request(`${release}/checksums.txt`), request(`${release}/${asset}`)]);
  if (!checksums.ok || !binary.ok) {
    // A just-published SDK can briefly precede its matching Release asset. For the public release
    // endpoint only, recover to GitHub's latest stable release; mirrors must be explicit and exact.
    if (!opts.releaseBaseUrl && !proc.process?.env?.MINDWIRE_RELEASE_BASE_URL) {
      const latest = await request("https://api.github.com/repos/oblien/mindwire/releases/latest");
      if (latest.ok) {
        const tag = (await latest.json() as { tag_name?: unknown }).tag_name;
        const fallback = typeof tag === "string" ? tag.replace(/^v/, "") : "";
        if (/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(fallback) && fallback !== version) {
          return ensureDaemonBinary({ ...opts, version: fallback });
        }
      }
    }
    throw new MindwireError(`mindwire: failed to download daemon v${version} for ${daemonPlatform}-${daemonArch}`);
  }
  const expected = checksumFor(await checksums.text(), asset);
  if (!expected) throw new MindwireError(`mindwire: release v${version} has no checksum for ${asset}`);
  const bytes = new Uint8Array(await binary.arrayBuffer());
  const actual = crypto.createHash("sha256").update(bytes).digest("hex");
  if (actual !== expected) throw new MindwireError(`mindwire: checksum verification failed for ${asset}`);

  await fs.mkdir(dir, { recursive: true });
  const temp = `${bin}.${process.pid}.tmp`;
  await fs.writeFile(temp, bytes, { mode: 0o755 });
  await fs.writeFile(`${shaFile}.${process.pid}.tmp`, `${expected}\n`);
  await fs.rename(temp, bin);
  await fs.rename(`${shaFile}.${process.pid}.tmp`, shaFile);
  if (daemonPlatform !== "win32") await fs.chmod(bin, 0o755);
  return bin;
}
