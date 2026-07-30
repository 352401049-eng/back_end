-- 032_delivery_fee_order.sql
-- 背包跑腿配送费预支付单：支付成功后再扣包并建配送单

CREATE TABLE IF NOT EXISTS `delivery_fee_order` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `order_no` VARCHAR(32) NOT NULL,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=pending_pay 1=fulfilled 8=cancelled',
  `amount` DECIMAL(10,2) NOT NULL DEFAULT 0,
  `rider_earnings` DECIMAL(10,2) NOT NULL DEFAULT 0,
  `pay_amount` DECIMAL(10,2) NOT NULL DEFAULT 0,
  `pay_status` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `paid_at` DATETIME NULL,
  `pay_expire_at` DATETIME NULL,
  `refunded_amount` DECIMAL(10,2) NOT NULL DEFAULT 0,
  `payload` JSON NULL COMMENT 'draft use-batch input',
  `delivery_order_id` BIGINT UNSIGNED NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_delivery_fee_order_no` (`order_no`),
  KEY `idx_delivery_fee_account` (`account_id`),
  KEY `idx_delivery_fee_delivery_order` (`delivery_order_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
