import { integer, primaryKey, sqliteTable, text } from "drizzle-orm/sqlite-core";

export const consoleSecret = sqliteTable("console_secret", {
  ownerId: text("owner_id").notNull(),
  name: text("name").notNull(),
  kind: text("kind").notNull(),
  ciphertext: text("ciphertext").notNull(),
  updatedAt: integer("updated_at").notNull(),
}, (table) => [primaryKey({ columns: [table.ownerId, table.name] })]);
