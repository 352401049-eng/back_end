-- 20260722_delivery_zone_spots.sql
-- 配送范围支持「配送点 + 半径」模式：mode + spots JSON

ALTER TABLE merchant_delivery_zone
  ADD COLUMN mode VARCHAR(16) NOT NULL DEFAULT 'polygon' COMMENT 'polygon|spots' AFTER enabled,
  ADD COLUMN spots JSON NULL COMMENT '配送点列表 [{name,latitude,longitude,radius_m}]' AFTER points;
