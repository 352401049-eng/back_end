-- 活动商品平台每日限购 + 刷新桶
ALTER TABLE `activity_product`
  ADD COLUMN `platform_daily_max` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '平台每日限购件数，0=不启用' AFTER `register_max`,
  ADD COLUMN `daily_refresh_time` TIME NOT NULL DEFAULT '00:00:00' COMMENT '平台日限刷新时刻' AFTER `platform_daily_max`,
  ADD COLUMN `platform_daily_sold` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '当前刷新周期已售' AFTER `daily_refresh_time`,
  ADD COLUMN `platform_daily_bucket` VARCHAR(32) NOT NULL DEFAULT '' COMMENT '当前刷新周期键 YYYY-MM-DD' AFTER `platform_daily_sold`;

ALTER TABLE `order_item`
  ADD COLUMN `platform_daily_bucket` VARCHAR(32) NULL DEFAULT NULL COMMENT '下单时平台日限桶' AFTER `activity_product_id`;
