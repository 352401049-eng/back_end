-- 20260722_product_package.sql
-- 平台套餐商品：分组 x 选 y + 订单父子拆单

ALTER TABLE `order`
  ADD COLUMN parent_order_id BIGINT UNSIGNED NULL COMMENT '父订单ID，子单指向套餐父单' AFTER id,
  ADD COLUMN package_product_id BIGINT UNSIGNED NULL COMMENT '套餐商品ID（父单）' AFTER merchant_id,
  ADD INDEX idx_order_parent (parent_order_id),
  ADD INDEX idx_order_package_product (package_product_id);

CREATE TABLE IF NOT EXISTS product_package_group (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  package_product_id BIGINT UNSIGNED NOT NULL COMMENT '套餐商品ID',
  name VARCHAR(64) NOT NULL DEFAULT '' COMMENT '分组名',
  select_count INT UNSIGNED NOT NULL DEFAULT 1 COMMENT '组内须选总份数 y',
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  is_deleted TINYINT UNSIGNED NOT NULL DEFAULT 0,
  INDEX idx_pkg_group_product (package_product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='套餐分组';

CREATE TABLE IF NOT EXISTS product_package_item (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  group_id BIGINT UNSIGNED NOT NULL COMMENT '分组ID',
  product_id BIGINT UNSIGNED NOT NULL COMMENT '候选商品ID',
  merchant_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '候选所属商家',
  max_qty INT UNSIGNED NOT NULL DEFAULT 1 COMMENT '组内该商品可选上限',
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  is_deleted TINYINT UNSIGNED NOT NULL DEFAULT 0,
  INDEX idx_pkg_item_group (group_id),
  INDEX idx_pkg_item_product (product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='套餐分组候选商品';
