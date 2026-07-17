package query

import (
	"yujixinjiang/backend/internal/model"

	"gorm.io/gorm"
)

// NotDeleted 过滤未逻辑删除的记录。
func NotDeleted(db *gorm.DB) *gorm.DB {
	return db.Where("is_deleted = ?", model.NotDeleted)
}

// SoftDelete 将记录标记为已删除。
// 用法：SoftDelete(db, &model.CartItem{}, "id = ? AND account_id = ?", id, accountID)
func SoftDelete(db *gorm.DB, dest interface{}, conds ...interface{}) *gorm.DB {
	q := db.Model(dest)
	if len(conds) > 0 {
		if query, ok := conds[0].(string); ok {
			q = q.Where(query, conds[1:]...)
		} else {
			q = q.Where(conds[0], conds[1:]...)
		}
	}
	return q.Where("is_deleted = ?", model.NotDeleted).
		Update("is_deleted", model.Deleted)
}
