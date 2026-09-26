import { spawn, execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import * as fs from "node:fs/promises";
import * as path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import type { Mindwire } from "../client.js";
import { ensureDaemonBinary } from "../daemon-binary.js";
import { SDK_VERSION } from "../version.js";
import type { ComputerInfo, ComputerRoute, ComputerUpdate } from "../computer.js";
import { computerClient, directRoutes, readJSON, writeJSON, CONTROLLER_PROTOCOL, type ComputerConfig, type ControllerState } from "./lifecycle.js";
import { adoptRelay, startRelay, type RelayHandle } from "./relay.js";
import { checkRelay, relayNeedsRestart, relayProviderReachable, RelayCheckError } from "./relay-health.js";
import { adoptProcess, ownChild, currentProcessIdentity, processStateAlive, processIdentity, type OwnedProcess, type ProcessIdentity } from "./process.js";
import { RelayOutput } from "./relay-output.js";

interface SavedRelay { owner: ProcessIdentity; port: number; options: string; route?: ComputerRoute; outputFile?: string }
export function recoveryDelay(failures: number): number { return Math.min(60_000, 1000 * 2 ** Math.min(6, Math.max(0, failures - 1))); }

/** Own process lifecycle separately from any mobile view or pairing command. */
export async function superviseComputer(directory: string): Promise<void> {
  if ((await readJSON<{ stopped: boolean }>(path.join(directory, "computer-stopped.json")))?.stopped) return;
  let config = await readJSON<ComputerConfig>(path.join(directory, "computer-config.json"));
  if (!config) throw new Error("Computer configuration is missing.");
  const controllerPath = path.join(directory, "computer-controller.json");
  const relayPath = path.join(directory, "computer-relay.json");
  const previous = await readJSON<ControllerState>(controllerPath);
  if (await processStateAlive(previous)) throw new Error("The computer already has a service controller.");
  const owner = await currentProcessIdentity();
  const phase = (message: string) => writeJSON(controllerPath, { ...owner, protocol: CONTROLLER_PROTOCOL, cliVersion: SDK_VERSION, ready: false, phase: message, recovering: true });
  await phase("Preparing Mindwire…");
  let child: OwnedProcess | undefined, relay: RelayHandle | undefined, stopping = false;
  let relayTask: Promise<void> | undefined, updateTask: Promise<void> | undefined;
  let relayResult: { handle?: RelayHandle; error?: unknown } | undefined;
  let checkTask: Promise<void> | undefined;
  let checkResult: { relay: RelayHandle; error?: unknown; canRestart?: boolean } | undefined;
  const abort = new AbortController();
  const stop = () => { stopping = true; abort.abort(); };
  process.on("SIGTERM", stop); process.on("SIGINT", stop);

  let token: string;
  try { token = (await fs.readFile(path.join(directory, "daemon.token"), "utf8")).trim(); }
  catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    token = randomBytes(32).toString("hex");
    await fs.writeFile(path.join(directory, "daemon.token"), token, { mode: 0o600, flag: "wx" });
  }
  const daemonEnv = { ...process.env };
  if (process.platform === "win32") {
    try {
      const git = execFileSync("where.exe", ["git"], { encoding: "utf8", timeout: 3000, windowsHide: true }).split(/\r?\n/)[0];
      if (git) {
        const shellDirectory = path.resolve(path.dirname(git), "..", "bin");
        await fs.access(path.join(shellDirectory, "bash.exe"));
        const pathKey = Object.keys(daemonEnv).find(key => key.toLowerCase() === "path") ?? "PATH";
        daemonEnv[pathKey] = `${shellDirectory}${path.delimiter}${daemonEnv[pathKey] ?? ""}`;
      }
    } catch { /* Git/Bash are checked only when their capabilities are used. */ }
  }
  const oldRuntime = await readJSON<ComputerInfo>(path.join(directory, "computer-runtime.json"));
  // An automatically selected port remains stable across controller/release restarts.
  let sshPort = config.sshPort || oldRuntime?.sshPort || 0;
  let websocketPort = config.websocketPort || oldRuntime?.websocketPort || 0;
  const launch = async (binary: string): Promise<Mindwire> => {
    await fs.rm(path.join(directory, "computer-runtime.json"), { force: true });
    const process = spawn(binary, ["--computer"], {
      cwd: config!.directory, stdio: ["ignore", "inherit", "inherit"], windowsHide: true,
      env: { ...daemonEnv, DAEMON_TOKEN: token, ADDR: "127.0.0.1:0", STATE_PATH: path.join(directory, "agent-state.json"),
        WORKSPACE_DB_PATH: path.join(directory, "workspace.db"), AGENT_CWD: config!.directory,
        COMPUTER_SSH_ADDR: `${config!.bind.includes(":") ? `[${config!.bind}]` : config!.bind}:${sshPort}`,
        COMPUTER_WS_ADDR: `127.0.0.1:${websocketPort}`, MINDWIRE_COMPUTER_MANAGED: "1" },
    });
    let failure: Error | undefined;
    process.on("error", error => { failure = error; });
    await new Promise<void>((resolve, reject) => { process.once("spawn", resolve); process.once("error", reject); });
    child = await ownChild(process);
    for (let attempt = 0; attempt < 100; attempt++) {
      if (stopping) throw new Error("Mindwire is stopping.");
      if (failure) throw failure;
      if (!child.alive()) throw new Error("Mindwire exited during startup. Check computer.log and the installed release.");
      try {
        const client = await computerClient(directory);
        const info = await client.computer.info();
        if (info.pid !== child.pid) throw new Error("Another service owns this computer's state.");
        const health = await client.health();
        if ((health as { computerConnectionVersion?: number }).computerConnectionVersion !== 1) throw new Error("Update Mindwire to a release that supports computer connections.");
        sshPort = info.sshPort; websocketPort = info.websocketPort;
        return client;
      } catch (error) { if (attempt === 99) throw error; }
      await delay(100);
    }
    throw new Error("Mindwire startup timed out.");
  };

  let routes: ComputerRoute[] = [], published = "";
  let client: Mindwire | undefined;
  let relayMessage = "Preparing your internet connection…", relayError: string | undefined;
  let relayErrorCode: string | undefined;
  let daemonError: string | undefined, nextDaemonAt = 0, daemonFailures = 0;
  let nextRelayAt = 0, relayFailures = 0, changingDaemon = false;
  let daemonReadyAt = Date.now(), relayReadyAt = Date.now();
  let relayReachable = false, relayWasReachable = false, relayHealthFailures = 0;
  let relayFailedAt = 0;
  let nextCheckAt = 0, checkedAt = 0;
  try {
    await phase("Checking the Mindwire service…");
    let binary = config.daemonBin ?? await ensureDaemonBinary({ version: config.version ?? SDK_VERSION, cacheDir: process.env.MINDWIRE_RELEASE_CACHE_DIR });
    // A killed controller must not restart a healthy daemon or lose its PTYs.
    try { client = await computerClient(directory); } catch { /* Cold start. */ }
    if (client) {
      const info = await client.computer.info();
      const identity = await processIdentity(info.pid);
      child = identity ? await adoptProcess(identity) : undefined;
      if (!child) throw new Error("Couldn't verify the existing Mindwire process. Leave it running and retry.");
      sshPort = info.sshPort; websocketPort = info.websocketPort;
    } else {
      await phase("Starting the Mindwire service…");
      client = await launch(binary);
    }

    const saved = await readJSON<SavedRelay>(relayPath);
    if (saved) {
      const owner = await adoptProcess(saved.owner);
      if (owner) {
        if (saved.outputFile && saved.route && saved.port === websocketPort && saved.options === JSON.stringify(config.relay)) {
          relay = await adoptRelay(owner, saved.route, directory, saved.outputFile);
        } else {
          // An old helper's pipes belonged to the controller we replaced. It
          // can still answer briefly, then die on its next log write. Migrate
          // it once before certifying a QR, keeping the daemon and phone keys.
          if (!saved.outputFile) await phase("Refreshing the internet tunnel for this update…");
          const abandoned = await adoptRelay(owner, saved.route, directory, saved.outputFile);
          await abandoned.close();
          await fs.rm(relayPath, { force: true });
        }
      } else if (saved.outputFile) {
        await (await RelayOutput.adopt(directory, saved.outputFile))?.close();
      }
    }

    let lastController = "";
    while (!stopping) {
      if (!changingDaemon && child && !child.alive()) {
        child = undefined; client = undefined;
        daemonError = "The service stopped. Restarting it…";
        nextDaemonAt = Date.now() + recoveryDelay(++daemonFailures);
      }
      if (child?.alive() && Date.now() - daemonReadyAt > 60_000) daemonFailures = 0;
      if (relayReachable && relay?.owner?.alive() && Date.now() - relayReadyAt > 60_000) relayFailures = 0;
      if (!changingDaemon && !child?.alive() && Date.now() >= nextDaemonAt) {
        daemonError = "The service stopped. Restarting it…";
        await phase(daemonError);
        try {
          client = await launch(binary); published = ""; daemonError = undefined; daemonReadyAt = Date.now();
        } catch (error) {
          if (child) await child.close();
          child = undefined; client = undefined;
          daemonError = error instanceof Error ? error.message : "Service restart failed.";
          nextDaemonAt = Date.now() + recoveryDelay(++daemonFailures);
        }
      }

      if (relay?.owner && !relay.owner.alive()) {
        await relay.close();
        relay = undefined;
        relayReachable = false; relayWasReachable = false;
        relayErrorCode = undefined;
        relayError = "The internet tunnel disconnected. Reconnecting…";
        nextRelayAt = Date.now() + recoveryDelay(++relayFailures);
      }
      if (relayResult) {
        const result = relayResult; relayResult = undefined; relayTask = undefined;
        if (result.handle) {
          relay = result.handle; relayError = undefined; relayErrorCode = undefined; relayReadyAt = Date.now();
          relayReachable = false; relayWasReachable = false; relayHealthFailures = 0; relayFailedAt = 0; nextCheckAt = 0;
          if (relay.owner?.identity) await writeJSON(relayPath, {
            owner: relay.owner.identity, port: websocketPort, options: JSON.stringify(config.relay), route: relay.route, outputFile: relay.outputFile,
          } satisfies SavedRelay);
        } else {
          relayError = result.error instanceof Error ? result.error.message : "The internet tunnel couldn't connect.";
          nextRelayAt = Date.now() + recoveryDelay(++relayFailures);
        }
      }
      if (!relay && !relayTask && Date.now() >= nextRelayAt) {
        relayError = undefined; relayErrorCode = undefined;
        relayMessage = "Reconnecting your internet tunnel…";
        relayTask = startRelay(config.relay, websocketPort, {
          cacheDir: path.join(directory, "tools"), outputDirectory: directory, signal: abort.signal,
          onProgress: message => { relayMessage = message; },
          onSpawn: async (owner, outputFile) => {
            if (owner.identity) await writeJSON(relayPath, {
              owner: owner.identity, port: websocketPort, options: JSON.stringify(config!.relay), outputFile,
            } satisfies SavedRelay);
          },
        }).then(handle => { relayResult = { handle }; }, error => { relayResult = { error }; });
      }

      if (checkResult) {
        const result = checkResult; checkResult = undefined; checkTask = undefined;
        // A late failure from a replaced helper cannot invalidate its successor.
        if (result.relay === relay) {
          checkedAt = Date.now();
          relayReachable = !result.error;
          if (!result.error) {
            relayWasReachable = true; relayHealthFailures = 0; relayFailedAt = 0; relayError = undefined; relayErrorCode = undefined;
            nextCheckAt = checkedAt + 30_000;
          } else {
            relayError = result.error instanceof Error ? result.error.message : "The internet tunnel couldn't connect. Retrying…";
            relayErrorCode = result.error instanceof RelayCheckError ? result.error.code : undefined;
            relayFailedAt ||= checkedAt;
            relayHealthFailures++;
            nextCheckAt = checkedAt + Math.min(15_000, recoveryDelay(relayHealthFailures));
            if (result.canRestart) {
              // Only replace the tunnel after repeated failures with working
              // internet. The daemon, phone keys, chats and PTYs keep running.
              await relay!.close(); relay = undefined;
              relayWasReachable = false; nextRelayAt = Date.now() + recoveryDelay(++relayFailures);
              await fs.rm(relayPath, { force: true });
            }
          }
        }
      }
      const requestedCheck = await readJSON<{ requestedAt: number }>(path.join(directory, "computer-check.json"));
      if (config.relay.kind === "none") checkedAt = Math.max(checkedAt, requestedCheck?.requestedAt ?? 0);
      if (relay?.route && !checkTask && (Date.now() >= nextCheckAt || (requestedCheck?.requestedAt ?? 0) > checkedAt)) {
        const checking = relay;
        checkTask = (async () => {
          try {
            await checkRelay(checking.route!, { signal: abort.signal });
            checkResult = { relay: checking };
          } catch (error) {
            const canRestart = !!checking.owner && relayFailedAt > 0
              && relayNeedsRestart(error, relayHealthFailures, Date.now() - relayFailedAt)
              && await relayProviderReachable(config!.relay, abort.signal).catch(() => false);
            checkResult = { relay: checking, error, canRestart };
          }
        })();
      }

      routes = directRoutes(config, sshPort);
      if (relay?.route && relayWasReachable) routes.push(relay.route);
      const routeJSON = JSON.stringify(routes);
      if (!changingDaemon && client && child?.alive() && routeJSON !== published) {
        try { await client.computer.setRoutes(routes); published = routeJSON; }
        catch { /* Retry publication; never claim the new routes are ready yet. */ }
      }
      const ready = !!client && !!child?.alive() && !changingDaemon && routes.length > 0 && published === routeJSON
        && (config.relay.kind === "none" || relayReachable);
      const state: ControllerState = { ...owner, protocol: CONTROLLER_PROTOCOL, cliVersion: SDK_VERSION, ready, routes, checkedAt,
        phase: ready ? undefined : daemonError ?? relayError ?? relayMessage, error: daemonError ?? relayError,
        errorCode: daemonError ? undefined : relayErrorCode, recovering: !ready };
      const serialized = JSON.stringify(state);
      if (serialized !== lastController) { await writeJSON(controllerPath, state); lastController = serialized; }

      const updatePath = path.join(directory, "computer-update.json");
      const update = !updateTask && client && child?.alive() ? await readJSON<ComputerUpdate>(updatePath) : undefined;
      if (update && ["queued", "downloading", "waiting", "restarting"].includes(update.status)) {
        // Waiting for idle does not block relay recovery or address publication.
        updateTask = (async () => {
          const status = async (state: ComputerUpdate["status"], error?: string) => writeJSON(updatePath,
            { ...update, status: state, error, updatedAt: new Date().toISOString() });
          try {
            await status("downloading");
            const replacement = await ensureDaemonBinary({ version: update.version, cacheDir: process.env.MINDWIRE_RELEASE_CACHE_DIR });
            await status("waiting");
            let lease: Awaited<ReturnType<Mindwire["service"]["acquireUpdate"]>> | undefined;
            while (!stopping && !lease) {
              try { lease = await client!.service.acquireUpdate(); }
              catch (error) { if ((error as { status?: number }).status !== 409) throw error; await delay(2000); }
            }
            if (stopping || !lease) return;
            if (lease.pid !== child?.pid) { await client!.service.releaseUpdate(lease.id); throw new Error("The leased service process changed."); }
            changingDaemon = true;
            await status("restarting");
            await child!.close();
            try {
              client = await launch(replacement);
              const health = await client.health();
              if (health.version.replace(/^v/, "") !== update.version) throw new Error("The replacement service reports a different release version.");
            } catch (error) {
              if (child) await child.close();
              if (!stopping) client = await launch(binary);
              throw error;
            }
            binary = replacement;
            config = { ...config!, daemonBin: undefined, version: update.version };
            await writeJSON(path.join(directory, "computer-config.json"), config);
            await client.computer.setRoutes(routes); published = JSON.stringify(routes);
            await status("complete");
          } catch (error) { await status("failed", error instanceof Error ? error.message : "Service update failed."); }
          finally { changingDaemon = false; published = ""; }
        })().finally(() => { updateTask = undefined; });
      }
      await delay(500);
    }
  } catch (error) {
    await writeJSON(controllerPath, { ...owner, ready: false, error: error instanceof Error ? error.message : "Computer service failed." });
    throw error;
  } finally {
    abort.abort();
    await relayTask; await checkTask; await updateTask;
    if (relayResult?.handle && relayResult.handle !== relay) await relayResult.handle.close();
    await relay?.close();
    if (child) await child.close();
    process.off("SIGTERM", stop); process.off("SIGINT", stop);
    if (stopping) await writeJSON(controllerPath, { pid: 0, ready: false });
  }
}
