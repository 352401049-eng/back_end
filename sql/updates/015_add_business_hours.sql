-- 015_add_business_hours.sql
-- 商家加营业时间：open_time / close_time，NULL 表示未设置（前端用 10:00-22:00 兜底）

ALTER TABLE merchant_profile
  ADD COLUMN open_time TIME NULL DEFAULT NULL
  COMMENT '营业开始时间，NULL 表示未设置'
  AFTER allow_reservation;

ALTER TABLE merchant_profile
  ADD COLUMN close_time TIME NULL DEFAULT NULL
  COMMENT '营业结束时间，NULL 表示未设置'
  AFTER open_time;
