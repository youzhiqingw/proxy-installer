// Package deploy 负责部署脚本生成、配置文件构建和远程部署编排。
// 从 app.go 提取，不依赖 Wails runtime（通过 sshclient.EmitFn 回调解耦）。
package deploy

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/crypto/curve25519"

	"proxy-installer/internal/config"
)

// ── 共享工具函数（供 speedtest / quality / app 等包引用）──────

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

// PortOrDefault 从端口 map 中获取指定协议的端口，不存在则返回默认值
func PortOrDefault(ports map[string]int, key string, def int) int {
	if ports != nil && ports[key] > 0 {
		return ports[key]
	}
	return def
}

// PublicPortOrDefault 从 DeployConfig 的公网端口 map 获取端口，回退到内网端口 map
func PublicPortOrDefault(cfg config.DeployConfig, key string, def int) int {
	if cfg.PublicPorts != nil && cfg.PublicPorts[key] > 0 {
		return cfg.PublicPorts[key]
	}
	return PortOrDefault(cfg.Ports, key, def)
}

// PublicWebPortOrDefault 获取公网 Web 端口，依次回退到 PublicWebPort → WebPort → 默认值
func PublicWebPortOrDefault(cfg config.DeployConfig) int {
	if cfg.PublicWebPort > 0 {
		return cfg.PublicWebPort
	}
	if cfg.WebPort > 0 {
		return cfg.WebPort
	}
	return config.DefaultWebPort
}

// SelectedPorts 返回已选协议对应的内网端口列表
func SelectedPorts(selected []string, ports map[string]int) []int {
	var out []int
	defaults := ProtocolDefaults()
	for _, id := range selected {
		def, ok := defaults[id]
		if !ok {
			continue
		}
		out = append(out, PortOrDefault(ports, id, def))
	}
	return out
}

// ProtocolDefaults 返回协议名到默认端口的映射
func ProtocolDefaults() map[string]int {
	return config.ProtocolDefaultPorts
}

// FilterSupportedProtocols 过滤出已支持的协议（去重、保序）
func FilterSupportedProtocols(selected []string) []string {
	defaults := ProtocolDefaults()
	seen := map[string]bool{}
	var out []string
	for _, id := range selected {
		if _, ok := defaults[id]; ok && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

func intsToShell(values []int) string {
	var parts []string
	for _, v := range values {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, " ")
}

// SafeToken 过滤 token 字符串，只保留字母数字和下划线/连字符，最长 64 字符
func SafeToken(s string) string {
	out := ""
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			if len(out) >= 64 {
				break
			}
			out += string(r)
		}
	}
	if out == "" {
		return config.DefaultToken
	}
	return out
}

// SafeName 过滤名称字符串，替换非法字符为 '-'，最长 64 字符
func SafeName(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	out := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			return r
		}
		return '-'
	}, s)
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// SafeDomain 过滤域名字符串，移除非法字符，最长 253 字符
func SafeDomain(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	out := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			return r
		}
		return -1
	}, s)
	if len(out) > 253 {
		out = out[:253]
	}
	return out
}

func stableUUID(seed string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] & 0x3F) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func urlEsc(s string) string {
	replacer := strings.NewReplacer(" ", "%20", "#", "%23", ":", "%3A", "@", "%40", "/", "%2F", "?", "%3F", "&", "%26", "=", "%3D")
	return replacer.Replace(s)
}

// RealityKeys 生成 Reality 协议的 private/public/shortID 密钥对（每次随机生成）
func RealityKeys(seed string) (string, string, string) {
	privateBytes := make([]byte, 32)
	if _, err := rand.Read(privateBytes); err != nil {
		return "", "", ""
	}
	publicBytes, err := curve25519.X25519(privateBytes, curve25519.Basepoint)
	if err != nil {
		return "", "", ""
	}
	shortBytes := make([]byte, 4)
	if _, err := rand.Read(shortBytes); err != nil {
		return "", "", ""
	}
	return base64.RawURLEncoding.EncodeToString(privateBytes), base64.RawURLEncoding.EncodeToString(publicBytes), hex.EncodeToString(shortBytes)
}

// ProtocolLabel 返回协议的显示名称
func ProtocolLabel(id string) string {
	switch id {
	case "vless-reality":
		return "VLESS Reality"
	case "hy2":
		return "Hysteria2"
	case "tuic":
		return "TUIC"
	case "trojan":
		return "Trojan"
	case "ss":
		return "Shadowsocks"
	case "vmess":
		return "VMess"
	default:
		return id
	}
}

// UsesUDPProtocol 判断所选协议中是否包含 UDP 协议（HY2/TUIC）
func UsesUDPProtocol(selected []string) bool {
	for _, id := range selected {
		if id == "hy2" || id == "tuic" {
			return true
		}
	}
	return false
}

// NormalizeHostLiteral 清理主机地址，去除协议前缀和方括号
func NormalizeHostLiteral(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		if parsed, err := url.Parse(host); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host
}

// FormatHostForURI 格式化主机地址用于 URI（IPv6 加方括号）
func FormatHostForURI(host string) string {
	host = NormalizeHostLiteral(host)
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

// FormatHostForURL 格式化主机地址用于 URL（IPv6 加方括号）
func FormatHostForURL(host string) string {
	host = NormalizeHostLiteral(host)
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
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
