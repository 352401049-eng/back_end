-- 034_group_buy_member_allow_repeat.sql
-- 同团可重复：同一账号可对同一团下多笔订单，各写一条 member。
-- 原 uk_team_account (team_id, account_id) 会在第二次参团时报 Duplicate entry。
-- 改为按订单唯一 (team_id, order_id)。

ALTER TABLE group_buy_member
  DROP INDEX uk_team_account;

ALTER TABLE group_buy_member
  ADD UNIQUE KEY uk_team_order (team_id, order_id);

ALTER TABLE group_buy_member
  ADD INDEX idx_team_account (team_id, account_id);
