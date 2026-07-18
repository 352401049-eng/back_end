package service

import (
	"errors"
	"fmt"
	"strings"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrMerchantNotFound   = errors.New("merchant not found")
	ErrPhoneExists        = errors.New("phone already exists")
	ErrOpenIDExists       = errors.New("openid already exists")
	ErrCategoryNotFound   = errors.New("category not found")
	ErrCategoryForbidden  = errors.New("category forbidden")
	ErrProductNotFound    = errors.New("product not found")
	ErrProductForbidden   = errors.New("product forbidden")
	ErrInvalidProductArg  = errors.New("invalid product argument")
	ErrInvalidMerchantArg = errors.New("invalid merchant argument")
)

type MerchantService struct {
	DB *gorm.DB
}

type CreateMerchantInput struct {
	Phone        string
	OpenID       string
	ShopName     string
	ShopLogo     *string
	Images       []string
	ContactPhone *string
	Address      *string
	Nickname     *string
}

type MerchantListFilter struct {
	Status  *uint8
	Keyword string
}

// MerchantCoordinateUpdate 店铺坐标 PATCH：Clear 清空；否则 Lat/Lng 成对写入。
type MerchantCoordinateUpdate struct {
	Update bool
	Clear  bool
	Lat    float64
	Lng    float64
}

// UpdateMerchantInput 选择性更新商家资料。
type UpdateMerchantInput struct {
	ShopName         *string
	ContactPhone     *string
	Address          *string
	ShopLogo         *string
	Images           *[]string
	Coordinates      *MerchantCoordinateUpdate
	AllowReservation *uint8
}

func (in UpdateMerchantInput) hasField() bool {
	return in.ShopName != nil || in.ContactPhone != nil || in.Address != nil ||
		in.ShopLogo != nil || in.Images != nil || in.Coordinates != nil ||
		in.AllowReservation != nil
}

func (s *MerchantService) Create(input CreateMerchantInput) (*model.MerchantProfile, error) {
	if input.ShopName == "" {
		return nil, fmt.Errorf("店铺名称不能为空")
	}

	if input.Phone != "" {
		var count int64
		if err := query.NotDeleted(s.DB.Model(&model.Account{})).Where("phone = ?", input.Phone).Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, ErrPhoneExists
		}
	}

	if input.OpenID != "" {
		var count int64
		if err := query.NotDeleted(s.DB.Model(&model.Account{})).Where("openid = ?", input.OpenID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, ErrOpenIDExists
		}
	}

	nickname := input.ShopName
	if input.Nickname != nil && *input.Nickname != "" {
		nickname = *input.Nickname
	}

	var profile model.MerchantProfile
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		account := model.Account{
			Type:     model.AccountTypeMerchant,
			Nickname: &nickname,
			Status:   1,
		}
		if input.Phone != "" {
			phone := input.Phone
			account.Phone = &phone
		}
		if input.OpenID != "" {
			oid := input.OpenID
			account.OpenID = &oid
		}
		if err := tx.Create(&account).Error; err != nil {
			return fmt.Errorf("创建商家账号失败: %w", err)
		}

		profile = model.MerchantProfile{
			AccountID:    account.ID,
			ShopName:     input.ShopName,
			ShopLogo:     resolveShopLogo(input.ShopLogo, input.Images),
			Images:       input.Images,
			ContactPhone: input.ContactPhone,
			Address:      input.Address,
			Status:       model.MerchantStatusOpen,
		}
		if err := tx.Create(&profile).Error; err != nil {
			return fmt.Errorf("创建商家资料失败: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return s.GetByID(profile.ID)
}

func (s *MerchantService) GetByID(id uint64) (*model.MerchantProfile, error) {
	var profile model.MerchantProfile
	if err := query.NotDeleted(s.DB).Preload("Account", "is_deleted = ?", model.NotDeleted).First(&profile, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMerchantNotFound
		}
		return nil, err
	}
	return &profile, nil
}

func (s *MerchantService) GetByAccountID(accountID uint64) (*model.MerchantProfile, error) {
	var profile model.MerchantProfile
	if err := query.NotDeleted(s.DB).Where("account_id = ?", accountID).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMerchantNotFound
		}
		return nil, err
	}
	return &profile, nil
}

func (s *MerchantService) List(page, pageSize int, filter MerchantListFilter) ([]model.MerchantProfile, int64, error) {
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

	q := query.NotDeleted(s.DB.Model(&model.MerchantProfile{}))
	if filter.Status != nil {
		q = q.Where("status = ?", *filter.Status)
	}
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		q = q.Where("shop_name LIKE ? OR contact_phone LIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []model.MerchantProfile
	if err := q.Preload("Account", "is_deleted = ?", model.NotDeleted).Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *MerchantService) UpdateStatus(id uint64, status uint8) (*model.MerchantProfile, error) {
	if status != model.MerchantStatusClosed && status != model.MerchantStatusOpen {
		return nil, ErrInvalidProductArg
	}
	result := query.NotDeleted(s.DB.Model(&model.MerchantProfile{})).Where("id = ?", id).Update("status", status)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrMerchantNotFound
	}
	return s.GetByID(id)
}

func (s *MerchantService) UpdateProfile(id uint64, input UpdateMerchantInput) (*model.MerchantProfile, error) {
	if !input.hasField() {
		return nil, ErrInvalidMerchantArg
	}
	profile, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{}
	var shopName string
	if input.ShopName != nil {
		shopName = strings.TrimSpace(*input.ShopName)
		if shopName == "" {
			return nil, ErrInvalidMerchantArg
		}
		updates["shop_name"] = shopName
	}
	if input.ContactPhone != nil {
		phone := strings.TrimSpace(*input.ContactPhone)
		if phone == "" {
			updates["contact_phone"] = nil
		} else {
			updates["contact_phone"] = phone
		}
	}
	if input.Address != nil {
		addr := strings.TrimSpace(*input.Address)
		if addr == "" {
			updates["address"] = nil
		} else {
			updates["address"] = addr
		}
	}
	if input.Images != nil {
		if len(*input.Images) == 0 {
			return nil, ErrInvalidMerchantArg
		}
		updates["images"] = toJSONColumn(*input.Images)
		updates["shop_logo"] = resolveShopLogo(input.ShopLogo, *input.Images)
	} else if input.ShopLogo != nil {
		logo := strings.TrimSpace(*input.ShopLogo)
		if logo == "" {
			updates["shop_logo"] = nil
		} else {
			updates["shop_logo"] = logo
		}
	}
	if input.Coordinates != nil && input.Coordinates.Update {
		if input.Coordinates.Clear {
			updates["latitude"] = nil
			updates["longitude"] = nil
		} else {
			if err := validateMerchantCoordinates(input.Coordinates.Lat, input.Coordinates.Lng); err != nil {
				return nil, err
			}
			updates["latitude"] = input.Coordinates.Lat
			updates["longitude"] = input.Coordinates.Lng
		}
	}
	if input.AllowReservation != nil {
		updates["allow_reservation"] = normalizeAllowReservation(*input.AllowReservation)
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.MerchantProfile{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新商家资料失败: %w", err)
		}
		if input.ShopName != nil {
			nickname := shopName
			if err := tx.Model(&model.Account{}).Where("id = ?", profile.AccountID).Update("nickname", nickname).Error; err != nil {
				return fmt.Errorf("同步账号昵称失败: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *MerchantService) UpdateImages(id uint64, images []string, shopLogo *string) (*model.MerchantProfile, error) {
	if len(images) == 0 {
		return nil, ErrInvalidMerchantArg
	}
	if _, err := s.GetByID(id); err != nil {
		return nil, err
	}
	logo := resolveShopLogo(shopLogo, images)
	if err := s.DB.Model(&model.MerchantProfile{}).Where("id = ?", id).Updates(map[string]interface{}{
		"images": toJSONColumn(images), "shop_logo": logo,
	}).Error; err != nil {
		return nil, fmt.Errorf("更新店铺图片失败: %w", err)
	}
	return s.GetByID(id)
}

func resolveShopLogo(logo *string, images []string) *string {
	if logo != nil && *logo != "" {
		return logo
	}
	if len(images) > 0 {
		s := images[0]
		return &s
	}
	return nil
}

// GetOpenByID 获取营业中的商家（用户端）。
func (s *MerchantService) GetOpenByID(id uint64) (*model.MerchantProfile, error) {
	var profile model.MerchantProfile
	if err := query.NotDeleted(s.DB).Where("id = ? AND status = ?", id, model.MerchantStatusOpen).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMerchantNotFound
		}
		return nil, err
	}
	return &profile, nil
}

// ListOpen 营业中商家列表（用户端，不返回账号敏感信息）。
func (s *MerchantService) ListOpen(page, pageSize int, keyword string, reservationOnly bool) ([]model.MerchantProfile, int64, error) {
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

	q := query.NotDeleted(s.DB.Model(&model.MerchantProfile{})).Where("status = ?", model.MerchantStatusOpen)
	if reservationOnly {
		q = q.Where("allow_reservation = 1")
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("shop_name LIKE ? OR address LIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []model.MerchantProfile
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func normalizeAllowReservation(v uint8) uint8 {
	if v == 0 {
		return 0
	}
	return 1
}

func validateMerchantCoordinates(lat, lng float64) error {
	if lat < -90 || lat > 90 {
		return fmt.Errorf("%w: latitude 须在 -90~90 之间", ErrInvalidMerchantArg)
	}
	if lng < -180 || lng > 180 {
		return fmt.Errorf("%w: longitude 须在 -180~180 之间", ErrInvalidMerchantArg)
	}
	return nil
}
