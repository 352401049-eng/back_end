package service

import "encoding/json"

// toJSONColumn 将字符串切片编码为 MySQL JSON 列可接受的值（用于 Updates(map) 场景）。
func toJSONColumn(v []string) interface{} {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
