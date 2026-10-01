import { bigint, index, integer, pgTable, primaryKey, text } from "drizzle-orm/pg-core";

export const consoleSecret = pgTable("console_secret", {
  ownerId: text("owner_id").notNull(),
  name: text("name").notNull(),
  kind: text("kind").notNull(),
  ciphertext: text("ciphertext").notNull(),
  updatedAt: bigint("updated_at", { mode: "number" }).notNull(),
}, (table) => [primaryKey({ columns: [table.ownerId, table.name] })]);

export const computerDirectory = pgTable("computer_directory", {
  directoryId: text("directory_id").primaryKey(),
  computerId: text("computer_id").notNull(),
  publicKey: text("public_key").notNull(),
  revoked: integer("revoked").notNull().default(0),
  sequence: bigint("sequence", { mode: "number" }).notNull().default(0),
  digest: text("digest"),
  enabled: integer("enabled").notNull().default(0),
  expiresAt: bigint("expires_at", { mode: "number" }).notNull().default(0),
  createdAt: bigint("created_at", { mode: "number" }).notNull(),
  publication: text("publication"),
}, (table) => [index("computer_directory_expiry").on(table.expiresAt)]);

export const directoryEnrollmentBudget = pgTable("computer_directory_enrollment_budget", {
  source: text("source").notNull(),
  day: integer("day").notNull(),
  count: integer("count").notNull(),
}, table => [primaryKey({ columns: [table.source, table.day] }), index("computer_directory_budget_day").on(table.day)]);
