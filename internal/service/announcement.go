package service

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrAnnouncementNotFound = errors.New("announcement not found")
	ErrAnnouncementForbidden = errors.New("announcement forbidden")
)

type AnnouncementService struct {
	DB *gorm.DB
}

type AnnouncementInput struct {
	MerchantID uint64
	Title      string
	Content    string
	CoverURL   *string
	SortOrder  int
	Status     uint8
	PublishAt  *time.Time
	ExpireAt   *time.Time
}

type AnnouncementListFilter struct {
	MerchantID *uint64
	Status     *uint8
	PublicOnly bool
}

func (s *AnnouncementService) List(page, pageSize int, filter AnnouncementListFilter) ([]model.Announcement, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}

	q := query.NotDeleted(s.DB.Model(&model.Announcement{}))
	if filter.MerchantID != nil {
		q = q.Where("merchant_id = ?", *filter.MerchantID)
	}
	if filter.Status != nil {
		q = q.Where("status = ?", *filter.Status)
	}
	if filter.PublicOnly {
		now := time.Now()
		q = q.Where("status = ?", model.AnnouncementStatusPublished).
			Where("(publish_at IS NULL OR publish_at <= ?)", now).
			Where("(expire_at IS NULL OR expire_at >= ?)", now)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []model.Announcement
	if err := q.Order("sort_order ASC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *AnnouncementService) GetByID(id uint64, merchantID *uint64) (*model.Announcement, error) {
	var ann model.Announcement
	q := query.NotDeleted(s.DB).Where("id = ?", id)
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	}
	if err := q.First(&ann).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAnnouncementNotFound
		}
		return nil, err
	}
	return &ann, nil
}

func (s *AnnouncementService) Create(input AnnouncementInput) (*model.Announcement, error) {
	if err := validateAnnouncementInput(input); err != nil {
		return nil, err
	}
	ann := model.Announcement{
		MerchantID: input.MerchantID,
		Title:      strings.TrimSpace(input.Title),
		Content:    strings.TrimSpace(input.Content),
		CoverURL:   input.CoverURL,
		SortOrder:  input.SortOrder,
		Status:     input.Status,
		PublishAt:  input.PublishAt,
		ExpireAt:   input.ExpireAt,
	}
	if ann.Status == 0 {
		ann.Status = model.AnnouncementStatusPublished
	}
	if err := s.DB.Create(&ann).Error; err != nil {
		return nil, err
	}
	return &ann, nil
}

func (s *AnnouncementService) Update(id uint64, input AnnouncementInput, merchantID *uint64) (*model.Announcement, error) {
	ann, err := s.GetByID(id, merchantID)
	if err != nil {
		return nil, err
	}
	if err := validateAnnouncementInput(input); err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"title": strings.TrimSpace(input.Title), "content": strings.TrimSpace(input.Content),
		"cover_url": input.CoverURL, "sort_order": input.SortOrder, "status": input.Status,
		"publish_at": input.PublishAt, "expire_at": input.ExpireAt,
	}
	if merchantID == nil && input.MerchantID > 0 {
		updates["merchant_id"] = input.MerchantID
	}
	if err := s.DB.Model(ann).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id, merchantID)
}

func (s *AnnouncementService) Delete(id uint64, merchantID *uint64) error {
	ann, err := s.GetByID(id, merchantID)
	if err != nil {
		return err
	}
	return query.SoftDelete(s.DB, ann).Error
}

func validateAnnouncementInput(input AnnouncementInput) error {
	title := strings.TrimSpace(input.Title)
	content := strings.TrimSpace(input.Content)
	if title == "" || content == "" {
		return ErrInvalidProductArg
	}
	if utf8.RuneCountInString(title) > 128 {
		return ErrInvalidProductArg
	}
	if input.ExpireAt != nil && input.PublishAt != nil && input.ExpireAt.Before(*input.PublishAt) {
		return ErrInvalidProductArg
	}
	return nil
}
