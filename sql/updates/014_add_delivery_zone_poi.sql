-- 014_add_delivery_zone_poi.sql
-- 配送范围增加地标快照：检索到的 POI 缓存，用于商家端展示与（未来）用户端复用
ALTER TABLE merchant_delivery_zone
  ADD COLUMN poi_landmarks JSON NULL
  COMMENT '区域内地标快照：[{name,latitude,longitude,category}]，由腾讯地图检索生成'
  AFTER spots;
