-- 019_drop_rider_earning_unique.sql
-- 删除 rider_earning 的联合唯一索引 uk_delivery_status
-- 原因：拆分结账逻辑需要允许同一 delivery_order_id 存在多条同状态记录
--   拆分产生 (delivery_order_id=X, Settled) + (delivery_order_id=X, Pending)
--   下次结账 Pending 那条改为 Settled 时，会与已有的 (X, Settled) 冲突
--   即同一配送单拆分2次必然违反联合唯一索引
-- 防并发重复入账改由 ConfirmReceipt 的行锁 + status=DeliveryDelivered 校验保证（已实现）

ALTER TABLE rider_earning
  DROP INDEX uk_delivery_status;

-- 恢复普通索引（便于按 delivery_order_id 查询）
ALTER TABLE rider_earning
  ADD INDEX idx_delivery (delivery_order_id);
