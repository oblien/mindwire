// `dockerode` is an optional, untyped peer used only for a runtime availability probe
// (`dockerAvailable()`), and externalized by tsup so it's never bundled. We don't touch its API here,
// so an ambient `any` declaration is all the type-checker needs — no `@types/dockerode` dependency.
declare module "dockerode";
