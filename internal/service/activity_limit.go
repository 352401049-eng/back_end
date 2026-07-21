package service

import (
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

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

// countOrders counts non-cancelled order_items for account+activityProduct.
// When start/end are both non-zero, filters o.created_at ∈ [start, end).
func countOrders(db *gorm.DB, accountID, activityProductID uint64, start, end time.Time) (int64, error) {
	q := query.NotDeleted(db).
		Table("order_item oi").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("o.account_id = ? AND oi.activity_product_id = ? AND oi.is_deleted = ?", accountID, activityProductID, model.NotDeleted).
		Where("o.status <> ?", model.OrderStatusCancelled)
	if !start.IsZero() && !end.IsZero() {
		q = q.Where("o.created_at >= ? AND o.created_at < ?", start, end)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
