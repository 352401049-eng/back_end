-- 拼团：同时进行中的团数量上限（0=不限制）
ALTER TABLE `product`
  ADD COLUMN `group_buy_max_concurrent_teams` INT UNSIGNED NOT NULL DEFAULT 0
    COMMENT '同时进行中团数上限，0=不限' AFTER `group_buy_allow_repeat`;

ALTER TABLE `activity_product`
  ADD COLUMN `group_buy_max_concurrent_teams` INT UNSIGNED NOT NULL DEFAULT 0
    COMMENT '同时进行中团数上限，0=不限' AFTER `group_buy_max_joins_per_user`;
