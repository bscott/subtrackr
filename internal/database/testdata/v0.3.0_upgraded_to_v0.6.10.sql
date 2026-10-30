-- Captured by creating a database with v0.3.0, then upgrading it with the v0.6.10 release (db26a58).
CREATE TABLE `categories` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`created_at` datetime,`updated_at` datetime);
CREATE UNIQUE INDEX `idx_categories_name` ON `categories`(`name`);
CREATE TABLE `subscriptions` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`cost` real NOT NULL,`schedule` text NOT NULL,`status` text NOT NULL,`category_id` integer,`payment_method` text,`account` text,`start_date` datetime,`renewal_date` datetime,`cancellation_date` datetime,`url` text,`notes` text,`usage` text,`created_at` datetime,`updated_at` datetime, original_currency TEXT DEFAULT 'USD', date_calculation_version INTEGER DEFAULT 1, icon_url TEXT DEFAULT '', last_reminder_sent DATETIME, last_reminder_renewal_date DATETIME, last_cancellation_reminder_sent DATETIME, last_cancellation_reminder_date DATETIME, schedule_interval INTEGER DEFAULT 1, reminder_enabled INTEGER DEFAULT 1, autopay INTEGER, label TEXT DEFAULT '', share_count INTEGER DEFAULT 1, last_reminder_window INTEGER DEFAULT -1, last_cancellation_reminder_window INTEGER DEFAULT -1, cancellation_notice_days INTEGER DEFAULT 0,CONSTRAINT `fk_subscriptions_category` FOREIGN KEY (`category_id`) REFERENCES `categories`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE);
CREATE TABLE `settings` (`id` integer PRIMARY KEY AUTOINCREMENT,`key` text NOT NULL,`value` text,`created_at` datetime,`updated_at` datetime);
CREATE UNIQUE INDEX `idx_settings_key` ON `settings`(`key`);
CREATE TABLE `api_keys` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`key` text NOT NULL,`last_used` datetime,`usage_count` integer DEFAULT 0,`created_at` datetime,`updated_at` datetime);
CREATE UNIQUE INDEX `idx_api_keys_key` ON `api_keys`(`key`);
CREATE TABLE `exchange_rates` (`id` integer PRIMARY KEY AUTOINCREMENT,`base_currency` text NOT NULL,`currency` text NOT NULL,`rate` real NOT NULL,`date` datetime NOT NULL,`created_at` datetime,`updated_at` datetime);
CREATE TABLE `tags` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`created_at` datetime,`updated_at` datetime);
CREATE UNIQUE INDEX `idx_tags_name` ON `tags`(`name`);
CREATE TABLE subscription_tags (
			subscription_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			PRIMARY KEY (subscription_id, tag_id),
			FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE,
			FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
		);
