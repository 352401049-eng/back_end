-- 018_add_rider_earning_unique.sql
-- rider_earning 加 (delivery_order_id, status) 联合唯一约束，防止 ConfirmReceipt 并发重复入账
-- 设计：同一配送单同一状态只能有一条收益记录
--   - Pending 状态唯一：防止重复入账（ConfirmReceipt 并发）
--   - Settled 状态唯一：防止重复结账
--   - 拆分结账时：原条变 Settled，新条为 Pending，不冲突

ALTER TABLE rider_earning
  DROP INDEX idx_delivery,
  ADD UNIQUE KEY uk_delivery_status (delivery_order_id, status);
