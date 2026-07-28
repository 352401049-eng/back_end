-- 021_add_delivery_pickup_code.sql
-- 配送单增加出餐号、备餐状态、出餐时间、异常原因字段。
-- 出餐号在商家库存确认通过时生成（4位数字），商家确认出餐后 merchant_prepared=1，骑手才能看到并接单。
ALTER TABLE delivery_order
  ADD COLUMN pickup_code VARCHAR(8) NULL COMMENT '出餐号4位数字' AFTER rider_earnings,
  ADD COLUMN merchant_prepared TINYINT NOT NULL DEFAULT 0 COMMENT '0=备餐中 1=已出餐' AFTER pickup_code,
  ADD COLUMN prepared_at DATETIME NULL COMMENT '商家确认出餐时间' AFTER merchant_prepared,
  ADD COLUMN exception_reason VARCHAR(512) NULL COMMENT '骑手上报异常原因' AFTER prepared_at,
  ADD INDEX idx_pickup_code (pickup_code);
