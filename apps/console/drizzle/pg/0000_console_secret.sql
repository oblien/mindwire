CREATE TABLE IF NOT EXISTS "console_secret" (
  "owner_id" text NOT NULL,
  "name" text NOT NULL,
  "kind" text NOT NULL,
  "ciphertext" text NOT NULL,
  "updated_at" bigint NOT NULL,
  PRIMARY KEY ("owner_id", "name")
);
