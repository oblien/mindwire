CREATE TABLE `computer_directory_enrollment_budget` (
	`source` text NOT NULL,
	`day` integer NOT NULL,
	`count` integer NOT NULL,
	PRIMARY KEY(`source`, `day`)
);
--> statement-breakpoint
CREATE INDEX `computer_directory_budget_day` ON `computer_directory_enrollment_budget` (`day`);--> statement-breakpoint
DROP INDEX `computer_directory_owner`;--> statement-breakpoint
ALTER TABLE `computer_directory` DROP COLUMN `owner_id`;