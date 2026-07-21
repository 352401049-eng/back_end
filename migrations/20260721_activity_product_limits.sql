-- 20260721_activity_product_limits.sql
-- 活动商品多维度限购：日/周/月/全程 + 新用户窗
-- activity_max 语义对齐 per_user_max_orders（全程限购）；校验时若 activity_max==0 且 per_user_max_orders>0 则视后者为 activity_max

ALTER TABLE activity_product
  ADD COLUMN daily_max INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '每天限购单数，0=关闭' AFTER per_user_max_orders,
  ADD COLUMN weekly_max INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '每周限购单数，0=关闭' AFTER daily_max,
  ADD COLUMN monthly_max INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '每月限购单数，0=关闭' AFTER weekly_max,
  ADD COLUMN activity_max INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '活动全程限购单数，0=关闭' AFTER monthly_max,
  ADD COLUMN register_hours INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '注册后有效小时，0=关闭' AFTER activity_max,
  ADD COLUMN register_max INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '新用户窗内限购单数' AFTER register_hours;
