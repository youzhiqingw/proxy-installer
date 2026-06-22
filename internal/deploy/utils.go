// Package deploy 负责部署脚本生成、配置文件构建和远程部署编排。
// 从 app.go 提取，不依赖 Wails runtime（通过 sshclient.EmitFn 回调解耦）。
package deploy

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/crypto/curve25519"

	"proxy-installer/internal/config"
	"proxy-installer/internal/util"
)

// ── 共享工具函数的向后兼容转发（实际定义在 internal/util）──────

// ShellQuote 将字符串用单引号包裹，内部单引号做 '\'' 转义
func ShellQuote(s string) string { return util.ShellQuote(s) }

// B64 对字符串做 base64 编码
func B64(s string) string { return util.B64(s) }

// B64JSON 将值序列化为 JSON 后做 base64 编码
func B64JSON(v any) string { return util.B64JSON(v) }

// ParseKeyValue 解析 key=value 格式的文本为 map
func ParseKeyValue(text string) map[string]string { return util.ParseKeyValue(text) }

// StripANSI 移除字符串中的 ANSI 转义序列
func StripANSI(s string) string { return util.StripANSI(s) }

// TrimForMessage 截断字符串到指定长度
func TrimForMessage(s string, max int) string { return util.TrimForMessage(s, max) }

// FlattenJSON 将嵌套 JSON 结构展平
func FlattenJSON(prefix string, value any, out map[string]string) { util.FlattenJSON(prefix, value, out) }

// FlattenAny 将任意值展平为 dot-separated key → string value 的 map
func FlattenAny(value any) map[string]string { return util.FlattenAny(value) }

// FirstValue 从 map 中按优先级取第一个非空值
func FirstValue(values map[string]string, keys ...string) string { return util.FirstValue(values, keys...) }

// CompactJoin 用 " / " 连接非空字符串
func CompactJoin(values ...string) string { return util.CompactJoin(values...) }

// SortedKeys 返回 map 的所有 key（排序后）
func SortedKeys(values map[string]string) []string { return util.SortedKeys(values) }

// ── 部署专用工具函数 ──────────────────────────────────────────

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
