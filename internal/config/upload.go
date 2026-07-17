package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type UploadConfig struct {
	Dir              string // 本地存储根目录
	MaxSizeMB        int
	URLPrefix        string // 对外访问前缀，如 /uploads
	PublicBase       string // 完整域名前缀，如 https://weixin.catmicloud.cn
	AvatarPublicBase string // 头像 URL 域名前缀，默认同 PublicBase
}

func loadUploadConfig(port string) UploadConfig {
	maxMB := 10
	if v := getEnv("UPLOAD_MAX_MB", "10"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxMB = n
		}
	}
	dir := getEnv("UPLOAD_DIR", "uploads")
	prefix := getEnv("UPLOAD_URL_PREFIX", "/uploads")
	base := getEnv("UPLOAD_PUBLIC_BASE", "")
	if base == "" {
		base = fmt.Sprintf("http://localhost:%s", port)
	}
	base = strings.TrimRight(base, "/")
	avatarBase := strings.TrimRight(getEnv("AVATAR_PUBLIC_BASE", ""), "/")
	if avatarBase == "" {
		avatarBase = base
	}
	prefix = "/" + strings.Trim(prefix, "/")
	return UploadConfig{
		Dir:              dir,
		MaxSizeMB:        maxMB,
		URLPrefix:        prefix,
		PublicBase:       base,
		AvatarPublicBase: avatarBase,
	}
}

func (u UploadConfig) PublicURL(relativePath string) string {
	rel := filepath.ToSlash(relativePath)
	return u.PublicBase + u.URLPrefix + "/" + strings.TrimPrefix(rel, "/")
}
