/**
 * mindwire — a typed TypeScript client for the mindwire daemon.
 *
 * One SDK for every coding-agent harness (Claude Code, Codex, Grok Build, opencode). The
 * daemon normalizes each agent to a single protocol; this package is a thin, dependency-free
 * client over its REST + SSE surface.
 */

export { Mindwire, AuthApi, PromptsApi, McpApi, ProvidersApi, NotifyApi } from "./client.js";
export type { MindwireOptions, AgentScoped } from "./client.js";

export { Run } from "./run.js";
export type { StreamOptions, WaitResult } from "./run.js";

export { Http } from "./http.js";
export type { HttpOptions, FetchLike, RequestInitLike, BaseResolver, TokenGetter } from "./http.js";

export { startEmbedded } from "./embedded.js";
export type { EmbeddedOptions, EmbeddedDaemon } from "./embedded.js";

// Destinations — where the daemon runs and how the SDK reaches it. `local`/`remote` are the
// dependency-free built-ins; `ssh`/`docker`/`oblien` each ensure a daemon on a box/container/workspace
// (their provider peers `ssh2`/`dockerode`/`oblien` load lazily, only when you call the factory).
export { local, remote } from "./target/index.js";
export type { Target, TargetHandle, ConnectSpec, RemoteOptions } from "./target/index.js";
export { ssh, provisionSsh, provisionSshContainer } from "./target/ssh.js";
export type { SshOptions, SshProvisionConfig, SshContainerProvisionConfig } from "./target/ssh.js";
export { docker, provisionDocker } from "./target/docker.js";
export type { DockerConfig, DockerEngineOptions, DockerodeLike } from "./target/docker.js";
export { oblien, provisionOblien } from "./target/oblien.js";
export type { OblienConfig } from "./target/oblien.js";
export { provisionContainer, ContainerHost } from "./target/container.js";
export type { ContainerConfig, ContainerHandle } from "./target/container.js";
export { ensureDaemon, resolveLinuxDaemon } from "./target/host.js";
export type { SandboxHost, ExecResult, EnsureDaemonConfig, EnsureEvent } from "./target/host.js";
export { SDK_VERSION } from "./version.js";
export { ensureDaemonBinary } from "./daemon-binary.js";
export type { EnsureDaemonBinaryOptions, DaemonPlatform, DaemonArch } from "./daemon-binary.js";

// The models.dev catalog — fetched live, never bundled. Pure "which providers/models exist" reference
// data; knows nothing about the daemon or a selected agent.
export {
  catalogProviders,
  catalogProvider,
  catalogModels,
  lookupModel,
  loadCatalog,
  clearCatalogCache,
  MODELS_DEV_URL,
} from "./catalog/index.js";
export type { CatalogOptions } from "./catalog/index.js";

export { MindwireError, ApiError, RunFailedError, TimeoutError } from "./errors.js";

export * from "./types.js";
