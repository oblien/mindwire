import { defineConfig } from "drizzle-kit";

export default defineConfig({
  schema: "./server/db-schema.sqlite.ts",
  out: "./drizzle/sqlite",
  dialect: "sqlite",
  dbCredentials: { url: process.env.AUTH_DB_PATH ?? "./.data/auth.db" },
});
