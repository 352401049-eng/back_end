-- 商家自动审核：开启后用户支付成功直入背包，无需商家点通过
ALTER TABLE `merchant_profile`
  ADD COLUMN `auto_approve` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '1=支付后自动审核入背包' AFTER `allow_reservation`;
