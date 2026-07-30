-- 031_takeout_order.sql
CREATE TABLE IF NOT EXISTS `takeout_order` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `order_no` VARCHAR(32) NOT NULL,
  `account_id` BIGINT UNSIGNED NOT NULL,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0待支付 1配餐中 2待骑手/配送中 3已完成 8已取消',
  `goods_amount` DECIMAL(10,2) NOT NULL,
  `delivery_fee` DECIMAL(10,2) NOT NULL DEFAULT 0,
  `rider_earnings` DECIMAL(10,2) NOT NULL DEFAULT 0,
  `pay_amount` DECIMAL(10,2) NOT NULL,
  `pay_status` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `paid_at` DATETIME NULL,
  `pay_expire_at` DATETIME NULL,
  `refunded_amount` DECIMAL(10,2) NOT NULL DEFAULT 0,
  `address_snapshot` JSON NULL,
  `delivery_time_remark` VARCHAR(128) NOT NULL DEFAULT '',
  `package_selections` JSON NULL,
  `option_selections` JSON NULL,
  `delivery_order_id` BIGINT UNSIGNED NULL,
  `remark` VARCHAR(512) NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_takeout_order_no` (`order_no`),
  KEY `idx_takeout_account` (`account_id`),
  KEY `idx_takeout_merchant_status` (`merchant_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `takeout_order_item` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `takeout_order_id` BIGINT UNSIGNED NOT NULL,
  `product_id` BIGINT UNSIGNED NOT NULL,
  `product_name` VARCHAR(128) NOT NULL,
  `product_image` VARCHAR(512) NULL,
  `unit_price` DECIMAL(10,2) NOT NULL,
  `quantity` INT UNSIGNED NOT NULL,
  `subtotal` DECIMAL(10,2) NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_deleted` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_toi_order` (`takeout_order_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE `delivery_order`
  ADD COLUMN `takeout_order_id` BIGINT UNSIGNED NULL COMMENT '外卖主单' AFTER `inventory_usage_id`,
  ADD KEY `idx_delivery_takeout` (`takeout_order_id`);

ALTER TABLE `payment_transaction`
  ADD COLUMN `subject_type` VARCHAR(32) NOT NULL DEFAULT 'order' COMMENT 'order|takeout|delivery_fee' AFTER `id`,
  ADD COLUMN `subject_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER `subject_type`;
UPDATE payment_transaction SET subject_type='order', subject_id=order_id WHERE subject_id=0;
