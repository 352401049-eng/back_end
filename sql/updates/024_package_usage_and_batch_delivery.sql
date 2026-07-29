-- 套餐履约选配 + 背包使用记录扩展
-- package_select_status: 0=不适用 1=核销后待商家选配 2=已确认 3=用户外卖已选
ALTER TABLE `user_inventory_usage`
  ADD COLUMN `package_selections` JSON NULL COMMENT '套餐选配快照' AFTER `remark`,
  ADD COLUMN `package_select_status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '套餐选配状态' AFTER `package_selections`;

CREATE INDEX `idx_usage_package_select` ON `user_inventory_usage` (`merchant_id`, `package_select_status`, `status`);
