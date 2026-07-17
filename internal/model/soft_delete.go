package model

const (
	NotDeleted uint8 = 0
	Deleted    uint8 = 1
)

// SoftDelete 逻辑删除标记（is_deleted: 0=正常 1=已删除）。
type SoftDelete struct {
	IsDeleted uint8 `gorm:"column:is_deleted;not null;default:0;index" json:"-"`
}
