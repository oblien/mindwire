CREATE TABLE "computer_directory" (
	"directory_id" text PRIMARY KEY NOT NULL,
	"owner_id" text NOT NULL,
	"computer_id" text NOT NULL,
	"public_key" text NOT NULL,
	"revoked" integer DEFAULT 0 NOT NULL,
	"sequence" bigint DEFAULT 0 NOT NULL,
	"digest" text,
	"enabled" integer DEFAULT 0 NOT NULL,
	"expires_at" bigint DEFAULT 0 NOT NULL,
	"created_at" bigint NOT NULL,
	"publication" text
);
--> statement-breakpoint
CREATE INDEX "computer_directory_owner" ON "computer_directory" USING btree ("owner_id");--> statement-breakpoint
CREATE INDEX "computer_directory_expiry" ON "computer_directory" USING btree ("expires_at");