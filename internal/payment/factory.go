package payment

import (
	"log"
	"strings"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/payment/wechatv3"

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
		v3cfg := wechatv3.Config{
			MchID:    cfg.Payment.WeChatMchID,
			SerialNo: cfg.Payment.WeChatSerialNo,
			CertPath: cfg.Payment.WeChatCertPath,
			KeyPath:  cfg.Payment.WeChatKeyPath,
			APIKey:   cfg.Payment.WeChatAPIKey,
		}
		client, err := wechatv3.NewClient(v3cfg)
		if err != nil {
			log.Printf("微信支付 V3 客户端初始化失败，降级为 Mock: %v", err)
			return &MockProvider{DB: db}
		}
		return &WeChatProvider{
			DB:        db,
			AppID:     cfg.WeChat.AppID,
			MchID:     cfg.Payment.WeChatMchID,
			APIKey:    cfg.Payment.WeChatAPIKey,
			NotifyURL: cfg.Payment.WeChatNotifyURL,
			Enabled:   cfg.Payment.WeChatEnabled,
			Client:    client,
		}
	default:
		return &MockProvider{DB: db}
	}
}
