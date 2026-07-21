package payment

import (
	"strings"

	"yujixinjiang/backend/internal/config"

	"gorm.io/gorm"
)

// NewProvider 按配置创建支付实现。默认 mock。
func NewProvider(cfg *config.Config, db *gorm.DB) Provider {
	name := "mock"
	if cfg != nil && cfg.Payment.Provider != "" {
		name = strings.ToLower(strings.TrimSpace(cfg.Payment.Provider))
	}
	switch name {
	case "wechat", "wx", "weixin":
		return &WeChatProvider{
			DB:        db,
			AppID:     cfg.WeChat.AppID,
			MchID:     cfg.Payment.WeChatMchID,
			APIKey:    cfg.Payment.WeChatAPIKey,
			NotifyURL: cfg.Payment.WeChatNotifyURL,
			Enabled:   cfg.Payment.WeChatEnabled,
		}
	default:
		return &MockProvider{DB: db}
	}
}
