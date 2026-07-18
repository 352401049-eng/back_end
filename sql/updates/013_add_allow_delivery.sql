-- 013_add_allow_delivery.sql
-- 商品加 allow_delivery：0=不支持配送 1=支持（默认 1，存量商品视为支持配送）
-- 配合 allow_pickup 实现商品级配送方式控制：
--   虚拟商品（如电影票）商家可设 allow_delivery=0 & allow_pickup=0，前端不显示配送方式选择，背包使用只能到店核销

ALTER TABLE product
  ADD COLUMN allow_delivery TINYINT NOT NULL DEFAULT 1
  COMMENT '0=不支持配送 1=支持'
  AFTER allow_pickup;
