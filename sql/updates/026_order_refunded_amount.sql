-- 订单已退款累计金额（支持背包未使用商品部分退款）
ALTER TABLE `order`
  ADD COLUMN `refunded_amount` DECIMAL(10,2) NOT NULL DEFAULT 0.00 COMMENT '已退款金额（元）' AFTER `pay_amount`;
