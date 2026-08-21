import { createOpenAPI } from "fumadocs-openapi/server";

// The daemon's OpenAPI doc, loaded for both the generated REST reference (scripts/gen-openapi-docs.mjs)
// and the interactive <APIPage> renderer. Path is resolved from the apps/web cwd.
export const openapi = createOpenAPI({
  input: ["../../daemon/openapi.json"],
});
