package service

import (
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrRiderApplicationNotFound = errors.New("rider application not found")
	ErrRiderApplicationPending  = errors.New("pending rider application exists")
	ErrAlreadyRider             = errors.New("already rider")
	ErrRiderApplyForbidden        = errors.New("account type cannot apply rider")
	ErrApplicationNotPending    = errors.New("application not pending")
	ErrInvalidReviewStatus      = errors.New("invalid review status")
)

type RiderApplicationService struct {
	DB *gorm.DB
}

type ApplyRiderInput struct {
	RealName string
	IDCardNo *string
	Phone    string
}

type ReviewRiderInput struct {
	Status       uint8
	RejectReason *string
}

type RiderApplicationView struct {
	model.RiderApplication
	StatusText string `json:"status_text"`
}

func (s *RiderApplicationService) Apply(accountID uint64, input ApplyRiderInput) (*RiderApplicationView, error) {
	if input.RealName == "" || input.Phone == "" {
		return nil, ErrInvalidProductArg
	}

	var account model.Account
	if err := query.NotDeleted(s.DB).First(&account, accountID).Error; err != nil {
		return nil, err
	}
	if account.IsRider == 1 {
		return nil, ErrAlreadyRider
	}
	switch account.Type {
	case model.AccountTypeUser, model.AccountTypeMerchant, model.AccountTypeAdmin:
		// 普通用户、商家、管理员均可提交（管理员用于联调测试）
	default:
		return nil, ErrRiderApplyForbidden
	}

	var pendingCount int64
	if err := query.NotDeleted(s.DB.Model(&model.RiderApplication{})).
		Where("account_id = ? AND status = ?", accountID, model.RiderApplicationPending).
		Count(&pendingCount).Error; err != nil {
		return nil, err
	}
	if pendingCount > 0 {
		return nil, ErrRiderApplicationPending
	}

	app := model.RiderApplication{
		AccountID: accountID,
		RealName:  input.RealName,
		IDCardNo:  input.IDCardNo,
		Phone:     input.Phone,
		Status:    model.RiderApplicationPending,
	}
	if err := s.DB.Create(&app).Error; err != nil {
		return nil, fmt.Errorf("提交申请失败: %w", err)
	}
	return s.toView(&app), nil
}

func (s *RiderApplicationService) GetLatestByAccount(accountID uint64) (*RiderApplicationView, error) {
	var app model.RiderApplication
	err := query.NotDeleted(s.DB).Where("account_id = ?", accountID).Order("id DESC").First(&app).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRiderApplicationNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.toView(&app), nil
}

func (s *RiderApplicationService) List(page, pageSize int, status *uint8) ([]RiderApplicationView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.RiderApplication{}))
	if status != nil {
		q = q.Where("status = ?", *status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []model.RiderApplication
	if err := q.Preload("Account", "is_deleted = ?", model.NotDeleted).Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}

	views := make([]RiderApplicationView, 0, len(list))
	for i := range list {
		views = append(views, *s.toView(&list[i]))
	}
	return views, total, nil
}

func (s *RiderApplicationService) GetByID(id uint64) (*RiderApplicationView, error) {
	var app model.RiderApplication
	if err := query.NotDeleted(s.DB).Preload("Account", "is_deleted = ?", model.NotDeleted).First(&app, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRiderApplicationNotFound
		}
		return nil, err
	}
	return s.toView(&app), nil
}

func (s *RiderApplicationService) Review(id, reviewerID uint64, input ReviewRiderInput) (*RiderApplicationView, error) {
	if input.Status != model.RiderApplicationApproved && input.Status != model.RiderApplicationRejected {
		return nil, ErrInvalidReviewStatus
	}
	if input.Status == model.RiderApplicationRejected && (input.RejectReason == nil || *input.RejectReason == "") {
		return nil, ErrInvalidProductArg
	}

	var app model.RiderApplication
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx).First(&app, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRiderApplicationNotFound
			}
			return err
		}
		if app.Status != model.RiderApplicationPending {
			return ErrApplicationNotPending
		}

		now := time.Now()
		updates := map[string]interface{}{
			"status":      input.Status,
			"reviewer_id": reviewerID,
			"reviewed_at": now,
		}
		if input.Status == model.RiderApplicationRejected {
			updates["reject_reason"] = *input.RejectReason
		} else {
			updates["reject_reason"] = nil
		}
		if err := tx.Model(&app).Updates(updates).Error; err != nil {
			return err
		}

		if input.Status == model.RiderApplicationApproved {
			result := query.NotDeleted(tx.Model(&model.Account{})).
				Where("id = ? AND is_rider = ?", app.AccountID, 0).
				Update("is_rider", 1)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return ErrAlreadyRider
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *RiderApplicationService) toView(app *model.RiderApplication) *RiderApplicationView {
	return &RiderApplicationView{
		RiderApplication: *app,
		StatusText:       model.RiderApplicationStatusText(app.Status),
	}
}
