// Package config 定义应用共享的类型和常量。
// 从 app.go 提取，作为 internal 各子包的公共数据层。
package config

import "encoding/json"

// ── 全局默认值常量 ──────────────────────────────────────────

const (
	// DefaultToken 是订阅令牌的默认值
	DefaultToken = "starter2026"
	// DefaultSNI 是 TLS 握手的默认 SNI 域名
	DefaultSNI = "www.bing.com"
	// DefaultSubRule 是订阅路由规则的默认模板
	DefaultSubRule = "/sub/{token}/{client}"
	// DefaultWebPort 是 Web 服务的默认监听端口
	DefaultWebPort = 8080
	// DefaultNodeName 是节点名称的默认回退值
	DefaultNodeName = "starter-node"
	// PasswordPrefix 和 PasswordSuffix 用于拼接协议密码: Pwd_{token}_2026
	PasswordPrefix = "Pwd_"
	PasswordSuffix = "_2026"
)

// ProtocolDefaultPorts 定义各协议的默认端口号
var ProtocolDefaultPorts = map[string]int{
	"vless-reality": 443,
	"hy2":           8443,
	"tuic":          8444,
	"trojan":        8445,
	"ss":            8388,
	"vmess":         2083,
}

// ── 核心类型 ─────────────────────────────────────────────

// SSHProfile 存储 SSH 连接凭据
type SSHProfile struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Host              string `json:"host"`
	User              string `json:"user"`
	Username          string `json:"username"`
	Port              int    `json:"port"`
	Password          []byte `json:"-"`                          // 内存安全：[]byte 可 zero out
	PasswordEncrypted string `json:"password_encrypted,omitempty"` // 加密存储字段
}

// MarshalJSON 自定义序列化，确保 Password 不直接暴露
func (p SSHProfile) MarshalJSON() ([]byte, error) {
	type Alias struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		Host              string `json:"host"`
		User              string `json:"user"`
		Username          string `json:"username"`
		Port              int    `json:"port"`
		Password          string `json:"password"`
		PasswordEncrypted string `json:"password_encrypted,omitempty"`
	}
	return json.Marshal(Alias{
		ID:                p.ID,
		Name:              p.Name,
		Host:              p.Host,
		User:              p.User,
		Username:          p.Username,
		Port:              p.Port,
		Password:          string(p.Password),
		PasswordEncrypted: p.PasswordEncrypted,
	})
}

// UnmarshalJSON 自定义反序列化，兼容旧版 string 格式 Password
func (p *SSHProfile) UnmarshalJSON(data []byte) error {
	type Alias struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		Host              string `json:"host"`
		User              string `json:"user"`
		Username          string `json:"username"`
		Port              int    `json:"port"`
		Password          string `json:"password"`
		PasswordEncrypted string `json:"password_encrypted,omitempty"`
	}
	var a Alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	p.ID = a.ID
	p.Name = a.Name
	p.Host = a.Host
	p.User = a.User
	p.Username = a.Username
	p.Port = a.Port
	p.Password = []byte(a.Password)
	p.PasswordEncrypted = a.PasswordEncrypted
	return nil
}

// ClearPassword 将密码字节置零，防止内存残留
func (p *SSHProfile) ClearPassword() {
	for i := range p.Password {
		p.Password[i] = 0
	}
	p.Password = nil
}

// PasswordString 返回密码字符串副本，并将原始字节置零
func (p *SSHProfile) PasswordString() string {
	s := string(p.Password)
	p.ClearPassword()
	return s
}

// DeployConfig 部署配置
type DeployConfig struct {
	ProfileID     string         `json:"profileId"`
	NodeName      string         `json:"nodeName"`
	Selected      []string       `json:"selected"`
	Ports         map[string]int `json:"ports"`
	PublicPorts   map[string]int `json:"publicPorts"`
	WebPort       int            `json:"webPort"`
	PublicWebPort int            `json:"publicWebPort"`
	Token         string         `json:"token"`
	Rule          string         `json:"rule"`
	SNI           string         `json:"sni"`
}

// DeployEvent 部署过程中向前端推送的事件
type DeployEvent struct {
	Type    string `json:"type"`
	Percent int    `json:"percent,omitempty"`
	Message string `json:"message"`
}

// CommandResult 远程命令执行结果
type CommandResult struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// AppState 持久化的完整应用状态
type AppState struct {
	Profiles     []SSHProfile   `json:"profiles"`
	DeployConfig DeployConfig   `json:"deployConfig"`
	ActiveClient string         `json:"activeClient"`
	UpdatedAt    string         `json:"updatedAt"`
	Extra        map[string]any `json:"extra,omitempty"`
}
