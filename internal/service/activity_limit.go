package service

import (
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/gorm"
)

// calendarWindow returns a half-open calendar period [start, end) for unit
// "day" | "week" | "month". Week starts Monday 00:00. end is the next period's start.
// Unknown unit returns zero times.
func calendarWindow(now time.Time, unit string) (start, end time.Time) {
	loc := now.Location()
	y, m, d := now.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, loc)

	switch unit {
	case "day":
		return midnight, midnight.AddDate(0, 0, 1)
	case "week":
		// time.Sunday=0 … Saturday=6 → days since Monday
		offset := (int(now.Weekday()) + 6) % 7
		start = midnight.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 7)
	case "month":
		start = time.Date(y, m, 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0)
	default:
		return time.Time{}, time.Time{}
	}
}

// registerDeadline is createdAt + hours (hours=0 → createdAt).
func registerDeadline(createdAt time.Time, hours uint32) time.Time {
	return createdAt.Add(time.Duration(hours) * time.Hour)
}

// inRegisterWindow is true iff now ∈ [createdAt, registerDeadline).
// hours=0 yields an empty window.
func inRegisterWindow(createdAt, now time.Time, hours uint32) bool {
	deadline := registerDeadline(createdAt, hours)
	return !now.Before(createdAt) && now.Before(deadline)
}

// sumBoughtQtyInWindow 统计账号对该活动商品的已购件数（非取消）。
// start/end 均为非零时限制 o.created_at ∈ [start, end)；否则不限时间。
func sumBoughtQtyInWindow(db *gorm.DB, accountID, activityProductID uint64, start, end time.Time) (uint32, error) {
	q := db.Table("order_item oi").
		Select("COALESCE(SUM(oi.quantity), 0)").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("o.account_id = ? AND oi.activity_product_id = ? AND oi.is_deleted = ?", accountID, activityProductID, model.NotDeleted).
		Where("o.status <> ?", model.OrderStatusCancelled)
	if !start.IsZero() && !end.IsZero() {
		q = q.Where("o.created_at >= ? AND o.created_at < ?", start, end)
	}
	var bought uint32
	err := q.Scan(&bought).Error
	return bought, err
}

// sumBoughtQty 全程已购件数。
func sumBoughtQty(db *gorm.DB, accountID, activityProductID uint64) (uint32, error) {
	return sumBoughtQtyInWindow(db, accountID, activityProductID, time.Time{}, time.Time{})
}

type activityRemainResult struct {
	RemainingQty uint32
	LimitReached bool
	LimitReason  string
}

func minU32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

// computeActivityRemaining 本单最多可买件数 = 库存与各限购剩余的最小值。
// 日/周/月/全程/新用户窗与每人限购一律按「已购件数」累计（非订单笔数）。
// 例：每日限购 3 + 每人 10 → 当天最多共买 3 件；首单买满 3 后当天不可再买。
func computeActivityRemaining(
	db *gorm.DB,
	ap *model.ActivityProduct,
	stock uint32,
	accountID *uint64,
	accountCreatedAt time.Time,
	now time.Time,
) (activityRemainResult, error) {
	out := activityRemainResult{RemainingQty: stock}
	tighten := func(n uint32, reason string) {
		if n == 0 {
			out.RemainingQty = 0
			out.LimitReached = true
			if out.LimitReason == "" {
				out.LimitReason = reason
			}
			return
		}
		out.RemainingQty = minU32(out.RemainingQty, n)
	}

	if ap.PerUserMaxQty > 0 {
		tighten(ap.PerUserMaxQty, "per_user_qty")
	}
	if ap.DailyMax > 0 {
		tighten(ap.DailyMax, "daily")
	}
	if ap.WeeklyMax > 0 {
		tighten(ap.WeeklyMax, "weekly")
	}
	if ap.MonthlyMax > 0 {
		tighten(ap.MonthlyMax, "monthly")
	}
	activityMax := ap.ActivityMax
	if activityMax == 0 && ap.PerUserMaxOrders > 0 {
		activityMax = ap.PerUserMaxOrders
	}
	if activityMax > 0 {
		tighten(activityMax, "activity_max")
	}
	if ap.RegisterHours > 0 && ap.RegisterMax > 0 {
		tighten(ap.RegisterMax, "register_max")
	}

	if accountID == nil || *accountID == 0 {
		return out, nil
	}

	if ap.PerUserMaxQty > 0 {
		bought, err := sumBoughtQty(db, *accountID, ap.ID)
		if err != nil {
			return out, err
		}
		var left uint32
		if bought < ap.PerUserMaxQty {
			left = ap.PerUserMaxQty - bought
		}
		tighten(left, "per_user_qty")
	}

	type qtyLim struct {
		max    uint32
		unit   string
		reason string
	}
	lims := []qtyLim{
		{ap.DailyMax, "day", "daily"},
		{ap.WeeklyMax, "week", "weekly"},
		{ap.MonthlyMax, "month", "monthly"},
		{activityMax, "", "activity_max"},
	}
	for _, lim := range lims {
		if lim.max == 0 {
			continue
		}
		var start, end time.Time
		if lim.unit != "" {
			start, end = calendarWindow(now, lim.unit)
		}
		bought, err := sumBoughtQtyInWindow(db, *accountID, ap.ID, start, end)
		if err != nil {
			return out, err
		}
		var left uint32
		if bought < lim.max {
			left = lim.max - bought
		}
		tighten(left, lim.reason)
	}

	if ap.RegisterHours > 0 && ap.RegisterMax > 0 {
		start := accountCreatedAt
		end := registerDeadline(accountCreatedAt, ap.RegisterHours)
		bought, err := sumBoughtQtyInWindow(db, *accountID, ap.ID, start, end)
		if err != nil {
			return out, err
		}
		var left uint32
		if bought < ap.RegisterMax {
			left = ap.RegisterMax - bought
		}
		tighten(left, "register_max")
	}

	return out, nil
}
