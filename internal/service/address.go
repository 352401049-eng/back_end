package service

import (
	"errors"
	"fmt"
	"strings"

	"yujixinjiang/backend/internal/geo"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrAddressNotFound = errors.New("address not found")
)

type AddressService struct {
	DB *gorm.DB
}

type AddressLocationInput struct {
	Update bool
	Clear  bool
	Lat    float64
	Lng    float64
}

type AddressInput struct {
	ContactName  string
	ContactPhone string
	Province     string
	City         string
	District     string
	Detail       string
	IsDefault    uint8
	Location     *AddressLocationInput
	LocationName *string
	LocationNameSet bool
}

type AddressPatchInput struct {
	ContactName  *string
	ContactPhone *string
	Province     *string
	City         *string
	District     *string
	Detail       *string
	IsDefault    *uint8
	Location     *AddressLocationInput
	LocationName *string
	LocationNameSet bool
}

func (s *AddressService) List(accountID uint64) ([]model.UserAddress, error) {
	var list []model.UserAddress
	err := query.NotDeleted(s.DB).Where("account_id = ?", accountID).
		Order("is_default DESC, id DESC").Find(&list).Error
	return list, err
}

func (s *AddressService) Get(accountID, id uint64) (*model.UserAddress, error) {
	var addr model.UserAddress
	err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", id, accountID).First(&addr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAddressNotFound
	}
	if err != nil {
		return nil, err
	}
	return &addr, nil
}

func (s *AddressService) Create(accountID uint64, input AddressInput) (*model.UserAddress, error) {
	if err := validateAddressInput(input); err != nil {
		return nil, err
	}
	lat, lng, locName, err := resolveAddressLocation(input.Location, input.LocationNameSet, input.LocationName)
	if err != nil {
		return nil, err
	}
	var addr model.UserAddress
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if input.IsDefault == 1 {
			if err := s.clearDefault(tx, accountID, 0); err != nil {
				return err
			}
		} else {
			var count int64
			if err := query.NotDeleted(tx.Model(&model.UserAddress{})).
				Where("account_id = ?", accountID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				input.IsDefault = 1
			}
		}
		addr = model.UserAddress{
			AccountID:    accountID,
			ContactName:  input.ContactName,
			ContactPhone: input.ContactPhone,
			Province:     input.Province,
			City:         input.City,
			District:     input.District,
			Detail:       input.Detail,
			Latitude:     lat,
			Longitude:    lng,
			LocationName: locName,
			IsDefault:    input.IsDefault,
		}
		return tx.Create(&addr).Error
	})
	if err != nil {
		return nil, fmt.Errorf("创建地址失败: %w", err)
	}
	return &addr, nil
}

func (s *AddressService) Update(accountID, id uint64, input AddressInput) (*model.UserAddress, error) {
	if err := validateAddressInput(input); err != nil {
		return nil, err
	}
	if _, err := s.Get(accountID, id); err != nil {
		return nil, err
	}
	lat, lng, locName, err := resolveAddressLocation(input.Location, input.LocationNameSet, input.LocationName)
	if err != nil {
		return nil, err
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if input.IsDefault == 1 {
			if err := s.clearDefault(tx, accountID, id); err != nil {
				return err
			}
		}
		updates := map[string]interface{}{
			"contact_name":  input.ContactName,
			"contact_phone": input.ContactPhone,
			"province":      input.Province,
			"city":          input.City,
			"district":      input.District,
			"detail":        input.Detail,
			"is_default":    input.IsDefault,
		}
		if input.Location != nil {
			if input.Location.Clear {
				updates["latitude"] = nil
				updates["longitude"] = nil
			} else if input.Location.Update {
				updates["latitude"] = lat
				updates["longitude"] = lng
			}
		}
		if input.LocationNameSet {
			updates["location_name"] = locName
		}
		return query.NotDeleted(tx.Model(&model.UserAddress{})).
			Where("id = ? AND account_id = ?", id, accountID).
			Updates(updates).Error
	})
	if err != nil {
		return nil, fmt.Errorf("更新地址失败: %w", err)
	}
	return s.Get(accountID, id)
}

func (s *AddressService) Patch(accountID, id uint64, input AddressPatchInput) (*model.UserAddress, error) {
	current, err := s.Get(accountID, id)
	if err != nil {
		return nil, err
	}
	merged := AddressInput{
		ContactName:  current.ContactName,
		ContactPhone: current.ContactPhone,
		Province:     current.Province,
		City:         current.City,
		District:     current.District,
		Detail:       current.Detail,
		IsDefault:    current.IsDefault,
	}
	if input.ContactName != nil {
		merged.ContactName = strings.TrimSpace(*input.ContactName)
	}
	if input.ContactPhone != nil {
		merged.ContactPhone = strings.TrimSpace(*input.ContactPhone)
	}
	if input.Province != nil {
		merged.Province = strings.TrimSpace(*input.Province)
	}
	if input.City != nil {
		merged.City = strings.TrimSpace(*input.City)
	}
	if input.District != nil {
		merged.District = strings.TrimSpace(*input.District)
	}
	if input.Detail != nil {
		merged.Detail = strings.TrimSpace(*input.Detail)
	}
	if input.IsDefault != nil {
		merged.IsDefault = *input.IsDefault
	}
	if input.Location != nil {
		merged.Location = input.Location
	}
	if input.LocationNameSet {
		merged.LocationNameSet = true
		merged.LocationName = input.LocationName
	}
	return s.Update(accountID, id, merged)
}

func (s *AddressService) Delete(accountID, id uint64) error {
	addr, err := s.Get(accountID, id)
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.SoftDelete(tx, &model.UserAddress{}, "id = ? AND account_id = ?", id, accountID).Error; err != nil {
			return err
		}
		if addr.IsDefault == 1 {
			var next model.UserAddress
			err := query.NotDeleted(tx).Where("account_id = ?", accountID).
				Order("id DESC").First(&next).Error
			if err == nil {
				return tx.Model(&model.UserAddress{}).Where("id = ?", next.ID).Update("is_default", 1).Error
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		return nil
	})
}

func (s *AddressService) SetDefault(accountID, id uint64) (*model.UserAddress, error) {
	if _, err := s.Get(accountID, id); err != nil {
		return nil, err
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.clearDefault(tx, accountID, id); err != nil {
			return err
		}
		return tx.Model(&model.UserAddress{}).Where("id = ?", id).Update("is_default", 1).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Get(accountID, id)
}

func (s *AddressService) clearDefault(tx *gorm.DB, accountID, exceptID uint64) error {
	q := query.NotDeleted(tx.Model(&model.UserAddress{})).Where("account_id = ? AND is_default = 1", accountID)
	if exceptID > 0 {
		q = q.Where("id <> ?", exceptID)
	}
	return q.Update("is_default", 0).Error
}

func validateAddressInput(input AddressInput) error {
	if input.ContactName == "" || input.ContactPhone == "" ||
		input.Province == "" || input.City == "" || input.District == "" || input.Detail == "" {
		return ErrInvalidProductArg
	}
	return nil
}

func resolveAddressLocation(loc *AddressLocationInput, locationNameSet bool, locationName *string) (*float64, *float64, *string, error) {
	var lat, lng *float64
	if loc != nil {
		if loc.Clear {
			lat, lng = nil, nil
		} else if loc.Update {
			if err := geo.ValidateCoordinate(loc.Lat, loc.Lng); err != nil {
				return nil, nil, nil, fmt.Errorf("%w: %s", ErrInvalidProductArg, err.Error())
			}
			latVal, lngVal := loc.Lat, loc.Lng
			lat, lng = &latVal, &lngVal
		}
	}
	var locName *string
	if locationNameSet {
		if locationName == nil || strings.TrimSpace(*locationName) == "" {
			locName = nil
		} else {
			name := strings.TrimSpace(*locationName)
			locName = &name
		}
	}
	return lat, lng, locName, nil
}

func AddressSnapshotFromUserAddress(addr *model.UserAddress) *model.AddressSnapshot {
	if addr == nil {
		return nil
	}
	return &model.AddressSnapshot{
		ContactName:  addr.ContactName,
		ContactPhone: addr.ContactPhone,
		Province:     addr.Province,
		City:         addr.City,
		District:     addr.District,
		Detail:       addr.Detail,
		Latitude:     addr.Latitude,
		Longitude:    addr.Longitude,
		LocationName: addr.LocationName,
	}
}
