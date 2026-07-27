-- 016_add_rider_earnings.sql
-- 骑手收益与配送费体系
-- 1. merchant_profile 加配送费/骑手收益配置（管理端按店设置）
-- 2. delivery_order 加金额快照（下单时锁定，不受后续改价影响）
-- 3. rider_earning 骑手收益记录（每单一记）
-- 4. rider_settlement 骑手结账/提现记录

-- 1. 商家配送费配置
ALTER TABLE merchant_profile
  ADD COLUMN delivery_fee DECIMAL(10,2) NOT NULL DEFAULT 0
  COMMENT '向用户收取的配送费'
  AFTER close_time,
  ADD COLUMN rider_earnings DECIMAL(10,2) NOT NULL DEFAULT 0
  COMMENT '骑手每单收益（独立于配送费）'
  AFTER delivery_fee;

-- 2. 配送单金额快照
ALTER TABLE delivery_order
  ADD COLUMN delivery_fee DECIMAL(10,2) NOT NULL DEFAULT 0
  COMMENT '下单时配送费快照（向用户收）'
  AFTER deliver_photos,
  ADD COLUMN rider_earnings DECIMAL(10,2) NOT NULL DEFAULT 0
  COMMENT '下单时骑手收益快照（给骑手）'
  AFTER delivery_fee;

-- 3. 骑手收益记录
CREATE TABLE rider_earning (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  rider_id BIGINT NOT NULL COMMENT '骑手 account_id',
  delivery_order_id BIGINT NOT NULL COMMENT '配送单 ID',
  order_id BIGINT COMMENT '冗余订单 ID 便于查询',
  merchant_id BIGINT NOT NULL COMMENT '商家 ID',
  amount DECIMAL(10,2) NOT NULL COMMENT '收益金额（下单时快照）',
  status TINYINT NOT NULL DEFAULT 0 COMMENT '0=待结账 1=已结账 2=已取消',
  settlement_id BIGINT COMMENT '关联结账单（已结账时填）',
  created_at DATETIME NOT NULL,
  settled_at DATETIME COMMENT '结账时间',
  is_deleted TINYINT NOT NULL DEFAULT 0,
  INDEX idx_rider_status (rider_id, status),
  INDEX idx_delivery (delivery_order_id),
  INDEX idx_settlement (settlement_id)
) COMMENT='骑手收益记录';

-- 4. 骑手结账/提现记录
CREATE TABLE rider_settlement (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  rider_id BIGINT NOT NULL COMMENT '骑手 account_id',
  amount DECIMAL(10,2) NOT NULL COMMENT '结账金额',
  status TINYINT NOT NULL DEFAULT 0 COMMENT '0=待审批 1=通过 2=拒绝',
  source TINYINT NOT NULL DEFAULT 0 COMMENT '0=骑手申请 1=管理员主动',
  operator_id BIGINT COMMENT '审批管理员 account_id',
  applicant_id BIGINT COMMENT '申请人 account_id（骑手申请时填自己）',
  reviewed_at DATETIME COMMENT '审批时间',
  reject_reason VARCHAR(256) COMMENT '拒绝原因',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  is_deleted TINYINT NOT NULL DEFAULT 0,
  INDEX idx_rider_status (rider_id, status),
  INDEX idx_status (status)
) COMMENT='骑手结账/提现记录';
