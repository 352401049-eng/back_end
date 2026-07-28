-- 023_add_order_prepay_id.sql
-- 订单表加微信预支付 ID，方便从订单直接查到对应 prepay

ALTER TABLE `order`
  ADD COLUMN prepay_id VARCHAR(64) NULL COMMENT '微信预支付 ID' AFTER pay_expire_at;
