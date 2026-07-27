-- 017_add_order_delivery_fee.sql
-- 订单加配送费/骑手收益快照，下单时锁定，审核通过创建 delivery_order 时透传
-- 避免商家改价影响已下单的配送费/骑手收益

ALTER TABLE `order`
  ADD COLUMN delivery_fee DECIMAL(10,2) NOT NULL DEFAULT 0
  COMMENT '下单时配送费快照（向用户收）'
  AFTER pay_amount,
  ADD COLUMN rider_earnings DECIMAL(10,2) NOT NULL DEFAULT 0
  COMMENT '下单时骑手收益快照（给骑手）'
  AFTER delivery_fee;
