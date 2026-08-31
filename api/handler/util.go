package handler

import (
	"encoding/json"
	"strconv"
)

// jsonMarshal 序列化（用于 tags/platforms 数组 → JSON 字符串）。
func jsonMarshal(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// fmtSscanfID 解析路径参数为 uint64。
func fmtSscanfID(s string, out *uint64) (int, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	*out = n
	return 1, nil
}

// parseID 解析路径参数为 uint64（便捷版）。
func parseID(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}
