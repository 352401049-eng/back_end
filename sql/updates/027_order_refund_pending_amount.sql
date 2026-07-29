-- 微信退款进行中预留金额（成功回调后再计入 refunded_amount）
ALTER TABLE `order`
  ADD COLUMN `refund_pending_amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '退款中预留金额（元）' AFTER `refunded_amount`;
