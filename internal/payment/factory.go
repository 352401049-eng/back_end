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
		if cfg.Payment.WeChatEnabled {
			if certSerial, err := wechatv3.ReadCertSerialNo(cfg.Payment.WeChatCertPath); err != nil {
				log.Printf("[wechat] 读取商户证书序列号失败: %v", err)
			} else if certSerial != cfg.Payment.WeChatSerialNo {
				log.Printf("[wechat] 警告: WECHAT_PAY_SERIAL_NO=%s 与证书文件不一致（证书为 %s），请更新 .env", cfg.Payment.WeChatSerialNo, certSerial)
			}
			if err := client.VerifyAPIKey(); err != nil {
				log.Printf("[wechat] APIv3 密钥验证失败: %v", err)
			} else {
				log.Printf("[wechat] APIv3 密钥验证通过")
			}
		}
		wp := &WeChatProvider{
			DB:        db,
			AppID:     cfg.WeChat.AppID,
			MchID:     cfg.Payment.WeChatMchID,
			APIKey:    cfg.Payment.WeChatAPIKey,
			NotifyURL: cfg.Payment.WeChatNotifyURL,
			Enabled:   cfg.Payment.WeChatEnabled,
			Client:    client,
		}
		return wp
	default:
		return &MockProvider{DB: db}
	}
}
