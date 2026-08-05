-- 商品/活动过期天数 + 待核销 usage 过期快照
ALTER TABLE `product`
  ADD COLUMN `deal_expire_days` INT UNSIGNED NULL DEFAULT NULL COMMENT '团购入待核销后过期天数，NULL/0=永不' AFTER `group_buy_max_concurrent_teams`,
  ADD COLUMN `group_expire_days` INT UNSIGNED NULL DEFAULT NULL COMMENT '拼团入待核销后过期天数，NULL/0=永不' AFTER `deal_expire_days`;

ALTER TABLE `activity_product`
  ADD COLUMN `expire_days` INT UNSIGNED NULL DEFAULT NULL COMMENT '覆盖商品过期；NULL=沿用商品对应通道' AFTER `group_buy_max_concurrent_teams`;

ALTER TABLE `user_inventory_usage`
  ADD COLUMN `expire_at` DATETIME NULL DEFAULT NULL COMMENT '待核销过期时间快照，NULL=永不' AFTER `option_select_status`,
  ADD KEY `idx_uiu_expire_at` (`status`, `delivery_type`, `expire_at`);
