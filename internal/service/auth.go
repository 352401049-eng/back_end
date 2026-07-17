package service

import (
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/wechat"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

var (
	ErrAccountDisabled     = errors.New("account disabled")
	ErrWeChatNotConfigured = errors.New("wechat not configured")
	ErrPhoneAlreadyBound   = errors.New("phone already bound")
)

// AuthService 登录与 token 签发。
type AuthService struct {
	DB               *gorm.DB
	JWTSecret        string
	WeChat           *wechat.Client
	AvatarPublicBase string
}

// LoginResult 登录成功返回。
type LoginResult struct {
	Token   string
	Account model.Account
	IsNew   bool
}

// LoginByWeChatCode 微信 code 登录（用户/商家/管理员/骑手统一入口）。
func (s *AuthService) LoginByWeChatCode(code string) (*LoginResult, error) {
	if s.WeChat == nil {
		return nil, ErrWeChatNotConfigured
	}

	session, err := s.WeChat.Code2Session(code)
	if err != nil {
		return nil, err
	}

	var unionid *string
	if session.UnionID != "" {
		unionid = &session.UnionID
	}

	return s.loginByOpenID(session.OpenID, unionid)
}

func (s *AuthService) loginByOpenID(openid string, unionid *string) (*LoginResult, error) {
	var account model.Account
	isNew := false

	result := query.NotDeleted(s.DB).Where("openid = ?", openid).First(&account)
	if result.Error == gorm.ErrRecordNotFound {
		oid := openid
		nick := "微信用户"
		now := time.Now()
		account = model.Account{
			Type:        model.AccountTypeUser,
			OpenID:      &oid,
			Nickname:    &nick,
			Status:      1,
			LastLoginAt: &now,
		}
		if unionid != nil && *unionid != "" {
			account.UnionID = unionid
		}
		if err := s.DB.Create(&account).Error; err != nil {
			return nil, fmt.Errorf("创建用户失败: %w", err)
		}
		isNew = true
	} else if result.Error != nil {
		return nil, fmt.Errorf("查询用户失败: %w", result.Error)
	} else {
		if account.Status != 1 {
			return nil, ErrAccountDisabled
		}

		now := time.Now()
		account.LastLoginAt = &now
		updates := map[string]interface{}{"last_login_at": now}
		if unionid != nil && *unionid != "" && (account.UnionID == nil || *account.UnionID == "") {
			updates["unionid"] = *unionid
			account.UnionID = unionid
		}
		if err := s.DB.Model(&account).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("更新登录信息失败: %w", err)
		}
	}

	token, err := s.SignToken(account.ID, account.Type)
	if err != nil {
		return nil, fmt.Errorf("生成 token 失败: %w", err)
	}

	return &LoginResult{
		Token:   token,
		Account: *s.withPublicAvatar(&account),
		IsNew:   isNew,
	}, nil
}

// SignToken 签发 JWT。
func (s *AuthService) SignToken(accountID uint64, accountType uint8) (string, error) {
	claims := jwt.MapClaims{
		"account_id":   accountID,
		"account_type": accountType,
		"exp":          time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.JWTSecret))
}

// GetAccount 按 ID 查询账号。
func (s *AuthService) GetAccount(accountID uint64) (*model.Account, error) {
	var account model.Account
	if err := query.NotDeleted(s.DB).First(&account, accountID).Error; err != nil {
		return nil, err
	}
	return s.withPublicAvatar(&account), nil
}

// BindWeChatPhone 绑定微信授权手机号。
func (s *AuthService) BindWeChatPhone(accountID uint64, code string) (*model.Account, error) {
	if s.WeChat == nil {
		return nil, ErrWeChatNotConfigured
	}

	phoneInfo, err := s.WeChat.GetPhoneNumber(code)
	if err != nil {
		return nil, err
	}

	phone := phoneInfo.PurePhoneNumber
	var existing model.Account
	err = query.NotDeleted(s.DB).Where("phone = ? AND id <> ?", phone, accountID).First(&existing).Error
	if err == nil {
		return nil, ErrPhoneAlreadyBound
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("查询手机号失败: %w", err)
	}

	if err := query.NotDeleted(s.DB.Model(&model.Account{})).Where("id = ?", accountID).Update("phone", phone).Error; err != nil {
		return nil, fmt.Errorf("绑定手机号失败: %w", err)
	}

	return s.GetAccount(accountID)
}

// UpdateAvatar 更新用户头像。
func (s *AuthService) UpdateAvatar(accountID uint64, avatarURL string) (*model.Account, error) {
	stored := config.NormalizeStoredURL(s.AvatarPublicBase, avatarURL)
	if err := query.NotDeleted(s.DB.Model(&model.Account{})).Where("id = ?", accountID).Update("avatar_url", stored).Error; err != nil {
		return nil, fmt.Errorf("更新头像失败: %w", err)
	}
	return s.GetAccount(accountID)
}

// UpdateNickname 更新昵称。
func (s *AuthService) UpdateNickname(accountID uint64, nickname string) (*model.Account, error) {
	if err := query.NotDeleted(s.DB.Model(&model.Account{})).Where("id = ?", accountID).Update("nickname", nickname).Error; err != nil {
		return nil, fmt.Errorf("更新昵称失败: %w", err)
	}
	return s.GetAccount(accountID)
}

func (s *AuthService) withPublicAvatar(account *model.Account) *model.Account {
	if account == nil || account.AvatarURL == nil || *account.AvatarURL == "" || s.AvatarPublicBase == "" {
		return account
	}
	expanded := config.ExpandPublicURL(s.AvatarPublicBase, *account.AvatarURL)
	account.AvatarURL = &expanded
	return account
}
