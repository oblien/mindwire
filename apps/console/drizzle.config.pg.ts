import { defineConfig } from "drizzle-kit";

export default defineConfig({
  schema: "./server/db-schema.pg.ts",
  out: "./drizzle/pg",
  dialect: "postgresql",
  dbCredentials: { url: process.env.DATABASE_URL ?? "postgres://mindwire:mindwire@127.0.0.1:5432/mindwire" },
});
