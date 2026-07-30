package service

import (
	"fmt"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NormalizeDailyRefreshTime 接受 HH:MM / HH:MM:SS，规范为 HH:MM:SS；空则 00:00:00。
func NormalizeDailyRefreshTime(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "00:00:00", nil
	}
	if len(s) == 5 && s[2] == ':' {
		s = s + ":00"
	}
	t, err := time.Parse("15:04:05", s)
	if err != nil {
		return "", fmt.Errorf("%w: daily_refresh_time 格式无效", ErrInvalidProductArg)
	}
	return t.Format("15:04:05"), nil
}

// PlatformDailyBucketKey 按刷新时刻计算当前周期键（YYYY-MM-DD）。
// 日限窗口边界与 calendarWindowAt(..., "day", refreshTime) 一致。
// 例：刷新 10:00，则 7/30 10:00～7/31 09:59:59 同属 2026-07-30。
func PlatformDailyBucketKey(refreshTime string, now time.Time) string {
	rt, err := NormalizeDailyRefreshTime(refreshTime)
	if err != nil {
		rt = "00:00:00"
	}
	parsed, _ := time.Parse("15:04:05", rt)
	loc := now.Location()
	y, m, d := now.Date()
	refreshToday := time.Date(y, m, d, parsed.Hour(), parsed.Minute(), parsed.Second(), 0, loc)
	bucketDate := time.Date(y, m, d, 0, 0, 0, 0, loc)
	if now.Before(refreshToday) {
		bucketDate = bucketDate.AddDate(0, 0, -1)
	}
	return bucketDate.Format("2006-01-02")
}

// ensurePlatformDailyBucketLocked 在已锁定的活动商品上惰性切桶；返回当前桶键。
func ensurePlatformDailyBucketLocked(tx *gorm.DB, ap *model.ActivityProduct, now time.Time) (string, error) {
	if ap == nil || ap.PlatformDailyMax == 0 {
		return "", nil
	}
	key := PlatformDailyBucketKey(ap.DailyRefreshTime, now)
	if ap.PlatformDailyBucket == key {
		return key, nil
	}
	if err := tx.Model(ap).Where("id = ?", ap.ID).Updates(map[string]interface{}{
		"platform_daily_sold":   0,
		"platform_daily_bucket": key,
	}).Error; err != nil {
		return "", err
	}
	ap.PlatformDailySold = 0
	ap.PlatformDailyBucket = key
	return key, nil
}

func platformDailyRemaining(ap *model.ActivityProduct) uint32 {
	if ap == nil || ap.PlatformDailyMax == 0 {
		return ^uint32(0) // 视为无限，调用方用 min 时需先判断 Max==0
	}
	if ap.PlatformDailySold >= ap.PlatformDailyMax {
		return 0
	}
	return ap.PlatformDailyMax - ap.PlatformDailySold
}

// creditPlatformDailyLocked 在已锁定且已切桶的 ap 上占用件数，返回写入订单的桶键。
func creditPlatformDailyLocked(tx *gorm.DB, ap *model.ActivityProduct, quantity uint32) (bucket *string, err error) {
	if ap == nil || ap.PlatformDailyMax == 0 || quantity == 0 {
		return nil, nil
	}
	if ap.PlatformDailySold+quantity > ap.PlatformDailyMax {
		return nil, ErrActivityLimitExceeded
	}
	res := tx.Model(&model.ActivityProduct{}).
		Where("id = ? AND platform_daily_bucket = ? AND platform_daily_sold + ? <= platform_daily_max",
			ap.ID, ap.PlatformDailyBucket, quantity).
		Update("platform_daily_sold", gorm.Expr("platform_daily_sold + ?", quantity))
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrActivityLimitExceeded
	}
	ap.PlatformDailySold += quantity
	b := ap.PlatformDailyBucket
	return &b, nil
}

// rollbackPlatformDailyLocked 同桶才释放；跨桶 no-op。
func rollbackPlatformDailyLocked(tx *gorm.DB, ap *model.ActivityProduct, quantity uint32, orderBucket string) error {
	if ap == nil || ap.PlatformDailyMax == 0 || quantity == 0 || orderBucket == "" {
		return nil
	}
	if ap.PlatformDailyBucket != orderBucket {
		return nil
	}
	res := tx.Model(&model.ActivityProduct{}).
		Where("id = ? AND platform_daily_bucket = ? AND platform_daily_sold >= ?",
			ap.ID, orderBucket, quantity).
		Update("platform_daily_sold", gorm.Expr("platform_daily_sold - ?", quantity))
	if res.Error != nil {
		return res.Error
	}
	// RowsAffected==0：计数已漂或已跨桶，忽略以免阻断取消/退款
	if res.RowsAffected > 0 && ap.PlatformDailySold >= quantity {
		ap.PlatformDailySold -= quantity
	}
	return nil
}

func lockActivityProduct(tx *gorm.DB, id uint64) (*model.ActivityProduct, error) {
	var ap model.ActivityProduct
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		First(&ap, id).Error; err != nil {
		return nil, err
	}
	return &ap, nil
}
