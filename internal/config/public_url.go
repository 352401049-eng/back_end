package config

import "strings"

// ExpandPublicURL 将相对路径补全为带域名的完整 URL；已是 http(s) 则原样返回。
func ExpandPublicURL(base, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return base + raw
}

// NormalizeStoredURL 入库前去掉已知域名前缀，优先存相对路径。
func NormalizeStoredURL(base, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	base = strings.TrimRight(base, "/")
	if base != "" && strings.HasPrefix(raw, base) {
		path := strings.TrimPrefix(raw, base)
		if path == "" {
			return raw
		}
		return path
	}
	return raw
}
