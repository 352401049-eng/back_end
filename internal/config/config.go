package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port    string
	DB      DBConfig
	JWT     JWTConfig
	WeChat  WeChatConfig
	Payment PaymentConfig
	Backup  BackupConfig
	Upload  UploadConfig
	Map     MapConfig
}

// PaymentConfig 支付渠道。Provider=mock|wechat；wechat 需另配商户参数。
type PaymentConfig struct {
	Provider         string
	WeChatEnabled    bool
	WeChatMchID      string
	WeChatAPIKey     string
	WeChatNotifyURL  string
	PayTimeoutMinutes int // 待支付订单超时分钟数，超时未支付则关单回滚
}

type BackupConfig struct {
	Enabled    bool
	Dir        string
	Interval   time.Duration
	RetainDays int
	Compress   bool
}

type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

type JWTConfig struct {
	Secret string
}

type WeChatConfig struct {
	AppID  string
	Secret string
}

// MapConfig 地图服务配置。TencentKey 用于腾讯位置服务 POI 检索（配送范围地标）。
type MapConfig struct {
	TencentKey string
}

func (d DBConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		d.User, d.Password, d.Host, d.Port, d.Name,
	)
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port: getEnv("PORT", "8080"),
		DB: DBConfig{
			Host:     getEnv("DB_HOST", "127.0.0.1"),
			Port:     getEnv("DB_PORT", "3306"),
			User:     getEnv("DB_USER", "root"),
			Password: getEnv("DB_PASSWORD", ""),
			Name:     getEnv("DB_NAME", "yujixinjiang"),
		},
		JWT: JWTConfig{
			Secret: getEnv("JWT_SECRET", "dev-secret-change-me"),
		},
		WeChat: WeChatConfig{
			AppID:  getEnv("WECHAT_APPID", ""),
			Secret: getEnv("WECHAT_SECRET", ""),
		},
		Payment: PaymentConfig{
			Provider:         getEnv("PAYMENT_PROVIDER", "mock"),
			WeChatEnabled:    getEnv("WECHAT_PAY_ENABLED", "false") == "true",
			WeChatMchID:      getEnv("WECHAT_MCH_ID", ""),
			WeChatAPIKey:     getEnv("WECHAT_PAY_API_KEY", ""),
			WeChatNotifyURL:  getEnv("WECHAT_PAY_NOTIFY_URL", ""),
			PayTimeoutMinutes: loadPayTimeoutMinutes(),
		},
		Backup: loadBackupConfig(),
	}
	cfg.Upload = loadUploadConfig(cfg.Port)
	cfg.Map = MapConfig{
		TencentKey: getEnv("TENCENT_MAP_KEY", ""),
	}

	return cfg, nil
}

func loadBackupConfig() BackupConfig {
	interval, err := time.ParseDuration(getEnv("BACKUP_INTERVAL", "24h"))
	if err != nil || interval < time.Minute {
		interval = 24 * time.Hour
	}
	retain, _ := strconv.Atoi(getEnv("BACKUP_RETAIN_DAYS", "7"))
	return BackupConfig{
		Enabled:    getEnv("BACKUP_ENABLED", "false") == "true",
		Dir:        getEnv("BACKUP_DIR", "backups"),
		Interval:   interval,
		RetainDays: retain,
		Compress:   getEnv("BACKUP_COMPRESS", "true") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadPayTimeoutMinutes 读取待支付订单超时分钟数，默认 15，最小 1。
func loadPayTimeoutMinutes() int {
	v, err := strconv.Atoi(getEnv("PAY_TIMEOUT_MINUTES", "15"))
	if err != nil || v < 1 {
		return 15
	}
	return v
}
