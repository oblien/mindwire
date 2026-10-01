CREATE TABLE "computer_directory_enrollment_budget" (
	"source" text NOT NULL,
	"day" integer NOT NULL,
	"count" integer NOT NULL,
	CONSTRAINT "computer_directory_enrollment_budget_source_day_pk" PRIMARY KEY("source","day")
);
--> statement-breakpoint
DROP INDEX "computer_directory_owner";--> statement-breakpoint
CREATE INDEX "computer_directory_budget_day" ON "computer_directory_enrollment_budget" USING btree ("day");--> statement-breakpoint
ALTER TABLE "computer_directory" DROP COLUMN "owner_id";