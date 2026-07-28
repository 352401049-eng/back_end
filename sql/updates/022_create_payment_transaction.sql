-- 022_create_payment_transaction.sql
-- 支付流水表：记录每笔支付/退款的微信侧信息，用于对账和幂等

CREATE TABLE IF NOT EXISTS payment_transaction (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  order_id        BIGINT UNSIGNED NOT NULL COMMENT '关联订单',
  order_no        VARCHAR(32) NOT NULL COMMENT '业务订单号，对应微信 out_trade_no',
  prepay_id       VARCHAR(64) DEFAULT NULL COMMENT '微信预支付 ID',
  transaction_id  VARCHAR(64) DEFAULT NULL COMMENT '微信支付订单号（支付成功后回填）',
  pay_amount      DECIMAL(10,2) NOT NULL COMMENT '支付金额（元）',
  status          TINYINT NOT NULL DEFAULT 0 COMMENT '0=预支付 1=已支付 2=已退款 3=失败',
  wechat_raw      JSON DEFAULT NULL COMMENT '微信回调/响应原始数据',
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_prepay_id (prepay_id),
  UNIQUE KEY uk_transaction_id (transaction_id),
  KEY idx_order (order_id),
  KEY idx_order_no (order_no),
  CONSTRAINT fk_pt_order FOREIGN KEY (order_id) REFERENCES `order` (id)
    ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='支付流水';
