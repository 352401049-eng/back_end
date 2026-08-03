-- 履约事件时间线：追加写入，供用户/商家/管理三端详情展示
CREATE TABLE IF NOT EXISTS `fulfillment_event` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `subject_type` VARCHAR(32) NOT NULL COMMENT 'order|takeout|delivery|usage|delivery_fee',
  `subject_id` BIGINT UNSIGNED NOT NULL,
  `event_code` VARCHAR(64) NOT NULL,
  `actor_role` VARCHAR(16) NOT NULL DEFAULT 'system',
  `actor_id` BIGINT UNSIGNED NULL,
  `title` VARCHAR(128) NOT NULL,
  `detail` JSON NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_fe_subject` (`subject_type`, `subject_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
