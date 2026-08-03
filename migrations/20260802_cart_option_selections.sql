-- 购物车行持久化规格选配（可重复执行）
SET @db := DATABASE();

SET @exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @db AND TABLE_NAME = 'cart_item' AND COLUMN_NAME = 'option_selections'
);
SET @sql := IF(@exists = 0,
  'ALTER TABLE cart_item ADD COLUMN option_selections JSON NULL COMMENT ''规格选配 JSON'' AFTER spec',
  'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @db AND TABLE_NAME = 'cart_item' AND COLUMN_NAME = 'option_text'
);
SET @sql := IF(@exists = 0,
  'ALTER TABLE cart_item ADD COLUMN option_text VARCHAR(512) NULL COMMENT ''规格摘要文案'' AFTER option_selections',
  'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @db AND TABLE_NAME = 'cart_item' AND COLUMN_NAME = 'option_key'
);
SET @sql := IF(@exists = 0,
  'ALTER TABLE cart_item ADD COLUMN option_key VARCHAR(64) NOT NULL DEFAULT '''' COMMENT ''规格合并键'' AFTER option_text',
  'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @exists := (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @db AND TABLE_NAME = 'cart_item' AND INDEX_NAME = 'idx_cart_option_key'
);
SET @sql := IF(@exists = 0,
  'ALTER TABLE cart_item ADD INDEX idx_cart_option_key (account_id, product_id, purchase_type, option_key)',
  'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
