CREATE TABLE `computer_directory` (
	`directory_id` text PRIMARY KEY NOT NULL,
	`owner_id` text NOT NULL,
	`computer_id` text NOT NULL,
	`public_key` text NOT NULL,
	`revoked` integer DEFAULT 0 NOT NULL,
	`sequence` integer DEFAULT 0 NOT NULL,
	`digest` text,
	`enabled` integer DEFAULT 0 NOT NULL,
	`expires_at` integer DEFAULT 0 NOT NULL,
	`created_at` integer NOT NULL,
	`publication` text
);
--> statement-breakpoint
CREATE INDEX `computer_directory_owner` ON `computer_directory` (`owner_id`);--> statement-breakpoint
CREATE INDEX `computer_directory_expiry` ON `computer_directory` (`expires_at`);