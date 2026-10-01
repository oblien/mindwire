import { index, integer, primaryKey, sqliteTable, text } from "drizzle-orm/sqlite-core";

export const consoleSecret = sqliteTable("console_secret", {
  ownerId: text("owner_id").notNull(),
  name: text("name").notNull(),
  kind: text("kind").notNull(),
  ciphertext: text("ciphertext").notNull(),
  updatedAt: integer("updated_at").notNull(),
}, (table) => [primaryKey({ columns: [table.ownerId, table.name] })]);

export const computerDirectory = sqliteTable("computer_directory", {
  directoryId: text("directory_id").primaryKey(),
  computerId: text("computer_id").notNull(),
  publicKey: text("public_key").notNull(),
  revoked: integer("revoked").notNull().default(0),
  sequence: integer("sequence").notNull().default(0),
  digest: text("digest"),
  enabled: integer("enabled").notNull().default(0),
  expiresAt: integer("expires_at").notNull().default(0),
  createdAt: integer("created_at").notNull(),
  publication: text("publication"),
}, (table) => [index("computer_directory_expiry").on(table.expiresAt)]);

export const directoryEnrollmentBudget = sqliteTable("computer_directory_enrollment_budget", {
  source: text("source").notNull(),
  day: integer("day").notNull(),
  count: integer("count").notNull(),
}, table => [primaryKey({ columns: [table.source, table.day] }), index("computer_directory_budget_day").on(table.day)]);
