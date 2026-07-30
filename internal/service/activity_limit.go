package service

import (
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/gorm"
)

// orderStatusesExcludedFromBoughtQty 不计入用户限购已购件数的终态订单。
// 必须用 []int：GORM 会把 []uint8 当成 []byte 绑成 '<binary>'，触发 MySQL 1064。
var orderStatusesExcludedFromBoughtQty = []int{
	int(model.OrderStatusCancelled),
	int(model.OrderStatusGroupFailed),
}

// refreshInstantOnDate returns the refresh clock on calendar date d (local).
func refreshInstantOnDate(d time.Time, refreshTime string) time.Time {
	rt, err := NormalizeDailyRefreshTime(refreshTime)
	if err != nil {
		rt = "00:00:00"
	}
	parsed, _ := time.Parse("15:04:05", rt)
	y, m, day := d.Date()
	loc := d.Location()
	return time.Date(y, m, day, parsed.Hour(), parsed.Minute(), parsed.Second(), 0, loc)
}

// calendarWindowAt returns a half-open period [start, end) for unit
// "day" | "week" | "month" aligned to daily_refresh_time.
// Week starts Monday at refresh; month starts on the 1st at refresh.
// Invalid refreshTime falls back to 00:00:00. Unknown unit returns zero times.
func calendarWindowAt(now time.Time, unit string, refreshTime string) (start, end time.Time) {
	loc := now.Location()
	y, m, d := now.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, loc)

	switch unit {
	case "day":
		refreshToday := refreshInstantOnDate(midnight, refreshTime)
		start = refreshToday
		if now.Before(refreshToday) {
			start = refreshToday.AddDate(0, 0, -1)
		}
		return start, start.AddDate(0, 0, 1)
	case "week":
		offset := (int(now.Weekday()) + 6) % 7
		monday := midnight.AddDate(0, 0, -offset)
		refreshMonday := refreshInstantOnDate(monday, refreshTime)
		start = refreshMonday
		if now.Before(refreshMonday) {
			start = refreshMonday.AddDate(0, 0, -7)
		}
		return start, start.AddDate(0, 0, 7)
	case "month":
		firstOfMonth := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		refreshFirst := refreshInstantOnDate(firstOfMonth, refreshTime)
		start = refreshFirst
		if now.Before(refreshFirst) {
			prev := firstOfMonth.AddDate(0, -1, 0)
			start = refreshInstantOnDate(prev, refreshTime)
		}
		endMonth := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
		return start, refreshInstantOnDate(endMonth, refreshTime)
	default:
		return time.Time{}, time.Time{}
	}
}

// calendarWindow is midnight-based; prefer calendarWindowAt with DailyRefreshTime.
func calendarWindow(now time.Time, unit string) (start, end time.Time) {
	return calendarWindowAt(now, unit, "00:00:00")
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

// sumBoughtQtyInWindow 统计账号对该活动商品的已购件数（排除取消与拼团失败）。
// start/end 均为非零时限制 o.created_at ∈ [start, end)；否则不限时间。
func sumBoughtQtyInWindow(db *gorm.DB, accountID, activityProductID uint64, start, end time.Time) (uint32, error) {
	q := db.Table("order_item oi").
		Select("COALESCE(SUM(oi.quantity), 0)").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("o.account_id = ? AND oi.activity_product_id = ? AND oi.is_deleted = ?", accountID, activityProductID, model.NotDeleted).
		Where("o.status NOT IN ?", orderStatusesExcludedFromBoughtQty)
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
	if ap.PlatformDailyMax > 0 {
		sold := ap.PlatformDailySold
		if PlatformDailyBucketKey(ap.DailyRefreshTime, now) != ap.PlatformDailyBucket {
			sold = 0
		}
		var left uint32
		if sold < ap.PlatformDailyMax {
			left = ap.PlatformDailyMax - sold
		}
		tighten(left, "platform_daily")
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
			start, end = calendarWindowAt(now, lim.unit, ap.DailyRefreshTime)
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
