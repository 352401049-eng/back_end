-- 20260731_product_three_channel.sql
-- 商品三通道：特惠 / 拼团 / 外卖 独立开关与库存

ALTER TABLE product
  ADD COLUMN enable_deal TINYINT UNSIGNED NOT NULL DEFAULT 1,
  ADD COLUMN enable_group TINYINT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN enable_takeout TINYINT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN deal_stock INT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN group_stock INT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN takeout_stock INT UNSIGNED NOT NULL DEFAULT 0;

UPDATE product SET
  enable_deal = 1,
  deal_stock = stock,
  group_stock = 0,
  takeout_stock = 0,
  enable_group = IFNULL(enable_group_buy, 0),
  enable_takeout = IFNULL(allow_delivery, 0);
