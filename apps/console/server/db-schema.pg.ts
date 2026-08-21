import { bigint, pgTable, primaryKey, text } from "drizzle-orm/pg-core";

export const consoleSecret = pgTable("console_secret", {
  ownerId: text("owner_id").notNull(),
  name: text("name").notNull(),
  kind: text("kind").notNull(),
  ciphertext: text("ciphertext").notNull(),
  updatedAt: bigint("updated_at", { mode: "number" }).notNull(),
}, (table) => [primaryKey({ columns: [table.ownerId, table.name] })]);
