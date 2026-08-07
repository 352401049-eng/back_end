package service

import (
	"strings"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

// FindAccountIDsByKeyword 按昵称/手机号模糊匹配账号 ID。
// keyword 为空时返回 (nil, false, nil) 表示不加筛选；
// 有关键词但无命中时返回 ([], true, nil)，调用方应直接返回空列表。
func FindAccountIDsByKeyword(db *gorm.DB, keyword string) (ids []uint64, empty bool, err error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, false, nil
	}
	like := "%" + keyword + "%"
	err = query.NotDeleted(db.Model(&model.Account{})).
		Where("nickname LIKE ? OR phone LIKE ? OR CAST(id AS CHAR) LIKE ?", like, like, like).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, false, err
	}
	if len(ids) == 0 {
		return ids, true, nil
	}
	return ids, false, nil
}
