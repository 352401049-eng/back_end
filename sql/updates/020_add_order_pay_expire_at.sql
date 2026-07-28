-- 020_add_order_pay_expire_at.sql
-- 为待支付订单增加支付超时时间戳，供超时关单 worker 扫描。
-- 微信支付接入后，下单未支付的订单会在该时间后被自动关单并回滚库存/券。
ALTER TABLE `order`
  ADD COLUMN pay_expire_at DATETIME NULL DEFAULT NULL COMMENT '支付超时时间，超时未支付则关单' AFTER paid_at;
