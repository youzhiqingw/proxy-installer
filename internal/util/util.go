// Package util 提供跨包的通用工具函数。
// 从 deploy/utils.go 提取，供 deploy、quality、singbox、speedtest 等包共享引用。
package util

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ShellQuote 将字符串用单引号包裹，内部单引号做 '\'' 转义，用于安全拼入 shell 命令
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// B64 对字符串做 base64 编码
func B64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// B64JSON 将值序列化为 JSON 后做 base64 编码
func B64JSON(v any) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return B64(string(data))
}

// ParseKeyValue 解析 key=value 格式的文本为 map
func ParseKeyValue(text string) map[string]string {
	kv := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 {
			kv[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	return kv
}

// StripANSI 移除字符串中的 ANSI 转义序列
func StripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inEsc {
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
				inEsc = false
			}
			continue
		}
		if ch == 0x1b {
			inEsc = true
			continue
		}
		b.WriteByte(ch)
	}
	return strings.TrimSpace(b.String())
}

// TrimForMessage 截断字符串到指定长度，用于日志/错误消息显示
func TrimForMessage(s string, max int) string {
	s = strings.TrimSpace(StripANSI(s))
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// FlattenJSON 将嵌套 JSON 结构展平为 dot-separated key → string value 的 map
func FlattenJSON(prefix string, value any, out map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			FlattenJSON(next, item, out)
		}
	case map[string]string:
		for key, item := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			out[next] = strings.TrimSpace(StripANSI(item))
		}
	case []any:
		for index, item := range typed {
			FlattenJSON(fmt.Sprintf("%s[%d]", prefix, index), item, out)
		}
	case string:
		out[prefix] = strings.TrimSpace(StripANSI(typed))
	case float64:
		out[prefix] = strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		out[prefix] = strconv.FormatBool(typed)
	case nil:
	default:
		out[prefix] = fmt.Sprint(typed)
	}
}

// FlattenAny 将任意值展平为 dot-separated key → string value 的 map（FlattenJSON 的便捷封装）
func FlattenAny(value any) map[string]string {
	flat := map[string]string{}
	FlattenJSON("", value, flat)
	return flat
}

// FirstValue 从 map 中按优先级取第一个非空值
func FirstValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

// CompactJoin 用 " / " 连接非空字符串
func CompactJoin(values ...string) string {
	var parts []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " / ")
}

// SortedKeys 返回 map 的所有 key（排序后）
func SortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// LooksLikeHTML 判断文本是否看起来像 HTML 响应（用于过滤非 API 结果）
func LooksLikeHTML(text string) bool {
	probe := strings.ToLower(strings.TrimSpace(text))
	if len(probe) > 256 {
		probe = probe[:256]
	}
	return strings.Contains(probe, "<!doctype html") || strings.Contains(probe, "<html") || strings.Contains(probe, "<head")
}
