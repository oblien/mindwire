import { SDK_VERSION, versionAtLeast } from "../version.js";
import type { ComputerConfig } from "./lifecycle.js";

/** The saved version is the last installed release, not an accidental permanent
 * pin. Upgrading the CLI must also upgrade its managed service. Never downgrade a
 * newer service or replace an explicitly supplied development binary. */
export function managedServiceVersion(config: ComputerConfig, cliVersion = SDK_VERSION): string {
  if (config.versionPinned && config.version) return config.version;
  if (!/^\d+\.\d+\.\d+$/.test(cliVersion)) return config.version ?? cliVersion;
  return versionAtLeast(config.version, cliVersion) ? config.version! : cliVersion;
}

export function serviceUpgradeVersion(config: ComputerConfig, installed: string, cliVersion = SDK_VERSION): string | undefined {
  const wanted = managedServiceVersion(config, cliVersion);
  if (config.daemonBin || !/^\d+\.\d+\.\d+$/.test(wanted) || versionAtLeast(installed, wanted)) return;
  return wanted;
}
