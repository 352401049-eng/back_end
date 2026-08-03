-- 多店适用 + 使用店字段（可重复执行）
SET @db := DATABASE();

CREATE TABLE IF NOT EXISTS product_applicable_merchant (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  product_id BIGINT UNSIGNED NOT NULL,
  merchant_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_product_merchant (product_id, merchant_id),
  KEY idx_merchant_product (merchant_id, product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='商品适用店面';

-- backfill: every product applicable to its owner
INSERT IGNORE INTO product_applicable_merchant (product_id, merchant_id)
SELECT id, merchant_id FROM product WHERE is_deleted = 0;

-- orders.usage_merchant_id  （表名是 `order`，不是 orders）
SET @exists := (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA=@db AND TABLE_NAME='order' AND COLUMN_NAME='usage_merchant_id');
SET @sql := IF(@exists=0,
  'ALTER TABLE `order` ADD COLUMN usage_merchant_id BIGINT UNSIGNED NULL COMMENT ''实际使用店'' AFTER merchant_id',
  'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
UPDATE `order` SET usage_merchant_id = merchant_id WHERE usage_merchant_id IS NULL;

-- takeout_order.usage_merchant_id
SET @exists := (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA=@db AND TABLE_NAME='takeout_order' AND COLUMN_NAME='usage_merchant_id');
SET @sql := IF(@exists=0,
  'ALTER TABLE takeout_order ADD COLUMN usage_merchant_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT ''实际使用店'' AFTER merchant_id',
  'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
UPDATE takeout_order SET usage_merchant_id = merchant_id WHERE usage_merchant_id = 0;

-- user_inventory_usage.usage_merchant_id
SET @exists := (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA=@db AND TABLE_NAME='user_inventory_usage' AND COLUMN_NAME='usage_merchant_id');
SET @sql := IF(@exists=0,
  'ALTER TABLE user_inventory_usage ADD COLUMN usage_merchant_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT ''实际使用店'' AFTER merchant_id',
  'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
UPDATE user_inventory_usage SET usage_merchant_id = merchant_id WHERE usage_merchant_id = 0;
