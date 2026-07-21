-- 20260722_product_package_instore.sql
-- 美团式店内套餐：分组类型 + 清理跨店套餐 + 平台 mid=0 同店套餐改挂本店

ALTER TABLE product_package_group
  ADD COLUMN group_type TINYINT UNSIGNED NOT NULL DEFAULT 2 COMMENT '1=固定包含 2=可选N选M' AFTER name;

-- 软删跨店套餐（候选商品分属多家店）
UPDATE product p
SET p.is_deleted = 1, p.status = 0
WHERE p.item_type = 3
  AND p.is_deleted = 0
  AND (
    SELECT COUNT(DISTINCT i.merchant_id)
    FROM product_package_group g
    INNER JOIN product_package_item i ON i.group_id = g.id AND i.is_deleted = 0
    WHERE g.package_product_id = p.id AND g.is_deleted = 0
  ) > 1;

-- 同步软删其分组
UPDATE product_package_group g
INNER JOIN product p ON p.id = g.package_product_id
SET g.is_deleted = 1
WHERE p.item_type = 3 AND p.is_deleted = 1 AND g.is_deleted = 0;

UPDATE product_package_item i
INNER JOIN product_package_group g ON g.id = i.group_id
SET i.is_deleted = 1
WHERE g.is_deleted = 1 AND i.is_deleted = 0;

-- merchant_id=0 且候选同属一店 → 改挂该店
UPDATE product p
INNER JOIN (
  SELECT g.package_product_id AS pid, MIN(i.merchant_id) AS mid
  FROM product_package_group g
  INNER JOIN product_package_item i ON i.group_id = g.id AND i.is_deleted = 0
  WHERE g.is_deleted = 0
  GROUP BY g.package_product_id
  HAVING COUNT(DISTINCT i.merchant_id) = 1 AND MIN(i.merchant_id) > 0
) t ON t.pid = p.id
SET p.merchant_id = t.mid
WHERE p.item_type = 3 AND p.merchant_id = 0 AND p.is_deleted = 0;
