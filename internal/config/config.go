package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port   string
	DB     DBConfig
	JWT    JWTConfig
	WeChat WeChatConfig
	Backup BackupConfig
	Upload UploadConfig
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
		Backup: loadBackupConfig(),
	}
	cfg.Upload = loadUploadConfig(cfg.Port)

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
