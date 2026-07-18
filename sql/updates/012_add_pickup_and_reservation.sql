-- 012_add_pickup_and_reservation.sql
-- 商品加 allow_pickup：0=不支持自取 1=支持（默认 1，存量商品视为可自取）
-- 商家加 allow_reservation：0=不可订座 1=可订座（默认 0，存量商家默认不可订座）

ALTER TABLE product
  ADD COLUMN allow_pickup TINYINT NOT NULL DEFAULT 1
  COMMENT '0=不支持自取 1=支持'
  AFTER enable_coupon;

ALTER TABLE merchant_profile
  ADD COLUMN allow_reservation TINYINT NOT NULL DEFAULT 0
  COMMENT '0=不可订座 1=可订座'
  AFTER status;
