// Better Auth owns its schema; these are the Console-owned typed tables. Keep dialect definitions in
// their own files so Drizzle Kit can generate each checked-in migration stream independently.
export { consoleSecret as pgConsoleSecret } from "./db-schema.pg";
export { consoleSecret as sqliteConsoleSecret } from "./db-schema.sqlite";
