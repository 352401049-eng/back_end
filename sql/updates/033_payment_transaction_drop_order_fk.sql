-- 033_payment_transaction_drop_order_fk.sql
-- 外卖/配送费支付流水 order_id=0，不能再强制外键引用 order 表
ALTER TABLE `payment_transaction` DROP FOREIGN KEY `fk_pt_order`;
