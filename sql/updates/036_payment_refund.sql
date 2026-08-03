-- 036_payment_refund.sql
-- 退款回调幂等表：按微信 refund_id / out_refund_no 去重，避免重复累加退款额

CREATE TABLE IF NOT EXISTS `payment_refund` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `order_no` VARCHAR(32) NOT NULL COMMENT '业务订单号=微信 out_trade_no',
  `out_refund_no` VARCHAR(64) NOT NULL COMMENT '商户退款单号',
  `refund_id` VARCHAR(64) NOT NULL COMMENT '微信退款单号',
  `subject_type` VARCHAR(32) NOT NULL DEFAULT 'order' COMMENT 'order|takeout|delivery_fee',
  `subject_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `refund_amount` DECIMAL(10,2) NOT NULL COMMENT '本次退款金额（元）',
  `status` TINYINT NOT NULL DEFAULT 1 COMMENT '1=成功',
  `wechat_raw` JSON NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_refund_id` (`refund_id`),
  UNIQUE KEY `uk_out_refund_no` (`out_refund_no`),
  KEY `idx_pr_order_no` (`order_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='退款回调幂等';
