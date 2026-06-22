package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ssh"

	"proxy-installer/internal/vault"
)

// ── 全局默认值常量 ──────────────────────────────────────────
// 集中管理所有硬编码默认值，避免散落在各函数中导致修改遗漏。
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

type App struct {
	ctx            context.Context
	mu             sync.Mutex
	allowQuit      bool
	hostKeyStore   *SSHHostKeyStore
	vault          *vault.Vault
}

// SSHHostKeyEntry 用于存储已知 HostKey
type SSHHostKeyEntry struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Keys   []byte `json:"keys"`
	Hash   string `json:"hash"`
	Added  string `json:"added"`
}

// SSHHostKeyStore 管理已知 HostKey
type SSHHostKeyStore struct {
	entries []SSHHostKeyEntry
	mu      sync.RWMutex
	path    string
}

// knownHostsPath 返回 known_hosts 文件路径
func knownHostsPath() (string, error) {
	dirs, err := proxyDirs()
	if err != nil {
		return "", err
	}
	return filepath.Join(dirs["data"], "known_hosts.json"), nil
}

// NewSSHHostKeyStore 创建 HostKey 存储
func NewSSHHostKeyStore() (*SSHHostKeyStore, error) {
	path, err := knownHostsPath()
	if err != nil {
		return nil, err
	}
	store := &SSHHostKeyStore{path: path}
	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return store, nil
}

// load 从文件加载 HostKey
func (s *SSHHostKeyStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	var entries []SSHHostKeyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	s.entries = entries
	return nil
}

// save 保存 HostKey 到文件
func (s *SSHHostKeyStore) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Get 获取指定主机的 HostKey
func (s *SSHHostKeyStore) Get(host string, port int) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, e := range s.entries {
		if e.Host == host && e.Port == port {
			return e.Keys, true
		}
	}
	return nil, false
}

// Add 添加新的 HostKey
func (s *SSHHostKeyStore) Add(host string, port int, keys []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查是否已存在
	for i, e := range s.entries {
		if e.Host == host && e.Port == port {
			s.entries[i].Keys = keys
			s.entries[i].Added = time.Now().Format(time.RFC3339)
			return s.save()
		}
	}

	s.entries = append(s.entries, SSHHostKeyEntry{
		Host:   host,
		Port:   port,
		Keys:   keys,
		Hash:   fmt.Sprintf("sha256:%s", hex.EncodeToString(keys)),
		Added:  time.Now().Format(time.RFC3339),
	})
	return s.save()
}

// Remove 删除指定主机的 HostKey
func (s *SSHHostKeyStore) Remove(host string, port int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, e := range s.entries {
		if e.Host == host && e.Port == port {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return s.save() == nil
		}
	}
	return false
}

// Entries 返回所有条目
func (s *SSHHostKeyStore) Entries() []SSHHostKeyEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries
}

type SSHProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	User     string `json:"user"`
	Username string `json:"username"`
	Port     int    `json:"port"`
	Password string `json:"password"`  // 保留兼容性，后续清空
	PasswordEncrypted string `json:"password_encrypted,omitempty"`  // 新增：加密字段
}

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

type DeployEvent struct {
	Type    string `json:"type"`
	Percent int    `json:"percent,omitempty"`
	Message string `json:"message"`
}

type CommandResult struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

type AppState struct {
	Profiles     []SSHProfile   `json:"profiles"`
	DeployConfig DeployConfig   `json:"deployConfig"`
	ActiveClient string         `json:"activeClient"`
	UpdatedAt    string         `json:"updatedAt"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// NewApp 创建 App 实例并初始化 HostKey 存储和 Vault
func NewApp() *App {
	store, _ := NewSSHHostKeyStore()
	v, _ := newAppVault()
	return &App{
		hostKeyStore: store,
		vault:        v,
	}
}

func newAppVault() (*vault.Vault, error) {
	dirs, err := proxyDirs()
	if err != nil {
		return nil, err
	}
	autoKeyPath := filepath.Join(dirs["data"], ".autokey")
	return vault.NewVault(autoKeyPath)
}

func proxyDataRoot() (string, error) {
	if goruntime.GOOS == "windows" {
		if base := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); base != "" {
			return filepath.Join(base, "proxy-installer"), nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "proxy-installer"), nil
}

func proxyDirs() (map[string]string, error) {
	root, err := proxyDataRoot()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"root":     root,
		"app":      filepath.Join(root, "app"),
		"data":     filepath.Join(root, "data"),
		"profiles": filepath.Join(root, "profiles"),
		"reports":  filepath.Join(root, "reports"),
		"logs":     filepath.Join(root, "logs"),
		"cache":    filepath.Join(root, "cache"),
		"runtime":  filepath.Join(root, "runtime"),
		"webview":  filepath.Join(root, "webview"),
	}, nil
}

func ensureProxyDirs() (map[string]string, error) {
	dirs, err := proxyDirs()
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"data", "profiles", "reports", "logs", "cache", "runtime", "webview"} {
		if err := os.MkdirAll(dirs[key], 0700); err != nil {
			return nil, err
		}
	}
	return dirs, nil
}

func appStatePath() (string, error) {
	dirs, err := proxyDirs()
	if err != nil {
		return "", err
	}
	return filepath.Join(dirs["data"], "state.json"), nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_, _ = ensureProxyDirs()
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	allowQuit := a.allowQuit
	a.mu.Unlock()
	if allowQuit {
		return false
	}
	runtime.WindowHide(ctx)
	return true
}

func (a *App) showMainWindow() {
	if a.ctx == nil {
		return
	}
	runtime.Show(a.ctx)
	runtime.WindowShow(a.ctx)
}

func (a *App) quitFromTray() {
	a.mu.Lock()
	a.allowQuit = true
	a.mu.Unlock()
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

func (a *App) LoadAppState() (map[string]any, error) {
	path, err := appStatePath()
	if err != nil {
		return nil, err
	}
	dirs, _ := ensureProxyDirs()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{
			"ok":       true,
			"path":     path,
			"dirs":     dirs,
			"profiles": []SSHProfile{},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	var state AppState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("读取本地配置失败: %w", err)
	}

	// Decrypt passwords and auto-migrate plaintext ones
	if a.vault != nil {
		for i := range state.Profiles {
			p := &state.Profiles[i]
			if p.PasswordEncrypted != "" {
				dec, err := a.vault.Decrypt(p.PasswordEncrypted)
				if err == nil {
					p.Password = dec
				} else {
					fmt.Fprintf(os.Stderr, "WARNING: Failed to decrypt password for profile %s: %v\n", p.Name, err)
					p.Password = p.PasswordEncrypted
				}
			}
		}
	}

	return map[string]any{
		"ok":           true,
		"path":         path,
		"dirs":         dirs,
		"profiles":     state.Profiles,
		"deployConfig": state.DeployConfig,
		"activeClient": state.ActiveClient,
		"updatedAt":    state.UpdatedAt,
		"extra":        state.Extra,
	}, nil
}

func (a *App) SaveAppState(state AppState) (map[string]any, error) {
	path, err := appStatePath()
	if err != nil {
		return nil, err
	}
	dirs, _ := ensureProxyDirs()
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	if state.Extra == nil {
		state.Extra = map[string]any{}
	}

	// Encrypt passwords before saving
	if a.vault != nil {
		for i := range state.Profiles {
			p := &state.Profiles[i]
			if p.Password != "" {
				enc, err := a.vault.Encrypt(p.Password)
				if err == nil {
					p.PasswordEncrypted = enc
					p.Password = ""
				}
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return map[string]any{"ok": true, "path": path, "dirs": dirs, "updatedAt": state.UpdatedAt}, nil
}

func savedProfileCount() int {
	path, err := appStatePath()
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var state AppState
	if json.Unmarshal(data, &state) != nil {
		return 0
	}
	return len(state.Profiles)
}

func (a *App) TestConnection(profile SSHProfile) (map[string]any, error) {
	client, err := a.connect(profile)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, `printf 'ok:%s@%s\n' "$(whoami)" "$(hostname)"`, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": strings.TrimSpace(result.Stdout)}, nil
}

func (a *App) InspectVPS(profile SSHProfile) (map[string]any, error) {
	client, err := a.connect(profile)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+shellQuote(detectScript()), 45*time.Second)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":     true,
		"report": parseDetect(result.Stdout),
		"raw":    result.Stdout,
	}, nil
}

func (a *App) CheckPorts(profile SSHProfile, ports []int) (map[string]any, error) {
	client, err := a.connect(profile)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	var checks []string
	for _, port := range ports {
		if port < 1 || port > 65535 {
			continue
		}
		checks = append(checks, fmt.Sprintf(`if (ss -lntup 2>/dev/null || netstat -lntup 2>/dev/null || true) | grep -Eq '[:.]%d([[:space:]]|$)'; then printf '%d=busy\n'; else printf '%d=free\n'; fi`, port, port, port))
	}
	script := "set +e\n" + strings.Join(checks, "\n")
	result, err := runCommand(client, "bash -lc "+shellQuote(script), 20*time.Second)
	if err != nil {
		return nil, err
	}
	statuses := map[string]string{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 {
			statuses[parts[0]] = parts[1]
		}
	}
	return map[string]any{"ok": true, "statuses": statuses}, nil
}

func (a *App) MeasureLatency(profile SSHProfile, config DeployConfig) (map[string]any, error) {
	host := normalizeHostLiteral(profile.Host)
	if host == "" {
		return nil, fmt.Errorf("请输入 VPS 主机/IP")
	}
	if len(config.Selected) == 0 {
		config.Selected = []string{"ss"}
	}
	config.Selected = filterSupportedProtocols(config.Selected)
	if config.WebPort == 0 {
		config.WebPort = DefaultWebPort
	}
	if config.PublicWebPort == 0 {
		config.PublicWebPort = config.WebPort
	}
	if config.Token == "" {
		config.Token = DefaultToken
	}
	if config.Rule == "" {
		config.Rule = DefaultSubRule
	}

	var items []map[string]any
	for _, id := range config.Selected {
		def := protocolDefaults()[id]
		port := publicPortOrDefault(config, id, def)
		latency, status := probeTCP(host, port, 3, 4*time.Second)
		items = append(items, map[string]any{
			"kind":      "node",
			"protocol":  protocolLabel(id),
			"target":    net.JoinHostPort(host, strconv.Itoa(port)),
			"port":      port,
			"latencyMs": latency,
			"status":    status,
		})
	}

	url := buildSubscriptionURL(host, config, "shadowrocket")
	latency, status := probeHTTP(url, 6*time.Second)
	items = append(items, map[string]any{
		"kind":      "subscription",
		"protocol":  "Shadowrocket 订阅",
		"target":    url,
		"port":      publicWebPortOrDefault(config),
		"latencyMs": latency,
		"status":    status,
	})

	return map[string]any{"ok": true, "items": items, "checkedAt": time.Now().Format(time.RFC3339)}, nil
}

func (a *App) RunSpeedTest(profile SSHProfile) (map[string]any, error) {
	client, err := a.connect(profile)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	script := `
set +e
if ! command -v curl >/dev/null 2>&1; then
  printf 'error=missing curl\n'
  exit 3
fi
last="curl_failed"
for family in "-4" "-6" ""; do
  label="${family:-auto}"
  if [ -n "$family" ]; then
    out="$(curl "$family" -LfsS --connect-timeout 8 --max-time 28 -o /dev/null -w 'family='"$label"'\nhttp_code=%{http_code}\ntime_total=%{time_total}\nspeed_download=%{speed_download}\nremote_ip=%{remote_ip}\n' 'https://speed.cloudflare.com/__down?bytes=25000000' 2>/tmp/proxy-installer-speed.err)"
  else
    out="$(curl -LfsS --connect-timeout 8 --max-time 28 -o /dev/null -w 'family='"$label"'\nhttp_code=%{http_code}\ntime_total=%{time_total}\nspeed_download=%{speed_download}\nremote_ip=%{remote_ip}\n' 'https://speed.cloudflare.com/__down?bytes=25000000' 2>/tmp/proxy-installer-speed.err)"
  fi
  code=$?
  if [ "$code" -eq 0 ]; then
    printf '%s\n' "$out"
    exit 0
  fi
  last="curl_${code}_${label}"
done
printf 'error=%s\n' "$last"
exit 7
`
	result, err := runCommand(client, "bash -lc "+shellQuote(script), 32*time.Second)
	if err != nil {
		return nil, err
	}
	kv := parseKeyValue(result.Stdout)
	if result.Code != 0 {
		if kv["error"] != "" {
			return nil, fmt.Errorf("%s", kv["error"])
		}
		return nil, fmt.Errorf("测速失败: %s", strings.TrimSpace(result.Stderr))
	}
	speedBytes, _ := strconv.ParseFloat(kv["speed_download"], 64)
	totalSeconds, _ := strconv.ParseFloat(kv["time_total"], 64)
	return map[string]any{
		"ok":           true,
		"target":       "speed.cloudflare.com",
		"httpCode":     kv["http_code"],
		"timeSeconds":  totalSeconds,
		"downloadMbps": speedBytes * 8 / 1000 / 1000,
		"downloadMBps": speedBytes / 1000 / 1000,
		"remoteIp":     kv["remote_ip"],
		"family":       kv["family"],
		"checkedAt":    time.Now().Format(time.RFC3339),
	}, nil
}

func (a *App) RunNodeSpeedTest(profile SSHProfile, config DeployConfig) (map[string]any, error) {
	host := normalizeHostLiteral(profile.Host)
	if host == "" {
		return nil, fmt.Errorf("请输入 VPS 主机/IP")
	}
	config.Selected = filterSupportedProtocols(config.Selected)
	if len(config.Selected) == 0 {
		return nil, fmt.Errorf("请选择至少一个协议")
	}

	bin, err := ensureLocalSingBox()
	if err != nil {
		return map[string]any{
			"ok":        false,
			"skipped":   true,
			"reason":    err.Error(),
			"checkedAt": time.Now().Format(time.RFC3339),
		}, nil
	}

	if config.Token == "" {
		config.Token = DefaultToken
	}
	if config.SNI == "" {
		config.SNI = DefaultSNI
	}
	{
		nodeName := safeName(config.NodeName, DefaultNodeName)
		token := safeToken(config.Token)
		password := PasswordPrefix + token + PasswordSuffix
		uuid := stableUUID(token)
		_, realityPublic, realityShortID := realityKeys(token)
		var protocols []map[string]any
		var best map[string]any
		for _, id := range config.Selected {
			item := runSingleNodeSpeed(bin, host, config, id, nodeName, password, uuid, realityPublic, realityShortID)
			protocols = append(protocols, item)
			if item["ok"] == true {
				if best == nil || floatFromAny(item["downloadMbps"]) > floatFromAny(best["downloadMbps"]) {
					best = item
				}
			}
		}
		if best == nil {
			return map[string]any{
				"ok":        false,
				"via":       "node",
				"protocols": protocols,
				"error":     "所有协议节点测速均失败，请查看每个协议的错误详情",
				"singBox":   bin,
				"checkedAt": time.Now().Format(time.RFC3339),
			}, nil
		}
		return map[string]any{
			"ok":             true,
			"via":            "node",
			"bestProtocol":   best["protocol"],
			"bestProtocolID": best["protocolID"],
			"downloadMbps":   best["downloadMbps"],
			"downloadMBps":   best["downloadMBps"],
			"target":         best["target"],
			"protocols":      protocols,
			"singBox":        bin,
			"checkedAt":      time.Now().Format(time.RFC3339),
		}, nil
	}

	nodeName := safeName(config.NodeName, DefaultNodeName)
	token := safeToken(config.Token)
	password := PasswordPrefix + token + PasswordSuffix
	uuid := stableUUID(token)
	_, realityPublic, realityShortID := realityKeys(token)
	proxyPort, err := getFreeLocalPort()
	if err != nil {
		return nil, err
	}

	cfg, cfgSource, cfgWarning := nodeSpeedClientConfig(host, config, nodeName, password, uuid, realityPublic, realityShortID, proxyPort)
	dir, err := os.MkdirTemp("", "proxy-installer-singbox-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	configPath := filepath.Join(dir, "client.json")
	if err := os.WriteFile(configPath, []byte(cfg), 0600); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "-c", configPath)
	cmd.Env = filteredProxyEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动本机 sing-box 失败: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cancel()
		killProcessTree(cmd.Process)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}()

	if err := waitLocalPort(proxyPort, 8*time.Second); err != nil {
		select {
		case runErr := <-done:
			return nil, fmt.Errorf("本机 sing-box 退出: %v %s", runErr, strings.TrimSpace(stderr.String()))
		default:
		}
		return nil, fmt.Errorf("本机代理未启动: %w %s", err, strings.TrimSpace(stderr.String()))
	}

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	probe, err := runHTTPProbe(proxyURL, 12*time.Second)
	if err != nil {
		extra := ""
		if cfgWarning != "" {
			extra += "；" + cfgWarning
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			extra += "；sing-box: " + trimForMessage(msg, 400)
		}
		if usesUDPProtocol(config.Selected) {
			extra += "；HY2/TUIC 需要公网 UDP 转发；如果是 NAT 机器，请确认公网 UDP 端口映射到 VPS 内部端口，并确认订阅端口返回的是配置而不是空响应。"
		}
		return nil, fmt.Errorf("%w%s", err, extra)
	}
	speed, err := runHTTPDownloadSpeed(proxyURL, 60*time.Second)
	if err != nil {
		return nil, err
	}
	speed["ok"] = true
	speed["via"] = "node"
	speed["probe"] = probe
	speed["configSource"] = cfgSource
	speed["configWarning"] = cfgWarning
	speed["proxy"] = proxyURL
	speed["singBox"] = bin
	speed["checkedAt"] = time.Now().Format(time.RFC3339)
	return speed, nil
}

func runSingleNodeSpeed(bin, host string, config DeployConfig, protocolID, nodeName, password, uuid, realityPublic, realityShortID string) map[string]any {
	proxyPort, err := getFreeLocalPort()
	if err != nil {
		return nodeSpeedFailure(protocolID, config, fmt.Errorf("获取本地代理端口失败: %w", err), "", "")
	}

	single := config
	single.Selected = []string{protocolID}
	cfg := buildSingboxClientWithListen(host, single, nodeName, password, uuid, realityPublic, realityShortID, proxyPort)
	cfgSource := "generated-protocol"
	cfgWarning := ""
	dir, err := os.MkdirTemp("", "proxy-installer-singbox-*")
	if err != nil {
		return nodeSpeedFailure(protocolID, config, err, cfgSource, cfgWarning)
	}
	defer os.RemoveAll(dir)

	configPath := filepath.Join(dir, "client.json")
	if err := os.WriteFile(configPath, []byte(cfg), 0600); err != nil {
		return nodeSpeedFailure(protocolID, config, err, cfgSource, cfgWarning)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "-c", configPath)
	cmd.Env = filteredProxyEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nodeSpeedFailure(protocolID, config, fmt.Errorf("启动本地 sing-box 失败: %w", err), cfgSource, cfgWarning)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cancel()
		killProcessTree(cmd.Process)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}()

	if err := waitLocalPort(proxyPort, 8*time.Second); err != nil {
		select {
		case runErr := <-done:
			return nodeSpeedFailure(protocolID, config, fmt.Errorf("本地 sing-box 退出: %v %s", runErr, strings.TrimSpace(stderr.String())), cfgSource, cfgWarning)
		default:
		}
		return nodeSpeedFailure(protocolID, config, fmt.Errorf("本地代理未启动: %w %s", err, strings.TrimSpace(stderr.String())), cfgSource, cfgWarning)
	}

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	probe, err := runHTTPProbe(proxyURL, 12*time.Second)
	if err != nil {
		extra := ""
		if cfgWarning != "" {
			extra += "；" + cfgWarning
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			extra += "；sing-box: " + trimForMessage(msg, 400)
		}
		if usesUDPProtocol([]string{protocolID}) {
			extra += "；HY2/TUIC 需要公网 UDP 转发，如果是 NAT 机器，请确认公网 UDP 端口已映射到 VPS 内部端口"
		}
		return nodeSpeedFailure(protocolID, config, fmt.Errorf("%w%s", err, extra), cfgSource, cfgWarning)
	}
	speed, err := runHTTPDownloadSpeed(proxyURL, 60*time.Second)
	if err != nil {
		return nodeSpeedFailure(protocolID, config, err, cfgSource, cfgWarning)
	}
	speed["ok"] = true
	speed["via"] = "node"
	speed["protocolID"] = protocolID
	speed["protocol"] = protocolLabel(protocolID)
	speed["port"] = publicPortOrDefault(config, protocolID, protocolDefaults()[protocolID])
	speed["status"] = "ok"
	speed["probe"] = probe
	speed["configSource"] = cfgSource
	speed["configWarning"] = cfgWarning
	speed["proxy"] = proxyURL
	speed["singBox"] = bin
	speed["checkedAt"] = time.Now().Format(time.RFC3339)
	return speed
}

func nodeSpeedFailure(protocolID string, config DeployConfig, err error, cfgSource, cfgWarning string) map[string]any {
	return map[string]any{
		"ok":            false,
		"via":           "node",
		"protocolID":    protocolID,
		"protocol":      protocolLabel(protocolID),
		"port":          publicPortOrDefault(config, protocolID, protocolDefaults()[protocolID]),
		"status":        "failed",
		"error":         trimForMessage(strings.TrimSpace(err.Error()), 800),
		"configSource":  cfgSource,
		"configWarning": cfgWarning,
		"checkedAt":     time.Now().Format(time.RFC3339),
	}
}

func floatFromAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	default:
		return 0
	}
}

func (a *App) RunIPQuality(profile SSHProfile) (map[string]any, error) {
	client, err := a.connect(profile)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+shellQuote(ipQualityScript()), 110*time.Second)
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		kv := parseKeyValue(result.Stdout)
		if kv["error"] != "" {
			return nil, fmt.Errorf("%s", kv["error"])
		}
		return nil, fmt.Errorf("IP 纯净度检测失败: %s", strings.TrimSpace(result.Stderr))
	}

	raw, sourceErrors := parseIPQualitySources(result.Stdout)
	if len(raw) == 0 && len(sourceErrors) == 0 {
		return nil, fmt.Errorf("IP 纯净度检测失败: 没有拿到可解析的检测结果")
	}
	summary, sites, sections := buildQualityReport(raw, sourceErrors)
	return map[string]any{
		"ok":        true,
		"summary":   summary,
		"sites":     sites,
		"sections":  sections,
		"raw":       raw,
		"errors":    sourceErrors,
		"checkedAt": time.Now().Format(time.RFC3339),
	}, nil
}

func (a *App) ScanFootprint(profile SSHProfile) (map[string]any, error) {
	client, err := a.connect(profile)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+shellQuote(footprintScanScript()), 35*time.Second)
	if err != nil {
		return nil, err
	}
	return parseFootprint(result.Stdout), nil
}

func (a *App) UninstallStarter(profile SSHProfile, removeRuntime bool) (map[string]any, error) {
	client, err := a.connect(profile)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+shellQuote(uninstallScript(removeRuntime)), 90*time.Second)
	if err != nil {
		return nil, err
	}
	logs := []string{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "LOG=") {
			logs = append(logs, strings.TrimPrefix(line, "LOG="))
		}
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("清理失败: %s", strings.TrimSpace(result.Stderr))
	}
	after, _ := runCommand(client, "bash -lc "+shellQuote(footprintScanScript()), 35*time.Second)
	report := parseFootprint(after.Stdout)
	report["logs"] = logs
	report["ok"] = true
	return report, nil
}

func (a *App) CleanupSelectedFootprint(profile SSHProfile, protocolIDs []string, removeRuntime bool) (map[string]any, error) {
	ids := filterSupportedProtocols(protocolIDs)
	if len(ids) == 0 {
		return nil, fmt.Errorf("请选择要清理的协议")
	}
	client, err := a.connect(profile)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+shellQuote(cleanupSelectedScript(ids, removeRuntime)), 90*time.Second)
	if err != nil {
		return nil, err
	}
	logs := []string{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "LOG=") {
			logs = append(logs, strings.TrimPrefix(line, "LOG="))
		}
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("清理选中协议失败: %s", strings.TrimSpace(result.Stderr))
	}
	after, _ := runCommand(client, "bash -lc "+shellQuote(footprintScanScript()), 35*time.Second)
	report := parseFootprint(after.Stdout)
	report["logs"] = logs
	report["ok"] = true
	return report, nil
}

func (a *App) StartDeploy(profile SSHProfile, config DeployConfig) (map[string]any, error) {
	client, err := a.connect(profile)
	if err != nil {
		a.emit("error", 0, err.Error())
		return nil, err
	}
	defer client.Close()

	if config.SNI == "" {
		config.SNI = DefaultSNI
	}
	if config.WebPort == 0 {
		config.WebPort = DefaultWebPort
	}
	if config.Token == "" {
		config.Token = DefaultToken
	}
	if config.Rule == "" {
		config.Rule = DefaultSubRule
	}
	if len(config.Selected) == 0 {
		config.Selected = []string{"ss"}
	}

	script, err := buildDeployScript(profile, config)
	if err != nil {
		a.emit("error", 0, err.Error())
		return nil, err
	}

	a.emit("progress", 2, "连接成功，开始远程部署")
	code, err := a.runStreaming(client, "bash -lc "+shellQuote(script))
	if err != nil {
		a.emit("error", 0, err.Error())
		return nil, err
	}
	if code == 11 {
		a.emit("progress", 34, "远端下载 sing-box 失败，尝试由本机上传 Linux 二进制")
		if uploadErr := a.installSingBoxViaUpload(client); uploadErr != nil {
			msg := "本机上传 sing-box 兜底失败: " + uploadErr.Error()
			a.emit("error", 34, msg)
			return map[string]any{"ok": false, "code": code, "uploadError": uploadErr.Error()}, nil
		}
		a.emit("progress", 36, "sing-box 已上传，重新执行远程部署")
		code, err = a.runStreaming(client, "bash -lc "+shellQuote(script))
		if err != nil {
			a.emit("error", 0, err.Error())
			return nil, err
		}
	}
	if code != 0 {
		msg := fmt.Sprintf("部署失败，退出码 %d", code)
		a.emit("error", 0, msg)
		return map[string]any{"ok": false, "code": code}, nil
	}
	a.emit("done", 100, "部署完成")
	return map[string]any{"ok": true, "code": 0}, nil
}

// hostKeyCallback 实现 SSH HostKey 验证
// 首次连接会提示用户确认，后续连接会自动验证
func (a *App) hostKeyCallback(host string, remote net.Addr, key ssh.PublicKey) error {
	// 获取存储的 HostKey
	if a.hostKeyStore != nil {
		storedKeys, found := a.hostKeyStore.Get(host, remote.(*net.TCPAddr).Port)
		if found {
			// 比对存储的 HostKey
			expected := string(storedKeys)
			actual := string(key.Marshal())
			if expected == actual {
				return nil // HostKey 匹配，验证通过
			}
			// HostKey 不匹配，可能存在中间人攻击
			return fmt.Errorf("SSH HostKey 变更，可能存在中间人攻击风险")
		}
		// 未找到存储的 HostKey，首次连接，需要用户确认
		// 由于在后端无法直接交互，这里记录 HostKey 供后续使用
		// 实际交互式确认需要在前端实现
		if err := a.hostKeyStore.Add(host, remote.(*net.TCPAddr).Port, key.Marshal()); err != nil {
			return err
		}
		return nil
	}
	// 兼容模式：如果 store 未初始化，记录 HostKey 但不拒绝
	return nil
}

func (a *App) connect(profile SSHProfile) (*ssh.Client, error) {
	host := normalizeHostLiteral(profile.Host)
	if host == "" {
		return nil, fmt.Errorf("请输入 VPS 主机/IP")
	}
	user := strings.TrimSpace(profile.User)
	if user == "" {
		user = strings.TrimSpace(profile.Username)
	}
	if user == "" {
		user = "root"
	}
	port := profile.Port
	if port == 0 {
		port = 22
	}
	if profile.Password == "" {
		return nil, fmt.Errorf("请输入 SSH 密码")
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(profile.Password)},
		HostKeyCallback: func(host string, remote net.Addr, key ssh.PublicKey) error {
			return a.hostKeyCallback(host, remote, key)
		},
		Timeout: 18 * time.Second,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	return ssh.Dial("tcp", addr, config)
}

// sanitizeLogMessage 对日志消息中的敏感信息进行脱敏处理
func sanitizeLogMessage(msg string) string {
	// 密码字段: password=xxx 或 password: xxx
	re := regexp.MustCompile(`(?i)(password[=:]["']?)[^\s,;\]]{3,}`)
	msg = re.ReplaceAllString(msg, `${1}***`)
	// 令牌字段: token=xxx
	re = regexp.MustCompile(`(?i)(token[=:]["']?)[A-Za-z0-9_-]{3,}`)
	msg = re.ReplaceAllString(msg, `${1}***`)
	// 私钥字段: private_key/private-key=xxx
	re = regexp.MustCompile(`(?i)(private[_\-]key[=:]["']?)[^\s,;\]]{3,}`)
	msg = re.ReplaceAllString(msg, `${1}***`)
	// 公钥字段: public-key/public_key=xxx
	re = regexp.MustCompile(`(?i)(public[_\-]key[=:]["']?)[^\s,;\]]{3,}`)
	msg = re.ReplaceAllString(msg, `${1}***`)
	// UUID v4 格式
	re = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
	msg = re.ReplaceAllString(msg, "****-****-****-****-************")
	return msg
}

func (a *App) emit(kind string, percent int, message string) {
	if a.ctx == nil {
		return
	}
	message = sanitizeLogMessage(message)
	runtime.EventsEmit(a.ctx, "deploy:event", DeployEvent{Type: kind, Percent: percent, Message: message})
}

func (a *App) runStreaming(client *ssh.Client, command string) (int, error) {
	session, err := client.NewSession()
	if err != nil {
		return -1, err
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return -1, err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go a.scanDeployStream(stdout, "log", &wg)
	go a.scanDeployStream(stderr, "log", &wg)

	if err := session.Start(command); err != nil {
		return -1, err
	}
	err = session.Wait()
	wg.Wait()

	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus(), nil
	}
	return -1, err
}

func (a *App) scanDeployStream(reader io.Reader, fallbackType string, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "__VPS_STARTER_EVENT__|") {
			parts := strings.SplitN(line, "|", 4)
			if len(parts) == 4 {
				percent, _ := strconv.Atoi(parts[2])
				messageBytes, err := base64.StdEncoding.DecodeString(parts[3])
				if err == nil {
					a.emit(parts[1], percent, string(messageBytes))
					continue
				}
			}
		}
		var event DeployEvent
		if err := json.Unmarshal([]byte(line), &event); err == nil && event.Type != "" {
			a.emit(event.Type, event.Percent, event.Message)
			continue
		}
		a.emit(fallbackType, 0, line)
	}
}

func runCommand(client *ssh.Client, command string, timeout time.Duration) (CommandResult, error) {
	session, err := client.NewSession()
	if err != nil {
		return CommandResult{}, err
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	select {
	case err := <-done:
		code := 0
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				code = exitErr.ExitStatus()
			} else {
				return CommandResult{}, err
			}
		}
		return CommandResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
	case <-time.After(timeout):
		_ = session.Signal(ssh.SIGKILL)
		return CommandResult{}, fmt.Errorf("远程命令超时")
	}
}

func parseDetect(text string) map[string]any {
	kv := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			kv[parts[0]] = strings.Trim(parts[1], `"`)
		}
	}
	return map[string]any{
		"os": map[string]any{
			"prettyName":     kv["OS_PRETTY_NAME"],
			"id":             kv["OS_ID"],
			"kernel":         kv["KERNEL"],
			"arch":           kv["ARCH"],
			"packageManager": packageManager(kv),
			"hasSystemd":     kv["HAS_SYSTEMD"] == "yes",
		},
		"resources": map[string]any{
			"cpuModel": kv["CPU_MODEL"],
			"cpuCores": kv["CPU_CORES"],
			"memory":   kv["MEMORY"],
			"disk":     kv["DISK"],
			"uptime":   kv["UPTIME"],
		},
		"network": map[string]any{
			"privateIp":          kv["PRIVATE_IP"],
			"privateIpv6":        kv["PRIVATE_IPV6"],
			"publicIpv4":         kv["PUBLIC_IPV4"],
			"publicIpv6":         kv["PUBLIC_IPV6"],
			"natLikely":          kv["NAT_LIKELY"] == "yes",
			"defaultInterface":   kv["DEFAULT_IFACE"],
			"defaultInterfaceV6": kv["DEFAULT_IFACE_V6"],
			"ipv4Route":          kv["IPV4_ROUTE"] == "yes",
			"ipv6Route":          kv["IPV6_ROUTE"] == "yes",
			"ipv6Global":         kv["IPV6_GLOBAL"] == "yes",
		},
		"runtime": map[string]any{
			"virtualization": kv["VIRT"],
			"firewall":       firewall(kv),
		},
		"tools": map[string]any{
			"curl":      kv["CMD_CURL"] == "yes",
			"nginx":     kv["CMD_NGINX"] == "yes",
			"openssl":   kv["CMD_OPENSSL"] == "yes",
			"ss":        kv["CMD_SS"] == "yes",
			"systemctl": kv["CMD_SYSTEMCTL"] == "yes",
			"ufw":       kv["CMD_UFW"] == "yes",
			"firewalld": kv["CMD_FIREWALL_CMD"] == "yes",
		},
	}
}

func packageManager(kv map[string]string) string {
	for _, item := range []struct {
		key, name string
	}{
		{"CMD_APT_GET", "apt"},
		{"CMD_DNF", "dnf"},
		{"CMD_YUM", "yum"},
		{"CMD_PACMAN", "pacman"},
		{"CMD_ZYPPER", "zypper"},
	} {
		if kv[item.key] == "yes" {
			return item.name
		}
	}
	return "unknown"
}

func firewall(kv map[string]string) string {
	if kv["CMD_FIREWALL_CMD"] == "yes" {
		return "firewalld"
	}
	if kv["CMD_UFW"] == "yes" {
		return "ufw"
	}
	if kv["CMD_NFT"] == "yes" {
		return "nftables"
	}
	if kv["CMD_IPTABLES"] == "yes" {
		return "iptables"
	}
	return "unknown"
}

func detectScript() string {
	return `
set +e
if [ -r /etc/os-release ]; then
  while IFS= read -r line; do
    case "$line" in ID=*|ID_LIKE=*|PRETTY_NAME=*) printf 'OS_%s\n' "$line" ;; esac
  done < /etc/os-release
fi
printf 'KERNEL=%s\n' "$(uname -r 2>/dev/null)"
printf 'ARCH=%s\n' "$(uname -m 2>/dev/null)"
command -v nproc >/dev/null 2>&1 && printf 'CPU_CORES=%s\n' "$(nproc)"
printf 'CPU_MODEL=%s\n' "$(awk -F: '/model name|Hardware|Processor/ {gsub(/^[ \t]+/, "", $2); print $2; exit}' /proc/cpuinfo 2>/dev/null)"
command -v free >/dev/null 2>&1 && printf 'MEMORY=%s\n' "$(free -h | awk '/Mem:/ {print $2 " total, " $7 " available"}')"
command -v df >/dev/null 2>&1 && printf 'DISK=%s\n' "$(df -h / | awk 'NR==2 {print $2 " total, " $4 " free"}')"
printf 'UPTIME=%s\n' "$(uptime -p 2>/dev/null)"
[ -d /run/systemd/system ] && printf 'HAS_SYSTEMD=yes\n' || printf 'HAS_SYSTEMD=no\n'
for cmd in curl nginx openssl ss systemctl ufw firewall-cmd nft iptables apt-get dnf yum pacman zypper; do
  key="$(printf '%s' "$cmd" | tr '[:lower:]-' '[:upper:]_')"
  command -v "$cmd" >/dev/null 2>&1 && printf 'CMD_%s=yes\n' "$key" || printf 'CMD_%s=no\n' "$key"
done
command -v systemd-detect-virt >/dev/null 2>&1 && printf 'VIRT=%s\n' "$(systemd-detect-virt 2>/dev/null || printf none)" || printf 'VIRT=unknown\n'
if command -v ip >/dev/null 2>&1; then
  printf 'DEFAULT_IFACE=%s\n' "$(ip route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="dev") {print $(i+1); exit}}')"
  printf 'DEFAULT_IFACE_V6=%s\n' "$(ip -6 route get 2606:4700:4700::1111 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="dev") {print $(i+1); exit}}')"
  printf 'PRIVATE_IP=%s\n' "$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')"
  printf 'PRIVATE_IPV6=%s\n' "$(ip -6 route get 2606:4700:4700::1111 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')"
  ip -4 route get 1.1.1.1 >/dev/null 2>&1 && printf 'IPV4_ROUTE=yes\n' || printf 'IPV4_ROUTE=no\n'
  ip -6 route get 2606:4700:4700::1111 >/dev/null 2>&1 && printf 'IPV6_ROUTE=yes\n' || printf 'IPV6_ROUTE=no\n'
  ip -6 addr show scope global 2>/dev/null | grep -q 'inet6 ' && printf 'IPV6_GLOBAL=yes\n' || printf 'IPV6_GLOBAL=no\n'
fi
pub4=""; pub6=""
command -v curl >/dev/null 2>&1 && pub4="$(curl -4fsS --connect-timeout 5 --max-time 8 https://api.ipify.org 2>/dev/null | tr -d '[:space:]')"
command -v curl >/dev/null 2>&1 && pub6="$(curl -6fsS --connect-timeout 5 --max-time 8 https://api64.ipify.org 2>/dev/null | tr -d '[:space:]')"
printf 'PUBLIC_IPV4=%s\n' "$pub4"
printf 'PUBLIC_IPV6=%s\n' "$pub6"
if [ -n "$pub4" ] && ip -4 addr 2>/dev/null | grep -q "$pub4"; then printf 'NAT_LIKELY=no\n'; elif [ -n "$pub4" ]; then printf 'NAT_LIKELY=yes\n'; elif [ -n "$pub6" ] && ip -6 addr 2>/dev/null | grep -qi "$pub6"; then printf 'NAT_LIKELY=no\n'; else printf 'NAT_LIKELY=unknown\n'; fi
`
}

func ipQualityScript() string {
	return `
set +e
if ! command -v curl >/dev/null 2>&1; then printf 'error=missing curl\n'; exit 3; fi
if ! command -v base64 >/dev/null 2>&1; then printf 'error=missing base64\n'; exit 4; fi
tmpdir="$(mktemp -d 2>/dev/null)"
if [ -z "$tmpdir" ]; then
  tmpdir="/tmp/proxy-installer-ipq-$$"
  mkdir -p "$tmpdir" || { printf 'error=tmpdir_failed\n'; exit 5; }
fi
trap 'rm -rf "$tmpdir"' EXIT
emit_b64(){ printf '%s' "$2" | base64 | tr -d '\n' | awk -v n="$1" '{print n "|" $0}'; }
fetch_json(){
  order="$1"; name="$2"; shift 2
  out="$tmpdir/$order-$name.out"
  last="empty"
  for url in "$@"; do
    for family in "-4" "-6" ""; do
      if [ -n "$family" ]; then
        body="$(curl "$family" -fsSL --connect-timeout 5 --max-time 12 "$url" 2>/dev/null)"
      else
        body="$(curl -fsSL --connect-timeout 5 --max-time 12 "$url" 2>/dev/null)"
      fi
      code=$?
      if [ "$code" -eq 0 ] && [ -n "$body" ]; then
        printf 'SOURCE=' > "$out"; emit_b64 "$name" "$body" >> "$out"; printf '\n' >> "$out"
        return
      fi
      last="curl_${code}_${family:-auto}"
    done
  done
  printf 'SOURCE_ERROR=%s|%s\n' "$name" "$last" > "$out"
}
fetch_text(){
  order="$1"; name="$2"; shift 2
  out="$tmpdir/$order-$name.out"
  last="empty"
  for url in "$@"; do
    for family in "-4" "-6" ""; do
      if [ -n "$family" ]; then
        body="$(curl "$family" -fsSL --connect-timeout 5 --max-time 12 "$url" 2>/dev/null)"
      else
        body="$(curl -fsSL --connect-timeout 5 --max-time 12 "$url" 2>/dev/null)"
      fi
      code=$?
      if [ "$code" -eq 0 ] && [ -n "$body" ]; then
        printf 'SOURCE_TEXT=' > "$out"; emit_b64 "$name" "$body" >> "$out"; printf '\n' >> "$out"
        return
      fi
      last="curl_${code}_${family:-auto}"
    done
  done
  printf 'SOURCE_ERROR=%s|%s\n' "$name" "$last" > "$out"
}
check_http(){
  order="$1"; key="$2"; url="$3"; ok_codes="$4"
  out="$tmpdir/$order-$key.out"
  code="000"
  for family in "-4" "-6" ""; do
    if [ -n "$family" ]; then
      code="$(curl "$family" -kLsS --connect-timeout 5 --max-time 12 -A 'Mozilla/5.0 ProxyInstaller' -o /dev/null -w '%{http_code}' "$url" 2>/dev/null)"
    else
      code="$(curl -kLsS --connect-timeout 5 --max-time 12 -A 'Mozilla/5.0 ProxyInstaller' -o /dev/null -w '%{http_code}' "$url" 2>/dev/null)"
    fi
    [ -n "$code" ] || code="000"
    [ "$code" != "000" ] && break
  done
  case " $ok_codes " in
    *" $code "*) status="ok" ;;
    *) status="fail" ;;
  esac
  printf 'CHECK=%s|%s|http_%s\n' "$key" "$status" "$code" > "$out"
}
dnsbl_check(){
  order="$1"; ip="$2"
  out="$tmpdir/$order-dnsbl.out"
  if ! command -v getent >/dev/null 2>&1; then printf 'CHECK=dnsbl.system|skip|getent_missing\n' > "$out"; return; fi
  if ! command -v timeout >/dev/null 2>&1; then printf 'CHECK=dnsbl.system|skip|timeout_missing\n' > "$out"; return; fi
  rev="$(printf '%s' "$ip" | awk -F. 'NF==4{print $4"."$3"."$2"."$1}')"
  [ -n "$rev" ] || { printf 'CHECK=dnsbl.system|skip|bad_ip\n' > "$out"; return; }
  : > "$out"
  for zone in zen.spamhaus.org bl.spamcop.net b.barracudacentral.org dnsbl.dronebl.org dnsbl.sorbs.net spam.dnsbl.sorbs.net cbl.abuseat.org psbl.surriel.com ubl.unsubscore.com dnsbl-1.uceprotect.net; do
    if timeout 4 getent hosts "$rev.$zone" >/dev/null 2>&1; then
      printf 'CHECK=dnsbl.%s|listed|listed\n' "$zone" >> "$out"
    else
      printf 'CHECK=dnsbl.%s|clean|clean\n' "$zone" >> "$out"
    fi
  done
}
smtp_check(){
  order="$1"; key="$2"; host="$3"
  out="$tmpdir/$order-$key.out"
  if command -v timeout >/dev/null 2>&1; then
    if timeout 7 bash -lc "</dev/tcp/$host/25" >/dev/null 2>&1; then
      printf 'CHECK=%s|open|connect_ok\n' "$key" > "$out"
    else
      printf 'CHECK=%s|blocked|connect_failed\n' "$key" > "$out"
    fi
  else
    printf 'CHECK=%s|skip|timeout_missing\n' "$key" > "$out"
  fi
}
public_ip="$(curl -4fsSL --connect-timeout 5 --max-time 8 https://api.ipify.org 2>/dev/null || curl -6fsSL --connect-timeout 5 --max-time 8 https://api64.ipify.org 2>/dev/null || true)"
[ -n "$public_ip" ] && printf 'CHECK=base.public_ip|ok|%s\n' "$public_ip" > "$tmpdir/00-base.public_ip.out"
fetch_json "10" "ippure" "https://my.ippure.com/v1/info" &
fetch_json "11" "ip-api" "http://ip-api.com/json/?fields=status,message,query,country,regionName,city,isp,org,as,asname,reverse,mobile,proxy,hosting" &
fetch_json "12" "ipinfo" "https://ipinfo.io/json" &
fetch_json "13" "ipwhois" "https://ipwho.is/" &
fetch_json "14" "ipapi" "https://ipapi.co/json/" &
fetch_json "15" "dbip" "https://api.db-ip.com/v2/free/self" &
fetch_text "20" "ping0" "https://ipv4.ping0.cc/geo" "https://ipv6.ping0.cc/geo" "https://ping0.cc/geo" &
fetch_text "30" "iplark" "https://iplark.com/cdn-cgi/trace" "https://iplark.com" &
fetch_text "31" "cloudflare" "https://www.cloudflare.com/cdn-cgi/trace" &
[ -n "$public_ip" ] && fetch_text "32" "scamalytics" "https://scamalytics.com/ip/$public_ip" &
[ -n "$public_ip" ] && dnsbl_check "40" "$public_ip" &
check_http "50" "stream.youtube" "https://www.youtube.com/generate_204" "204" &
check_http "51" "stream.netflix" "https://www.netflix.com/title/81280792" "200 301 302" &
check_http "52" "stream.disneyplus" "https://www.disneyplus.com/" "200 301 302 403" &
check_http "53" "stream.tiktok" "https://www.tiktok.com/" "200 301 302 403" &
check_http "54" "stream.reddit" "https://www.reddit.com/" "200 301 302 403" &
check_http "55" "ai.openai" "https://api.openai.com/" "200 401 403 404 421" &
check_http "56" "ai.chatgpt" "https://chat.openai.com/cdn-cgi/trace" "200 403" &
smtp_check "70" "mail.gmail" "gmail-smtp-in.l.google.com" &
smtp_check "71" "mail.outlook" "outlook-com.olc.protection.outlook.com" &
smtp_check "72" "mail.yahoo" "mta5.am0.yahoodns.net" &
smtp_check "73" "mail.apple" "mx01.mail.icloud.com" &
smtp_check "74" "mail.qq" "mx3.qq.com" &
smtp_check "75" "mail.mailru" "mxs.mail.ru" &
smtp_check "76" "mail.aol" "mx-aol.mail.gm0.yahoodns.net" &
smtp_check "77" "mail.gmx" "mx00.gmx.net" &
smtp_check "78" "mail.mailcom" "mx00.mail.com" &
wait
for item in "$tmpdir"/*.out; do
  [ -f "$item" ] && cat "$item"
done
exit 0
`
}

func footprintScanScript() string {
	return `
set +e
item(){
  label="$1"; path="$2"
  if [ -e "$path" ]; then
    size="$(du -sh "$path" 2>/dev/null | awk '{print $1}')"
    [ -n "$size" ] || size="-"
    printf 'ITEM=%s|%s|present|%s\n' "$label" "$path" "$size"
  else
    printf 'ITEM=%s|%s|missing|-\n' "$label" "$path"
  fi
}
svc(){
  name="$1"
  if command -v systemctl >/dev/null 2>&1; then
    active="$(systemctl is-active "$name" 2>/dev/null || true)"
    enabled="$(systemctl is-enabled "$name" 2>/dev/null || true)"
  else
    active="no-systemd"; enabled="no-systemd"
  fi
  [ -n "$active" ] || active="unknown"
  [ -n "$enabled" ] || enabled="unknown"
  printf 'SERVICE=%s|%s|%s\n' "$name" "$active" "$enabled"
}
item "核心配置目录" "/etc/proxy-installer"
item "订阅文件目录" "/var/www/proxy-installer"
item "nginx 订阅配置" "/etc/nginx/conf.d/proxy-installer.conf"
item "旧版核心配置目录" "/etc/vps-node-starter"
item "旧版订阅文件目录" "/var/www/vps-node-starter"
item "旧版 nginx 订阅配置" "/etc/nginx/conf.d/vps-node-starter.conf"
item "sing-box 配置" "/etc/sing-box/config.json"
owned="no"
if [ -f /etc/sing-box/config.json ] && grep -Eq '/etc/proxy-installer|/etc/vps-node-starter|proxy-installer|vps-node-starter|vless-reality-in|hy2-in|trojan-in|ss-in|vmess-in|tuic-in' /etc/sing-box/config.json; then owned="yes"; fi
printf 'FLAG=singboxOwned|%s\n' "$owned"
protocol(){
  id="$1"; label="$2"; tag="$3"; pattern="$4"
  cfg="missing"; sub="missing"; port="-"; status="missing"
  if [ -f /etc/sing-box/config.json ] && grep -q "\"tag\"[[:space:]]*:[[:space:]]*\"$tag\"" /etc/sing-box/config.json; then
    cfg="present"
    port="$(awk -v tag="$tag" '
      $0 ~ "\"tag\"[[:space:]]*:[[:space:]]*\"" tag "\"" { seen=1 }
      seen && /"listen_port"[[:space:]]*:/ { gsub(/[^0-9]/, "", $0); print $0; exit }
    ' /etc/sing-box/config.json 2>/dev/null)"
    [ -n "$port" ] || port="-"
  fi
  for subroot in /var/www/proxy-installer /var/www/vps-node-starter; do
    if [ -d "$subroot" ] && grep -R -Eiq "$pattern" "$subroot" 2>/dev/null; then
      sub="present"
      break
    fi
  done
  if [ "$cfg" = "present" ] && [ "$sub" = "present" ]; then status="complete"; fi
  if [ "$cfg" = "present" ] && [ "$sub" != "present" ]; then status="partial"; fi
  if [ "$cfg" != "present" ] && [ "$sub" = "present" ]; then status="partial"; fi
  printf 'PROTOCOL=%s|%s|%s|%s|%s|%s\n' "$id" "$label" "$status" "$cfg" "$sub" "$port"
}
protocol "vless-reality" "VLESS Reality" "vless-reality-in" "Reality|vless|VLESS"
protocol "hy2" "Hysteria2" "hy2-in" "Hysteria|hysteria2|HY2"
protocol "tuic" "TUIC" "tuic-in" "TUIC|tuic"
protocol "trojan" "Trojan" "trojan-in" "Trojan|trojan"
protocol "ss" "Shadowsocks" "ss-in" "Shadowsocks|shadowsocks|ss://"
protocol "vmess" "VMess" "vmess-in" "VMess|vmess"
svc sing-box
svc nginx
count=0
for f in /tmp/proxy-installer-*.log /tmp/vps-lite-*.log; do
  [ -e "$f" ] || continue
  count=$((count+1))
  size="$(wc -c < "$f" 2>/dev/null | tr -d '[:space:]')"
  [ -n "$size" ] || size="0"
  printf 'LOGFILE=%s|%s\n' "$f" "$size"
done
printf 'FLAG=tmpLogCount|%s\n' "$count"
`
}

func uninstallScript(removeRuntime bool) string {
	remove := "no"
	if removeRuntime {
		remove = "yes"
	}
	return fmt.Sprintf(`
set +e
log(){ printf 'LOG=%%s\n' "$1"; }
owned="no"
if [ -f /etc/sing-box/config.json ] && grep -Eq '/etc/proxy-installer|/etc/vps-node-starter|proxy-installer|vps-node-starter|vless-reality-in|hy2-in|trojan-in|ss-in|vmess-in|tuic-in' /etc/sing-box/config.json; then owned="yes"; fi
ports=""
if [ "$owned" = "yes" ] && [ -f /etc/sing-box/config.json ]; then
  ports="$(grep -Eo '"listen_port"[[:space:]]*:[[:space:]]*[0-9]+' /etc/sing-box/config.json 2>/dev/null | grep -Eo '[0-9]+' | sort -n | uniq | tr '\n' ' ')"
fi
for nginx_conf in /etc/nginx/conf.d/proxy-installer.conf /etc/nginx/conf.d/vps-node-starter.conf; do
  if [ -f "$nginx_conf" ]; then
    webports="$(awk '/^[[:space:]]*listen[[:space:]]+[0-9]+/ {gsub(/;/,"",$2); print $2}' "$nginx_conf" | sort -n | uniq | tr '\n' ' ')"
    ports="$ports $webports"
  fi
done
if [ "$owned" = "yes" ]; then
  if command -v systemctl >/dev/null 2>&1; then
    systemctl stop sing-box >/dev/null 2>&1 || true
    systemctl disable sing-box >/dev/null 2>&1 || true
  fi
  rm -f /etc/sing-box/config.json
  log "已停止并禁用本工具部署的 sing-box 服务"
else
  log "未确认 /etc/sing-box/config.json 属于本工具，已跳过 sing-box 停止与配置删除"
fi
if command -v ufw >/dev/null 2>&1; then
  for port in $ports; do
    ufw delete allow "$port/tcp" >/dev/null 2>&1 || true
    ufw delete allow "$port/udp" >/dev/null 2>&1 || true
  done
  [ -n "$ports" ] && log "已尝试删除 ufw 中的相关端口规则: $ports"
fi
if command -v firewall-cmd >/dev/null 2>&1; then
  for port in $ports; do
    firewall-cmd --remove-port="$port/tcp" >/dev/null 2>&1 || true
    firewall-cmd --remove-port="$port/udp" >/dev/null 2>&1 || true
    firewall-cmd --permanent --remove-port="$port/tcp" >/dev/null 2>&1 || true
    firewall-cmd --permanent --remove-port="$port/udp" >/dev/null 2>&1 || true
  done
  [ -n "$ports" ] && firewall-cmd --reload >/dev/null 2>&1 || true
  [ -n "$ports" ] && log "已尝试删除 firewalld 中的相关端口规则: $ports"
fi
rm -rf /etc/proxy-installer /var/www/proxy-installer /etc/vps-node-starter /var/www/vps-node-starter
rm -f /etc/nginx/conf.d/proxy-installer.conf /etc/nginx/conf.d/vps-node-starter.conf
rm -f /tmp/proxy-installer-*.log /tmp/vps-lite-*.log
log "已删除本工具配置、订阅目录和临时日志"
if command -v nginx >/dev/null 2>&1; then
  if nginx -t >/tmp/proxy-installer-nginx-clean.log 2>&1; then
    if command -v systemctl >/dev/null 2>&1; then systemctl reload nginx >/dev/null 2>&1 || systemctl restart nginx >/dev/null 2>&1 || true; fi
    log "nginx 配置已重载"
  else
    log "nginx -t 未通过，已保留错误日志 /tmp/proxy-installer-nginx-clean.log"
  fi
fi
if [ "%s" = "yes" ] && [ "$owned" = "yes" ]; then
  bin="$(command -v sing-box 2>/dev/null || true)"
  [ -n "$bin" ] && rm -f "$bin" && log "已删除 sing-box 二进制: $bin"
  rm -f /etc/systemd/system/sing-box.service /lib/systemd/system/sing-box.service /usr/lib/systemd/system/sing-box.service
  rm -rf /etc/sing-box
  if command -v systemctl >/dev/null 2>&1; then systemctl daemon-reload >/dev/null 2>&1 || true; fi
  log "已尝试移除 sing-box 运行时和 systemd service"
fi
exit 0
`, remove)
}

func cleanupSelectedScript(protocolIDs []string, removeRuntime bool) string {
	ids := strings.Join(filterSupportedProtocols(protocolIDs), " ")
	remove := "no"
	if removeRuntime {
		remove = "yes"
	}
	script := `
set +e
IDS="__IDS__"
REMOVE_RUNTIME="__REMOVE__"
log(){ printf 'LOG=%s\n' "$1"; }
tags=""
labels=""
for id in $IDS; do
  case "$id" in
    vless-reality) tag="vless-reality-in"; label="VLESS Reality" ;;
    hy2) tag="hy2-in"; label="Hysteria2" ;;
    tuic) tag="tuic-in"; label="TUIC" ;;
    trojan) tag="trojan-in"; label="Trojan" ;;
    ss) tag="ss-in"; label="Shadowsocks" ;;
    vmess) tag="vmess-in"; label="VMess" ;;
    *) continue ;;
  esac
  [ -n "$tags" ] && tags="$tags,$tag" || tags="$tag"
  labels="$labels $label"
done
[ -n "$tags" ] || { log "没有可清理的协议"; exit 0; }
owned="no"
if [ -f /etc/sing-box/config.json ] && grep -Eq '/etc/proxy-installer|/etc/vps-node-starter|proxy-installer|vps-node-starter|vless-reality-in|hy2-in|trojan-in|ss-in|vmess-in|tuic-in' /etc/sing-box/config.json; then owned="yes"; fi
if [ "$owned" != "yes" ]; then
  log "未确认 /etc/sing-box/config.json 属于本工具，已跳过协议级修改"
  exit 0
fi
py=""
command -v python3 >/dev/null 2>&1 && py="$(command -v python3)"
if [ -z "$py" ] && command -v python >/dev/null 2>&1; then py="$(command -v python)"; fi
if [ -z "$py" ]; then
  log "缺少 python3/python，无法安全编辑 sing-box JSON；请使用全量清理或安装 python 后重试"
  exit 2
fi
pyout="$("$py" - "$tags" <<'PY'
import json, os, sys, tempfile
path = "/etc/sing-box/config.json"
tags = set(filter(None, sys.argv[1].split(",")))
try:
    with open(path, "r", encoding="utf-8") as fh:
        cfg = json.load(fh)
except Exception as exc:
    print("ERROR=" + str(exc).replace("\n", " ")[:300])
    sys.exit(1)
ports = []
removed = []
remaining = []
for inbound in cfg.get("inbounds", []):
    tag = str(inbound.get("tag", ""))
    if tag in tags:
        removed.append(tag)
        port = inbound.get("listen_port")
        if port:
            ports.append(str(port))
    else:
        remaining.append(inbound)
cfg["inbounds"] = remaining
tmp = path + ".proxy-installer.tmp"
with open(tmp, "w", encoding="utf-8") as fh:
    json.dump(cfg, fh, ensure_ascii=False, indent=2)
    fh.write("\n")
os.replace(tmp, path)
print("PORTS=" + " ".join(sorted(set(ports), key=lambda x: int(x) if x.isdigit() else 0)))
print("REMOVED=" + str(len(removed)))
print("REMAINING=" + str(len(remaining)))
print("REMOVED_TAGS=" + ",".join(removed))
PY
)"
code=$?
if [ "$code" -ne 0 ]; then
  err="$(printf '%s\n' "$pyout" | awk -F= '/^ERROR=/{print $2; exit}')"
  log "编辑 sing-box JSON 失败: ${err:-unknown}"
  exit "$code"
fi
ports="$(printf '%s\n' "$pyout" | awk -F= '/^PORTS=/{print $2; exit}')"
removed="$(printf '%s\n' "$pyout" | awk -F= '/^REMOVED=/{print $2; exit}')"
remaining="$(printf '%s\n' "$pyout" | awk -F= '/^REMAINING=/{print $2; exit}')"
removed_tags="$(printf '%s\n' "$pyout" | awk -F= '/^REMOVED_TAGS=/{print $2; exit}')"
log "已移除选中协议 inbound: ${removed_tags:-none}"
if command -v ufw >/dev/null 2>&1; then
  for port in $ports; do
    ufw delete allow "$port/tcp" >/dev/null 2>&1 || true
    ufw delete allow "$port/udp" >/dev/null 2>&1 || true
  done
  [ -n "$ports" ] && log "已尝试删除 ufw 端口规则: $ports"
fi
if command -v firewall-cmd >/dev/null 2>&1; then
  for port in $ports; do
    firewall-cmd --remove-port="$port/tcp" >/dev/null 2>&1 || true
    firewall-cmd --remove-port="$port/udp" >/dev/null 2>&1 || true
    firewall-cmd --permanent --remove-port="$port/tcp" >/dev/null 2>&1 || true
    firewall-cmd --permanent --remove-port="$port/udp" >/dev/null 2>&1 || true
  done
  [ -n "$ports" ] && firewall-cmd --reload >/dev/null 2>&1 || true
  [ -n "$ports" ] && log "已尝试删除 firewalld 端口规则: $ports"
fi
if [ "${remaining:-0}" = "0" ]; then
  if command -v systemctl >/dev/null 2>&1; then
    systemctl stop sing-box >/dev/null 2>&1 || true
    systemctl disable sing-box >/dev/null 2>&1 || true
  fi
  rm -f /etc/sing-box/config.json
  rm -rf /etc/proxy-installer /var/www/proxy-installer /etc/vps-node-starter /var/www/vps-node-starter
  rm -f /etc/nginx/conf.d/proxy-installer.conf /etc/nginx/conf.d/vps-node-starter.conf
  rm -f /tmp/proxy-installer-*.log /tmp/vps-lite-*.log
  log "没有剩余协议，已清理本工具配置、订阅目录、nginx 片段和临时日志"
  if command -v nginx >/dev/null 2>&1; then
    nginx -t >/tmp/proxy-installer-nginx-clean.log 2>&1 && { command -v systemctl >/dev/null 2>&1 && systemctl reload nginx >/dev/null 2>&1 || true; }
  fi
  if [ "$REMOVE_RUNTIME" = "yes" ]; then
    bin="$(command -v sing-box 2>/dev/null || true)"
    [ -n "$bin" ] && rm -f "$bin" && log "已删除 sing-box 二进制: $bin"
    rm -f /etc/systemd/system/sing-box.service /lib/systemd/system/sing-box.service /usr/lib/systemd/system/sing-box.service
    rm -rf /etc/sing-box
    command -v systemctl >/dev/null 2>&1 && systemctl daemon-reload >/dev/null 2>&1 || true
    log "已尝试移除 sing-box 运行时和 systemd service"
  fi
else
  if command -v systemctl >/dev/null 2>&1; then
    systemctl restart sing-box >/dev/null 2>&1 && log "sing-box 已重启并应用剩余协议配置" || log "sing-box 重启失败，请查看 systemctl status sing-box"
  else
    log "已修改配置；当前系统没有 systemctl，请手动重启 sing-box"
  fi
  log "订阅文件为聚合生成，清理部分协议后建议重新部署一次以刷新订阅内容"
fi
exit 0
`
	script = strings.ReplaceAll(script, "__IDS__", ids)
	script = strings.ReplaceAll(script, "__REMOVE__", remove)
	return script
}

func buildDeployScript(profile SSHProfile, config DeployConfig) (string, error) {
	host := normalizeHostLiteral(profile.Host)
	if host == "" {
		return "", fmt.Errorf("host is empty")
	}
	config.Selected = filterSupportedProtocols(config.Selected)
	if len(config.Selected) == 0 {
		return "", fmt.Errorf("请选择至少一个支持的协议")
	}
	for _, id := range config.Selected {
		port := portOrDefault(config.Ports, id, protocolDefaults()[id])
		if port < 1 || port > 65535 {
			return "", fmt.Errorf("%s 内部端口必须在 1-65535 之间", id)
		}
		publicPort := publicPortOrDefault(config, id, port)
		if publicPort < 1 || publicPort > 65535 {
			return "", fmt.Errorf("%s 公网端口必须在 1-65535 之间", id)
		}
	}
	nodeName := safeName(config.NodeName, DefaultNodeName)
	token := safeToken(config.Token)
	sni := safeDomain(config.SNI, DefaultSNI)
	password := PasswordPrefix + token + PasswordSuffix
	uuid := stableUUID(token)
	realityPrivate, realityPublic, realityShortID := realityKeys(token)
	webPort := config.WebPort
	if webPort == 0 {
		webPort = DefaultWebPort
	}
	if config.PublicWebPort == 0 {
		config.PublicWebPort = webPort
	}

	serverConfig := buildServerConfig(config.Selected, config.Ports, nodeName, password, uuid, realityPrivate, realityShortID, sni)
	files := buildClientFiles(host, config, nodeName, password, uuid, realityPublic, realityShortID)
	nginxConfig := buildNginxConfig(webPort, token, config.Rule)
	selectedPorts := selectedPorts(config.Selected, config.Ports)
	portList := intsToShell(selectedPorts)

	return fmt.Sprintf(`
set -euo pipefail
emit(){ msg="$(printf '%%s' "$3" | base64 | tr -d '\n')"; printf '__VPS_STARTER_EVENT__|%%s|%%s|%%s\n' "$1" "$2" "$msg"; }
write_b64(){ printf '%%s' "$1" | base64 -d > "$2"; }
emit_file(){ stage="$1"; file="$2"; [ -f "$file" ] || return 0; while IFS= read -r line; do emit log "$stage" "$line"; done < "$file"; }
backup_apt_file(){ f="$1"; [ -f "$f" ] || return 0; [ -f "$f.proxy-installer.bak" ] || cp "$f" "$f.proxy-installer.bak" 2>/dev/null || true; }
repair_debian_apt_sources(){
  [ "${ID:-}" = "debian" ] || return 0
  changed=0
  case "${VERSION_CODENAME:-}" in
    bullseye)
      for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.list; do
        [ -f "$f" ] || continue
        if grep -Eq 'bullseye/updates|bullseye-backports' "$f"; then
          backup_apt_file "$f"
          sed -i -E 's#bullseye/updates#bullseye-security#g; /^[[:space:]]*deb(-src)?[[:space:]].*[[:space:]]bullseye-backports([[:space:]]|$)/ s#^#\# disabled by proxy-installer: #' "$f"
          changed=1
        fi
      done
      ;;
  esac
  [ "$changed" -eq 1 ] && emit log 18 "已修复 Debian bullseye APT 源：bullseye/updates -> bullseye-security，并禁用已关闭的 bullseye-backports"
}
disable_apt_suite(){
  suite="$1"
  [ -n "$suite" ] || return 0
  safe_suite="$(echo "$suite" | sed 's/[.[\*^$()+?{}|]/\\&/g; s#/#\\/#g')"
  for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.list; do
    [ -f "$f" ] || continue
    if grep -Eq "^[[:space:]]*deb(-src)?[[:space:]].*[[:space:]]$safe_suite([[:space:]]|$)" "$f"; then
      backup_apt_file "$f"
      sed -i -E "/^[[:space:]]*deb(-src)?[[:space:]].*[[:space:]]$safe_suite([[:space:]]|$)/ s#^#\# disabled by proxy-installer: #" "$f"
      emit log 18 "已临时禁用无 Release 文件的 APT suite: $suite"
    fi
  done
}
disable_no_release_suites_from_log(){
  log_file="$1"
  [ -f "$log_file" ] || return 0
  sed -n "s/.*The repository '\([^']*\)'.*/\1/p" "$log_file" | awk '{print $(NF-1)}' | sort -u | while IFS= read -r suite; do disable_apt_suite "$suite"; done
}
run_apt_update(){
  log_file=/tmp/proxy-installer-apt-update.log
  apt-get update -y >"$log_file" 2>&1 && return 0
  if grep -Eq 'bullseye/updates|bullseye-backports|does not have a Release file' "$log_file"; then
    emit log 18 "检测到 APT 源异常，正在自动修复并重试"
    repair_debian_apt_sources
    disable_no_release_suites_from_log "$log_file"
    apt-get update -y >"$log_file" 2>&1 && return 0
  fi
  emit_file 18 "$log_file"
  return 100
}
has_global_ipv6(){
  command -v ip >/dev/null 2>&1 || return 1
  ip -6 addr show scope global 2>/dev/null | grep -q 'inet6 '
}
curl_probe(){
  family="$1"; label="$2"; url="$3"
  tmp="/tmp/proxy-installer-probe.$$"
  if [ -n "$family" ]; then
    curl "$family" -fsSL --connect-timeout 6 --max-time 12 -A 'ProxyInstaller/1.0' -o "$tmp" "$url" >/dev/null 2>&1
  else
    curl -fsSL --connect-timeout 6 --max-time 12 -A 'ProxyInstaller/1.0' -o "$tmp" "$url" >/dev/null 2>&1
  fi
  code=$?
  if [ "$code" -eq 0 ]; then
    value="$(head -c 120 "$tmp" 2>/dev/null | tr '\n' ' ')"
    emit log 24 "网络探测 ${label} ${family:-auto}: ok ${value}"
    rm -f "$tmp"
    return 0
  fi
  emit log 24 "网络探测 ${label} ${family:-auto}: curl_${code}"
  rm -f "$tmp"
  return 1
}
test_network(){
  emit progress 24 "Testing network"
  command -v curl >/dev/null 2>&1 || { emit log 24 "网络探测跳过：curl 未安装"; return 0; }
  curl_probe "-4" "IPv4 出口" "https://api.ipify.org" || true
  curl_probe "-6" "IPv6 出口" "https://api64.ipify.org" || true
  curl_probe "" "GitHub" "https://github.com" || true
  curl_probe "" "sing-box.app" "https://sing-box.app" || true
  curl_probe "" "Cloudflare" "https://www.cloudflare.com/cdn-cgi/trace" || true
}
curl_to_file(){
  out="$1"; shift
  err="${out}.err"
  for url in "$@"; do
    for family in "-4" "-6" ""; do
      label="${family:-auto}"
      if [ -n "$family" ]; then
        curl "$family" -fL --retry 2 --retry-delay 1 --connect-timeout 10 --max-time 150 -A 'ProxyInstaller/1.0' -o "$out" "$url" 2>"$err"
      else
        curl -fL --retry 2 --retry-delay 1 --connect-timeout 10 --max-time 150 -A 'ProxyInstaller/1.0' -o "$out" "$url" 2>"$err"
      fi
      code=$?
      if [ "$code" -eq 0 ] && [ -s "$out" ]; then
        emit log 30 "下载成功 ${label}: $url"
        rm -f "$err"
        return 0
      fi
      msg="$(tr '\n' ' ' < "$err" 2>/dev/null | cut -c1-180)"
      emit log 30 "下载失败 ${label}: curl_${code} $url ${msg}"
      rm -f "$out"
    done
  done
  rm -f "$err"
  return 1
}
install_sing_box(){
  if command -v sing-box >/dev/null 2>&1; then
    emit log 30 "sing-box 已存在: $(command -v sing-box)"
    return 0
  fi
  tmp="$(mktemp -d /tmp/proxy-installer-singbox.XXXXXX 2>/dev/null || true)"
  [ -n "$tmp" ] || tmp="/tmp/proxy-installer-singbox-$$"
  mkdir -p "$tmp" || return 1

  if curl_to_file "$tmp/install.sh" "https://sing-box.app/install.sh"; then
    if sh "$tmp/install.sh" >/tmp/proxy-installer-singbox-install.log 2>&1; then
      command -v sing-box >/dev/null 2>&1 && { emit log 30 "官方安装脚本成功"; rm -rf "$tmp"; return 0; }
    fi
    emit_file 30 /tmp/proxy-installer-singbox-install.log
  fi

  arch="$(uname -m 2>/dev/null)"
  case "$arch" in
    x86_64|amd64) asset_arch="amd64" ;;
    aarch64|arm64) asset_arch="arm64" ;;
    armv7l|armv7*) asset_arch="armv7" ;;
    *) emit log 30 "未知架构，无法手动安装 sing-box: $arch"; rm -rf "$tmp"; return 1 ;;
  esac

  api="$tmp/release.json"
  if ! curl_to_file "$api" "https://api.github.com/repos/SagerNet/sing-box/releases/latest"; then
    emit log 30 "无法访问 GitHub Release API，sing-box 手动安装失败"
    rm -rf "$tmp"
    return 1
  fi
  asset_url="$(grep -Eo 'https://[^"]+sing-box-[^"]+-linux-'"$asset_arch"'\.tar\.gz' "$api" | head -n 1 || true)"
  if [ -z "$asset_url" ]; then
    emit log 30 "未在 GitHub Release 中找到 linux-${asset_arch} 资源"
    rm -rf "$tmp"
    return 1
  fi
  archive="$tmp/sing-box.tar.gz"
  curl_to_file "$archive" "$asset_url" || { rm -rf "$tmp"; return 1; }

  # SHA256 校验：下载 sha256sum.txt 并验证压缩包完整性
  checksum_url="$(dirname "$asset_url")/sha256sum.txt"
  if command -v sha256sum >/dev/null 2>&1 && curl_to_file "$tmp/sha256sum.txt" "$checksum_url"; then
    (cd "$tmp" && sha256sum -c --ignore-missing sha256sum.txt 2>/dev/null) || {
      emit log 30 "SHA256 校验失败，压缩包损坏或被篡改，已终止安装"
      rm -rf "$tmp"
      return 1
    }
    emit log 30 "SHA256 校验通过"
  fi

  tar -xzf "$archive" -C "$tmp" >/tmp/proxy-installer-singbox-tar.log 2>&1 || { emit_file 30 /tmp/proxy-installer-singbox-tar.log; rm -rf "$tmp"; return 1; }
  bin="$(find "$tmp" -type f -name sing-box -perm -111 2>/dev/null | head -n 1)"
  [ -n "$bin" ] || { emit log 30 "压缩包内未找到 sing-box 二进制"; rm -rf "$tmp"; return 1; }
  mkdir -p /usr/local/bin
  cp "$bin" /usr/local/bin/sing-box
  chmod 755 /usr/local/bin/sing-box
  rm -rf "$tmp"
  command -v sing-box >/dev/null 2>&1
}
ensure_singbox_service(){
  command -v systemctl >/dev/null 2>&1 || return 0
  if systemctl cat sing-box >/dev/null 2>&1; then
    return 0
  fi
  bin="$(command -v sing-box 2>/dev/null || true)"
  [ -n "$bin" ] || return 1
  cat >/etc/systemd/system/sing-box.service <<EOF
[Unit]
Description=sing-box service managed by Proxy Installer
Documentation=https://sing-box.sagernet.org
After=network-online.target nss-lookup.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$bin run -c /etc/sing-box/config.json
Restart=on-failure
RestartSec=10
LimitNOFILE=infinity
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload >/dev/null 2>&1 || true
}
emit progress 6 "Detecting package manager"
if [ -r /etc/os-release ]; then . /etc/os-release; else ID=unknown; fi
if command -v apt-get >/dev/null 2>&1; then PM=apt; elif command -v dnf >/dev/null 2>&1; then PM=dnf; elif command -v yum >/dev/null 2>&1; then PM=yum; elif command -v pacman >/dev/null 2>&1; then PM=pacman; elif command -v zypper >/dev/null 2>&1; then PM=zypper; else PM=unknown; fi
if ! command -v systemctl >/dev/null 2>&1; then emit error 8 "systemd is required"; exit 9; fi
emit progress 18 "Installing dependencies"
case "$PM" in
  apt) export DEBIAN_FRONTEND=noninteractive; repair_debian_apt_sources; run_apt_update || exit 100; apt-get install -y --no-install-recommends curl ca-certificates openssl tar iproute2 procps nginx >/tmp/proxy-installer-apt-install.log 2>&1 || { emit_file 18 /tmp/proxy-installer-apt-install.log; exit 101; } ;;
  dnf) dnf -y install curl ca-certificates openssl tar iproute procps-ng nginx >/dev/null ;;
  yum) yum -y install curl ca-certificates openssl tar iproute procps-ng nginx >/dev/null ;;
  pacman) pacman -Sy --needed --noconfirm curl ca-certificates openssl tar iproute2 procps-ng nginx >/dev/null ;;
  zypper) zypper --non-interactive refresh >/dev/null; zypper --non-interactive install -y curl ca-certificates openssl tar iproute2 procps nginx >/dev/null ;;
  *) emit error 18 "Unsupported package manager"; exit 10 ;;
esac
test_network
emit progress 30 "Installing sing-box"
install_sing_box || { emit error 34 "sing-box install failed：远端无法从 sing-box.app/GitHub 下载，请检查 VPS 的 IPv4/IPv6 出口和 DNS"; exit 11; }
ensure_singbox_service || { emit error 34 "sing-box systemd service 准备失败"; exit 11; }
emit progress 42 "Preparing directories"
mkdir -p /etc/proxy-installer /etc/sing-box /etc/nginx/conf.d /var/www/proxy-installer/%s
chmod 755 /etc/proxy-installer /var/www/proxy-installer /var/www/proxy-installer/%s
if [ ! -f /etc/proxy-installer/server.crt ] || [ ! -f /etc/proxy-installer/server.key ]; then
  openssl req -x509 -nodes -newkey rsa:2048 -keyout /etc/proxy-installer/server.key -out /etc/proxy-installer/server.crt -subj "/CN=%s" -days 3650 >/dev/null 2>&1
  chmod 600 /etc/proxy-installer/server.key
  chmod 644 /etc/proxy-installer/server.crt
fi
emit progress 52 "Checking ports"
systemctl stop sing-box 2>/dev/null || true
busy=0
for port in %s; do
  if (ss -lntup 2>/dev/null || true) | grep -Eq "[:.]$port([[:space:]]|$)"; then emit error 52 "Port $port is busy"; busy=1; fi
done
if [ "$busy" -eq 1 ]; then exit 12; fi
emit progress 64 "Writing config files"
write_b64 "%s" /etc/sing-box/config.json
write_b64 "%s" /etc/nginx/conf.d/proxy-installer.conf
write_b64 "%s" /var/www/proxy-installer/%s/shadowrocket
write_b64 "%s" /var/www/proxy-installer/%s/v2rayng
write_b64 "%s" /var/www/proxy-installer/%s/mihomo.yaml
write_b64 "%s" /var/www/proxy-installer/%s/sing-box.json
chmod 600 /etc/sing-box/config.json
chmod -R 644 /var/www/proxy-installer/%s/* 2>/dev/null || true
if has_global_ipv6; then
  emit log 64 "检测到全局 IPv6，sing-box 与 nginx 将启用双栈监听"
else
  sed -i 's/"listen": "::"/"listen": "0.0.0.0"/g' /etc/sing-box/config.json 2>/dev/null || true
  sed -i '/listen \[::\]:/d' /etc/nginx/conf.d/proxy-installer.conf 2>/dev/null || true
  emit log 64 "未检测到全局 IPv6，已自动降级为 IPv4 监听"
fi
emit progress 74 "Validating sing-box"
if ! sing-box check -c /etc/sing-box/config.json >/tmp/proxy-installer-singbox-check.log 2>&1; then
  while IFS= read -r line; do emit log 74 "$line"; done </tmp/proxy-installer-singbox-check.log
  emit error 74 "sing-box config validation failed"; exit 13
fi
emit progress 82 "Opening firewall"
if command -v ufw >/dev/null 2>&1; then for port in %s %d; do ufw allow "$port/tcp" >/dev/null 2>&1 || true; ufw allow "$port/udp" >/dev/null 2>&1 || true; done; fi
if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then for port in %s %d; do firewall-cmd --add-port="$port/tcp" >/dev/null 2>&1 || true; firewall-cmd --add-port="$port/udp" >/dev/null 2>&1 || true; firewall-cmd --permanent --add-port="$port/tcp" >/dev/null 2>&1 || true; firewall-cmd --permanent --add-port="$port/udp" >/dev/null 2>&1 || true; done; firewall-cmd --reload >/dev/null 2>&1 || true; fi
emit progress 88 "Starting nginx"
nginx -t >/tmp/proxy-installer-nginx-check.log 2>&1 || { while IFS= read -r line; do emit log 88 "$line"; done </tmp/proxy-installer-nginx-check.log; emit error 88 "nginx config invalid"; exit 14; }
systemctl enable nginx >/dev/null 2>&1 || true
systemctl restart nginx >/tmp/proxy-installer-nginx-service.log 2>&1 || { while IFS= read -r line; do emit log 88 "$line"; done </tmp/proxy-installer-nginx-service.log; emit error 88 "nginx failed"; exit 15; }
emit progress 94 "Starting sing-box"
systemctl enable sing-box >/dev/null 2>&1 || true
systemctl restart sing-box >/tmp/proxy-installer-singbox-service.log 2>&1 || { while IFS= read -r line; do emit log 94 "$line"; done </tmp/proxy-installer-singbox-service.log; journalctl -u sing-box --no-pager -n 40 2>/dev/null | while IFS= read -r line; do emit log 94 "$line"; done; emit error 94 "sing-box failed"; exit 16; }
systemctl is-active --quiet sing-box || { emit error 96 "sing-box is not active"; exit 17; }
emit result 100 "Services are running"
`,
		token, token, sni, portList,
		b64JSON(serverConfig), b64(nginxConfig),
		b64(files["raw"]), token,
		b64(files["raw"]), token,
		b64(files["mihomo"]), token,
		b64(files["singbox"]), token,
		token, portList, webPort, portList, webPort,
	), nil
}

func buildServerConfig(selected []string, ports map[string]int, nodeName, password, uuid, realityPrivate, realityShortID, sni string) map[string]any {
	tls := map[string]any{"enabled": true, "certificate_path": "/etc/proxy-installer/server.crt", "key_path": "/etc/proxy-installer/server.key"}
	var inbounds []map[string]any
	has := func(id string) bool {
		for _, item := range selected {
			if item == id {
				return true
			}
		}
		return false
	}
	if has("hy2") {
		inbounds = append(inbounds, map[string]any{"type": "hysteria2", "tag": "hy2-in", "listen": "::", "listen_port": portOrDefault(ports, "hy2", 8443), "users": []map[string]string{{"name": nodeName, "password": password}}, "tls": tls, "masquerade": "https://" + DefaultSNI + "/"})
	}
	if has("vless-reality") {
		inbounds = append(inbounds, map[string]any{
			"type":        "vless",
			"tag":         "vless-reality-in",
			"listen":      "::",
			"listen_port": portOrDefault(ports, "vless-reality", 443),
			"users":       []map[string]any{{"name": nodeName, "uuid": uuid, "flow": "xtls-rprx-vision"}},
			"tls": map[string]any{
				"enabled":     true,
				"server_name": sni,
				"reality": map[string]any{
					"enabled":     true,
					"handshake":   map[string]any{"server": sni, "server_port": 443},
					"private_key": realityPrivate,
					"short_id":    []string{realityShortID},
				},
			},
		})
	}
	if has("trojan") {
		inbounds = append(inbounds, map[string]any{"type": "trojan", "tag": "trojan-in", "listen": "::", "listen_port": portOrDefault(ports, "trojan", 8445), "users": []map[string]string{{"name": nodeName, "password": password}}, "tls": tls})
	}
	if has("ss") {
		inbounds = append(inbounds, map[string]any{"type": "shadowsocks", "tag": "ss-in", "listen": "::", "listen_port": portOrDefault(ports, "ss", 8388), "method": "aes-256-gcm", "password": password})
	}
	if has("vmess") {
		inbounds = append(inbounds, map[string]any{"type": "vmess", "tag": "vmess-in", "listen": "::", "listen_port": portOrDefault(ports, "vmess", 2083), "users": []map[string]any{{"name": nodeName, "uuid": uuid, "alterId": 0}}, "tls": tls})
	}
	if has("tuic") {
		inbounds = append(inbounds, map[string]any{"type": "tuic", "tag": "tuic-in", "listen": "::", "listen_port": portOrDefault(ports, "tuic", 8444), "users": []map[string]string{{"name": nodeName, "uuid": uuid, "password": password}}, "congestion_control": "bbr", "tls": tls})
	}
	return map[string]any{"log": map[string]any{"level": "warn", "timestamp": true}, "inbounds": inbounds, "outbounds": []map[string]any{{"type": "direct", "tag": "direct"}, {"type": "block", "tag": "block"}}, "route": map[string]any{"final": "direct"}}
}

func buildClientFiles(host string, config DeployConfig, name, password, uuid, realityPublic, realityShortID string) map[string]string {
	host = normalizeHostLiteral(host)
	uriHost := formatHostForURI(host)
	sni := safeDomain(config.SNI, DefaultSNI)
	raw := []string{}
	has := func(id string) bool {
		for _, item := range config.Selected {
			if item == id {
				return true
			}
		}
		return false
	}
	if has("hy2") {
		raw = append(raw, fmt.Sprintf("hysteria2://%s@%s:%d/?insecure=1&sni=%s#%s", urlEsc(password), uriHost, publicPortOrDefault(config, "hy2", 8443), urlEsc(sni), urlEsc(name+"-HY2")))
	}
	if has("vless-reality") {
		raw = append(raw, fmt.Sprintf("vless://%s@%s:%d?encryption=none&flow=xtls-rprx-vision&security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&type=tcp#%s", uuid, uriHost, publicPortOrDefault(config, "vless-reality", 443), urlEsc(sni), urlEsc(realityPublic), urlEsc(realityShortID), urlEsc(name+"-Reality")))
	}
	if has("trojan") {
		raw = append(raw, fmt.Sprintf("trojan://%s@%s:%d?security=tls&sni=%s&allowInsecure=1#%s", urlEsc(password), uriHost, publicPortOrDefault(config, "trojan", 8445), urlEsc(sni), urlEsc(name+"-Trojan")))
	}
	if has("ss") {
		ss := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("aes-256-gcm:%s@%s:%d", password, uriHost, publicPortOrDefault(config, "ss", 8388))))
		raw = append(raw, fmt.Sprintf("ss://%s#%s", ss, urlEsc(name+"-SS")))
	}
	if has("vmess") {
		vmess, _ := json.Marshal(map[string]string{"v": "2", "ps": name + "-VMess", "add": host, "port": strconv.Itoa(publicPortOrDefault(config, "vmess", 2083)), "id": uuid, "aid": "0", "scy": "auto", "net": "tcp", "type": "none", "host": "", "path": "", "tls": "tls", "sni": sni})
		raw = append(raw, "vmess://"+base64.StdEncoding.EncodeToString(vmess))
	}
	if has("tuic") {
		raw = append(raw, fmt.Sprintf("tuic://%s:%s@%s:%d?congestion_control=bbr&udp_relay_mode=native&sni=%s&allow_insecure=1#%s", uuid, urlEsc(password), uriHost, publicPortOrDefault(config, "tuic", 8444), urlEsc(sni), urlEsc(name+"-TUIC")))
	}
	rawText := strings.Join(raw, "\n")
	return map[string]string{
		"raw":     rawText,
		"mihomo":  buildMihomo(host, config, name, password, uuid, realityPublic, realityShortID),
		"singbox": buildSingboxClient(host, config, name, password, uuid, realityPublic, realityShortID),
	}
}

func buildNginxConfig(webPort int, token, rule string) string {
	if rule == "" {
		rule = DefaultSubRule
	}
	path := func(client string) string {
		out := strings.ReplaceAll(rule, "{token}", token)
		out = strings.ReplaceAll(out, "{client}", client)
		return safeLocationPath(out, token, client)
	}
	return fmt.Sprintf(`server {
    listen %d;
    listen [::]:%d;
    server_name _;
    charset utf-8;
    autoindex off;
    location = / { return 404; }
    location = %s { alias /var/www/proxy-installer/%s/shadowrocket; default_type text/plain; add_header Cache-Control "no-store" always; }
    location = %s { alias /var/www/proxy-installer/%s/v2rayng; default_type text/plain; add_header Cache-Control "no-store" always; }
    location = %s { alias /var/www/proxy-installer/%s/mihomo.yaml; default_type text/yaml; add_header Cache-Control "no-store" always; }
    location = %s { alias /var/www/proxy-installer/%s/sing-box.json; default_type application/json; add_header Cache-Control "no-store" always; }
    location / { return 404; }
}
`, webPort, webPort, path("shadowrocket"), token, path("v2rayng"), token, path("mihomo.yaml"), token, path("sing-box.json"), token)
}

func safeLocationPath(path, token, client string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/sub/" + token + "/" + client
	}
	path = strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '/' || r == '_' || r == '-' || r == '.' {
			return r
		}
		return -1
	}, path)
	// 阻断路径遍历序列
	if strings.Contains(path, "..") {
		path = "/sub/" + token + "/" + client
	}
	if path == "" || path == "/" {
		path = "/sub/" + token + "/" + client
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func buildMihomo(host string, config DeployConfig, name, password, uuid, realityPublic, realityShortID string) string {
	host = normalizeHostLiteral(host)
	var proxies []string
	var names []string
	for _, id := range config.Selected {
		switch id {
		case "hy2":
			names = append(names, name+"-HY2")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-HY2'\n    type: hysteria2\n    server: '%s'\n    port: %d\n    password: '%s'\n    sni: '%s'\n    skip-cert-verify: true", name, host, publicPortOrDefault(config, "hy2", 8443), password, safeDomain(config.SNI, DefaultSNI)))
		case "vless-reality":
			names = append(names, name+"-Reality")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-Reality'\n    type: vless\n    server: '%s'\n    port: %d\n    uuid: %s\n    network: tcp\n    tls: true\n    udp: true\n    flow: xtls-rprx-vision\n    servername: '%s'\n    reality-opts:\n      public-key: '%s'\n      short-id: '%s'\n    client-fingerprint: chrome", name, host, publicPortOrDefault(config, "vless-reality", 443), uuid, safeDomain(config.SNI, DefaultSNI), realityPublic, realityShortID))
		case "trojan":
			names = append(names, name+"-Trojan")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-Trojan'\n    type: trojan\n    server: '%s'\n    port: %d\n    password: '%s'\n    sni: '%s'\n    skip-cert-verify: true", name, host, publicPortOrDefault(config, "trojan", 8445), password, safeDomain(config.SNI, DefaultSNI)))
		case "ss":
			names = append(names, name+"-SS")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-SS'\n    type: ss\n    server: '%s'\n    port: %d\n    cipher: aes-256-gcm\n    password: '%s'", name, host, publicPortOrDefault(config, "ss", 8388), password))
		case "vmess":
			names = append(names, name+"-VMess")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-VMess'\n    type: vmess\n    server: '%s'\n    port: %d\n    uuid: %s\n    alterId: 0\n    cipher: auto\n    tls: true\n    servername: '%s'\n    skip-cert-verify: true", name, host, publicPortOrDefault(config, "vmess", 2083), uuid, safeDomain(config.SNI, DefaultSNI)))
		case "tuic":
			names = append(names, name+"-TUIC")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-TUIC'\n    type: tuic\n    server: '%s'\n    port: %d\n    uuid: %s\n    password: '%s'\n    sni: '%s'\n    skip-cert-verify: true\n    congestion-controller: bbr\n    udp-relay-mode: native", name, host, publicPortOrDefault(config, "tuic", 8444), uuid, password, safeDomain(config.SNI, DefaultSNI)))
		}
	}
	var groupItems []string
	for _, item := range names {
		groupItems = append(groupItems, "      - '"+item+"'")
	}
	groupItems = append(groupItems, "      - DIRECT")
	return "mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: warning\nipv6: true\n\nproxies:\n" + strings.Join(proxies, "\n") + "\n\nproxy-groups:\n  - name: PROXY\n    type: select\n    proxies:\n" + strings.Join(groupItems, "\n") + "\n\nrules:\n  - MATCH,PROXY\n"
}

func buildSingboxClient(host string, config DeployConfig, name, password, uuid, realityPublic, realityShortID string) string {
	return buildSingboxClientWithListen(host, config, name, password, uuid, realityPublic, realityShortID, 2080)
}

func nodeSpeedClientConfig(host string, config DeployConfig, name, password, uuid, realityPublic, realityShortID string, listenPort int) (string, string, string) {
	subURL := buildSubscriptionURL(host, config, "sing-box.json")
	client := http.Client{Timeout: 12 * time.Second}
	resp, err := client.Get(subURL)
	if err == nil && resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
			if readErr == nil && len(strings.TrimSpace(string(body))) > 0 {
				if cfg, rewriteErr := rewriteSingboxListenPort(body, listenPort); rewriteErr == nil {
					return cfg, "subscription", ""
				} else {
					return buildSingboxClientWithListen(host, config, name, password, uuid, realityPublic, realityShortID, listenPort), "generated", "订阅配置解析失败，已回退到本地推导配置: " + rewriteErr.Error()
				}
			}
			return buildSingboxClientWithListen(host, config, name, password, uuid, realityPublic, realityShortID, listenPort), "generated", "订阅端口返回空内容，已回退到本地推导配置"
		}
		return buildSingboxClientWithListen(host, config, name, password, uuid, realityPublic, realityShortID, listenPort), "generated", fmt.Sprintf("订阅请求 HTTP %d，已回退到本地推导配置", resp.StatusCode)
	}
	warning := "订阅请求失败，已回退到本地推导配置"
	if err != nil {
		warning += ": " + err.Error()
	}
	return buildSingboxClientWithListen(host, config, name, password, uuid, realityPublic, realityShortID, listenPort), "generated", warning
}

func rewriteSingboxListenPort(data []byte, listenPort int) (string, error) {
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	inbounds, _ := cfg["inbounds"].([]any)
	if len(inbounds) == 0 {
		cfg["inbounds"] = []map[string]any{{"type": "mixed", "listen": "127.0.0.1", "listen_port": listenPort}}
	} else {
		rewritten := false
		for _, inbound := range inbounds {
			item, ok := inbound.(map[string]any)
			if !ok {
				continue
			}
			if item["type"] == "mixed" || item["type"] == "socks" || item["type"] == "http" {
				item["listen"] = "127.0.0.1"
				item["listen_port"] = listenPort
				rewritten = true
				break
			}
		}
		if !rewritten {
			cfg["inbounds"] = append([]any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": listenPort}}, inbounds...)
		}
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func buildSingboxClientWithListen(host string, config DeployConfig, name, password, uuid, realityPublic, realityShortID string, listenPort int) string {
	host = normalizeHostLiteral(host)
	outbounds := []map[string]any{}
	for _, id := range config.Selected {
		switch id {
		case "hy2":
			outbounds = append(outbounds, map[string]any{"type": "hysteria2", "tag": name + "-HY2", "server": host, "server_port": publicPortOrDefault(config, "hy2", 8443), "password": password, "tls": map[string]any{"enabled": true, "server_name": safeDomain(config.SNI, DefaultSNI), "insecure": true}})
		case "vless-reality":
			outbounds = append(outbounds, map[string]any{"type": "vless", "tag": name + "-Reality", "server": host, "server_port": publicPortOrDefault(config, "vless-reality", 443), "uuid": uuid, "flow": "xtls-rprx-vision", "tls": map[string]any{"enabled": true, "server_name": safeDomain(config.SNI, DefaultSNI), "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}, "reality": map[string]any{"enabled": true, "public_key": realityPublic, "short_id": realityShortID}}})
		case "trojan":
			outbounds = append(outbounds, map[string]any{"type": "trojan", "tag": name + "-Trojan", "server": host, "server_port": publicPortOrDefault(config, "trojan", 8445), "password": password, "tls": map[string]any{"enabled": true, "server_name": safeDomain(config.SNI, DefaultSNI), "insecure": true}})
		case "ss":
			outbounds = append(outbounds, map[string]any{"type": "shadowsocks", "tag": name + "-SS", "server": host, "server_port": publicPortOrDefault(config, "ss", 8388), "method": "aes-256-gcm", "password": password})
		case "vmess":
			outbounds = append(outbounds, map[string]any{"type": "vmess", "tag": name + "-VMess", "server": host, "server_port": publicPortOrDefault(config, "vmess", 2083), "uuid": uuid, "security": "auto", "tls": map[string]any{"enabled": true, "server_name": safeDomain(config.SNI, DefaultSNI), "insecure": true}})
		case "tuic":
			outbounds = append(outbounds, map[string]any{"type": "tuic", "tag": name + "-TUIC", "server": host, "server_port": publicPortOrDefault(config, "tuic", 8444), "uuid": uuid, "password": password, "congestion_control": "bbr", "udp_relay_mode": "native", "tls": map[string]any{"enabled": true, "server_name": safeDomain(config.SNI, DefaultSNI), "insecure": true}})
		}
	}
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})
	data, _ := json.MarshalIndent(map[string]any{"log": map[string]string{"level": "warn"}, "inbounds": []map[string]any{{"type": "mixed", "listen": "127.0.0.1", "listen_port": listenPort}}, "outbounds": outbounds, "route": map[string]any{"final": outbounds[0]["tag"], "auto_detect_interface": true}}, "", "  ")
	return string(data)
}

func parseKeyValue(text string) map[string]string {
	kv := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 {
			kv[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	return kv
}

func parseIPQualitySources(text string) (map[string]any, map[string]string) {
	raw := map[string]any{}
	checks := map[string]any{}
	sourceErrors := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "SOURCE="):
			parts := strings.SplitN(strings.TrimPrefix(line, "SOURCE="), "|", 2)
			if len(parts) != 2 {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil {
				sourceErrors[parts[0]] = "base64_decode_failed"
				continue
			}
			var parsed any
			if err := json.Unmarshal(data, &parsed); err != nil {
				sourceErrors[parts[0]] = "json_parse_failed"
				continue
			}
			raw[parts[0]] = parsed
		case strings.HasPrefix(line, "SOURCE_TEXT="):
			parts := strings.SplitN(strings.TrimPrefix(line, "SOURCE_TEXT="), "|", 2)
			if len(parts) != 2 {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil {
				sourceErrors[parts[0]] = "base64_decode_failed"
				continue
			}
			if looksLikeHTML(string(data)) {
				sourceErrors[parts[0]] = "html_response"
				continue
			}
			raw[parts[0]] = parseLooseTextSource(parts[0], string(data))
		case strings.HasPrefix(line, "SOURCE_ERROR="):
			parts := strings.SplitN(strings.TrimPrefix(line, "SOURCE_ERROR="), "|", 2)
			if len(parts) == 2 {
				sourceErrors[parts[0]] = parts[1]
			}
		case strings.HasPrefix(line, "CHECK="):
			parts := strings.SplitN(strings.TrimPrefix(line, "CHECK="), "|", 3)
			if len(parts) != 3 {
				continue
			}
			checks[parts[0]] = map[string]string{"status": parts[1], "detail": parts[2]}
		}
	}
	if len(checks) > 0 {
		raw["checks"] = checks
	}
	return raw, sourceErrors
}

func parseLooseTextSource(name, text string) map[string]string {
	text = strings.TrimSpace(stripANSI(text))
	out := map[string]string{}
	lines := strings.Split(text, "\n")
	if name == "ping0" {
		var values []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				values = append(values, line)
			}
		}
		names := []string{"ip", "location", "asn", "org"}
		for i, value := range values {
			if i < len(names) {
				out[names[i]] = value
			} else {
				out[fmt.Sprintf("line%d", i)] = value
			}
		}
		return out
	}
	hasPairs := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			hasPairs = true
		}
	}
	if hasPairs {
		return out
	}
	for index, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out[fmt.Sprintf("line%d", index)] = line
		}
	}
	return out
}

func buildQualityReport(raw map[string]any, errors map[string]string) (map[string]any, []map[string]any, []map[string]any) {
	sites := []map[string]any{
		buildQualitySite("ippure", "IPPure", "https://ippure.com/", raw["ippure"], errors["ippure"], []qualityField{
			{"IP", "ip"},
			{"FraudScore", "fraudScore"},
			{"ASN", "asn"},
			{"AS 组织", "asOrganization"},
			{"国家", "country"},
			{"地区", "region"},
			{"城市", "city"},
			{"住宅 IP", "isResidential"},
			{"广播 IP", "isBroadcast"},
		}),
		buildQualitySite("ping0", "ping0", "https://ping0.cc/", raw["ping0"], errors["ping0"], []qualityField{
			{"IP", "ip"},
			{"位置", "location"},
			{"ASN", "asn"},
			{"组织", "org"},
		}),
		buildQualitySite("iplark", "IPLark", "https://iplark.com/", raw["iplark"], errors["iplark"], []qualityField{
			{"IP", "ip"},
			{"地区", "loc"},
			{"边缘节点", "colo"},
			{"HTTP", "http"},
			{"TLS", "tls"},
			{"SNI", "sni"},
			{"WARP", "warp"},
			{"Gateway", "gateway"},
			{"RBI", "rbi"},
			{"密钥交换", "kex"},
			{"纯文本结果", "line0"},
		}),
	}
	success := 0
	primaryIP := ""
	for _, site := range sites {
		if site["status"] == "success" {
			success++
		}
		if primaryIP == "" {
			if ip, ok := site["ip"].(string); ok && ip != "" {
				primaryIP = ip
			}
		}
	}
	sections := buildQualitySections(raw, errors)
	checkTotal := 0
	checkOK := 0
	flat := flattenAny(raw)
	purityPercent := -1
	puritySource := ""
	if scoreText := firstValue(flat, "ippure.fraudScore"); scoreText != "" {
		if score, err := strconv.Atoi(strings.TrimSpace(scoreText)); err == nil {
			if score < 0 {
				score = 0
			}
			if score > 100 {
				score = 100
			}
			purityPercent = 100 - score
			puritySource = "IPPure FraudScore"
		}
	}
	for key, value := range flat {
		if strings.HasPrefix(strings.ToLower(key), "checks.") && strings.HasSuffix(strings.ToLower(key), ".status") {
			checkTotal++
			if value == "ok" || value == "open" || value == "clean" {
				checkOK++
			}
		}
	}
	return map[string]any{
		"sourceSuccess": success,
		"sourceFailed":  len(sites) - success,
		"sourceTotal":   len(sites),
		"primaryIP":     primaryIP,
		"headline":      fmt.Sprintf("%d/%d 来源成功", success, len(sites)),
		"checkOK":       checkOK,
		"checkTotal":    checkTotal,
		"moduleTotal":   len(sections),
		"purityPercent": purityPercent,
		"puritySource":  puritySource,
	}, sites, sections
}

type qualityField struct {
	Label string
	Key   string
}

func buildQualitySite(id, name, siteURL string, data any, errText string, fields []qualityField) map[string]any {
	site := map[string]any{
		"id":      id,
		"name":    name,
		"url":     siteURL,
		"status":  "failed",
		"metric":  "--",
		"summary": "未拿到结果",
		"rows":    []map[string]string{},
	}
	if errText != "" {
		site["error"] = errText
		site["summary"] = errText
		return site
	}
	if data == nil {
		site["error"] = "no_response"
		site["summary"] = "无响应"
		return site
	}
	flat := map[string]string{}
	flattenJSON("", data, flat)
	var rows []map[string]string
	for _, field := range fields {
		if value := strings.TrimSpace(flat[field.Key]); value != "" {
			rows = append(rows, map[string]string{"label": field.Label, "value": value})
		}
	}
	if len(rows) == 0 {
		for _, key := range sortedKeys(flat) {
			if value := strings.TrimSpace(flat[key]); value != "" {
				rows = append(rows, map[string]string{"label": key, "value": value})
			}
		}
	}
	site["status"] = "success"
	site["rows"] = rows
	ip := firstValue(flat, "ip", "query")
	site["ip"] = ip
	switch id {
	case "ippure":
		score := firstValue(flat, "fraudScore")
		if score != "" {
			site["metric"] = "Fraud " + score
		}
		site["summary"] = compactJoin(firstValue(flat, "country"), firstValue(flat, "city"), firstValue(flat, "asOrganization"))
	case "ping0":
		site["metric"] = firstValue(flat, "asn")
		site["summary"] = compactJoin(firstValue(flat, "location"), firstValue(flat, "org"))
	case "iplark":
		site["metric"] = compactJoin(firstValue(flat, "colo"), firstValue(flat, "loc"))
		site["summary"] = compactJoin(firstValue(flat, "http"), firstValue(flat, "tls"), "WARP "+firstValue(flat, "warp"))
	}
	if site["metric"] == "" {
		site["metric"] = ip
	}
	if site["summary"] == "" {
		site["summary"] = "已返回结果"
	}
	return site
}

func buildQualitySections(raw map[string]any, errors map[string]string) []map[string]any {
	flat := flattenAny(raw)
	row := func(label, value, source, status string) map[string]string {
		value = strings.TrimSpace(value)
		if value == "" {
			value = "-"
		}
		if status == "" {
			status = "ok"
		}
		return map[string]string{"label": label, "value": value, "source": source, "status": status}
	}
	check := func(key string) (string, string) {
		status := flat["checks."+key+".status"]
		detail := flat["checks."+key+".detail"]
		if status == "" && detail == "" {
			return "-", "skip"
		}
		return compactJoin(status, detail), status
	}
	dnsListed := 0
	dnsTotal := 0
	for key, value := range flat {
		if strings.HasPrefix(key, "checks.dnsbl.") && strings.HasSuffix(key, ".status") {
			dnsTotal++
			if value == "listed" {
				dnsListed++
			}
		}
	}
	dnsStatus := "skip"
	if dnsTotal > 0 {
		if dnsListed > 0 {
			dnsStatus = "fail"
		} else {
			dnsStatus = "ok"
		}
	}
	sourceStatus := func(name string) string {
		if errors[name] != "" {
			return "fail"
		}
		if raw[name] != nil {
			return "ok"
		}
		return "skip"
	}
	sourceValue := func(name string) string {
		if errors[name] != "" {
			return errors[name]
		}
		if raw[name] != nil {
			return "已返回"
		}
		return "-"
	}

	basic := []map[string]string{
		row("公网 IPv4", firstValue(flat, "checks.base.public_ip.detail", "ippure.ip", "ping0.ip", "iplark.ip", "ip-api.query", "ipinfo.ip", "ipapi.ip", "ipwhois.ip", "dbip.ipAddress"), "多源", "ok"),
		row("国家/城市", compactJoin(firstValue(flat, "ippure.country", "ip-api.country", "ipinfo.country", "ipapi.country_name", "ipwhois.country"), firstValue(flat, "ippure.city", "ip-api.city", "ipapi.city", "ipwhois.city")), "IPPure / ip-api", sourceStatus("ippure")),
		row("ASN/组织", compactJoin(firstValue(flat, "ippure.asn", "ip-api.as", "ping0.asn"), firstValue(flat, "ippure.asOrganization", "ipinfo.org", "ping0.org", "ip-api.org")), "IPPure / ping0", sourceStatus("ping0")),
		row("Cloudflare 边缘", compactJoin(firstValue(flat, "iplark.colo", "cloudflare.colo"), firstValue(flat, "iplark.loc", "cloudflare.loc")), "IPLark / Cloudflare", sourceStatus("iplark")),
		row("时区", firstValue(flat, "ip-api.timezone", "ipwhois.timezone.id", "ipinfo.timezone"), "ip-api / ipwhois", sourceStatus("ip-api")),
		row("坐标", compactJoin(firstValue(flat, "ip-api.lat", "ipwhois.latitude"), firstValue(flat, "ip-api.lon", "ipwhois.longitude"), firstValue(flat, "ipinfo.loc")), "ip-api / ipinfo", sourceStatus("ip-api")),
		row("地区代码", compactJoin(firstValue(flat, "ip-api.countryCode", "ipapi.country_code", "ipwhois.country_code"), firstValue(flat, "ip-api.regionName", "ipapi.region", "dbip.stateProv")), "ip-api / ipapi", sourceStatus("ip-api")),
		row("邮编", firstValue(flat, "ip-api.zip", "ipapi.postal", "ipwhois.postal"), "ip-api / ipapi", sourceStatus("ip-api")),
		row("运营商", firstValue(flat, "ip-api.isp", "ipapi.org", "ipwhois.connection.isp"), "ip-api / ipapi", sourceStatus("ip-api")),
		row("反查/主机名", firstValue(flat, "ip-api.reverse", "ipinfo.hostname"), "ip-api / ipinfo", sourceStatus("ip-api")),
	}

	ipType := []map[string]string{
		row("住宅 IP", firstValue(flat, "ippure.isResidential"), "IPPure", sourceStatus("ippure")),
		row("广播 IP", firstValue(flat, "ippure.isBroadcast"), "IPPure", sourceStatus("ippure")),
		row("Proxy", firstValue(flat, "ip-api.proxy"), "ip-api", sourceStatus("ip-api")),
		row("Hosting", firstValue(flat, "ip-api.hosting"), "ip-api", sourceStatus("ip-api")),
		row("Mobile", firstValue(flat, "ip-api.mobile"), "ip-api", sourceStatus("ip-api")),
		row("ISP", firstValue(flat, "ip-api.isp", "ipapi.org", "ipwhois.connection.isp"), "ip-api / ipapi", sourceStatus("ip-api")),
	}

	risk := []map[string]string{
		row("IPPure FraudScore", firstValue(flat, "ippure.fraudScore"), "IPPure", sourceStatus("ippure")),
		row("Scamalytics", sourceValue("scamalytics"), "Scamalytics", sourceStatus("scamalytics")),
		row("ip-api Proxy/Hosting", compactJoin("proxy="+firstValue(flat, "ip-api.proxy"), "hosting="+firstValue(flat, "ip-api.hosting")), "ip-api", sourceStatus("ip-api")),
		row("db-ip", compactJoin(firstValue(flat, "dbip.ipAddress"), firstValue(flat, "dbip.countryName"), firstValue(flat, "dbip.stateProv")), "DB-IP Free", sourceStatus("dbip")),
	}

	factors := []map[string]string{
		row("DNSBL 黑名单", fmt.Sprintf("%d listed / %d checked", dnsListed, dnsTotal), "DNSBL", dnsStatus),
		row("SMTP 25", func() string { v, _ := check("mail.gmail"); return v }(), "Gmail MX", func() string { _, s := check("mail.gmail"); return s }()),
		row("WARP", firstValue(flat, "iplark.warp", "cloudflare.warp"), "IPLark / Cloudflare", sourceStatus("iplark")),
		row("Gateway", firstValue(flat, "iplark.gateway"), "IPLark", sourceStatus("iplark")),
		row("HTTP/TLS 指纹", compactJoin(firstValue(flat, "iplark.http", "cloudflare.http"), firstValue(flat, "iplark.tls")), "IPLark", sourceStatus("iplark")),
	}

	streams := []map[string]string{}
	for _, item := range []struct{ key, label, source string }{
		{"stream.netflix", "Netflix", "netflix.com"},
		{"stream.youtube", "YouTube", "youtube.com"},
		{"stream.disneyplus", "Disney+", "disneyplus.com"},
		{"stream.tiktok", "TikTok", "tiktok.com"},
		{"stream.reddit", "Reddit", "reddit.com"},
		{"ai.openai", "OpenAI API", "api.openai.com"},
		{"ai.chatgpt", "ChatGPT", "chat.openai.com"},
	} {
		value, status := check(item.key)
		streams = append(streams, row(item.label, value, item.source, status))
	}

	mails := []map[string]string{}
	for _, item := range []struct{ key, label, source string }{
		{"mail.gmail", "Gmail", "gmail-smtp-in.l.google.com"},
		{"mail.outlook", "Outlook", "outlook.com"},
		{"mail.yahoo", "Yahoo", "yahoodns.net"},
		{"mail.apple", "iCloud", "icloud.com"},
		{"mail.qq", "QQ Mail", "qq.com"},
		{"mail.mailru", "Mail.ru", "mail.ru"},
		{"mail.aol", "AOL", "aol.com"},
		{"mail.gmx", "GMX", "gmx.net"},
		{"mail.mailcom", "Mail.com", "mail.com"},
	} {
		value, status := check(item.key)
		mails = append(mails, row(item.label, value, item.source, status))
	}

	return []map[string]any{
		{"id": "basic", "title": "基础信息", "icon": "info", "rows": basic},
		{"id": "type", "title": "IP 类型", "icon": "type", "rows": ipType},
		{"id": "risk", "title": "风险评分", "icon": "risk", "rows": risk},
		{"id": "factor", "title": "风险因子", "icon": "factor", "rows": factors},
		{"id": "stream", "title": "流媒体 / AI", "icon": "stream", "rows": streams},
		{"id": "mail", "title": "邮局 / 黑名单", "icon": "mail", "rows": mails},
	}
}

func flattenAny(value any) map[string]string {
	flat := map[string]string{}
	flattenJSON("", value, flat)
	return flat
}

func firstValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func compactJoin(values ...string) string {
	var parts []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " / ")
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func looksLikeHTML(text string) bool {
	probe := strings.ToLower(strings.TrimSpace(text))
	if len(probe) > 256 {
		probe = probe[:256]
	}
	return strings.Contains(probe, "<!doctype html") || strings.Contains(probe, "<html") || strings.Contains(probe, "<head")
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func parseFootprint(text string) map[string]any {
	var items []map[string]any
	var protocols []map[string]any
	services := map[string]map[string]string{}
	flags := map[string]string{}
	var logs []map[string]any
	present := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "ITEM="):
			parts := strings.SplitN(strings.TrimPrefix(line, "ITEM="), "|", 4)
			if len(parts) != 4 {
				continue
			}
			if parts[2] == "present" {
				present++
			}
			items = append(items, map[string]any{
				"label":  parts[0],
				"path":   parts[1],
				"status": parts[2],
				"size":   parts[3],
			})
		case strings.HasPrefix(line, "SERVICE="):
			parts := strings.SplitN(strings.TrimPrefix(line, "SERVICE="), "|", 3)
			if len(parts) != 3 {
				continue
			}
			services[parts[0]] = map[string]string{"active": parts[1], "enabled": parts[2]}
		case strings.HasPrefix(line, "FLAG="):
			parts := strings.SplitN(strings.TrimPrefix(line, "FLAG="), "|", 2)
			if len(parts) == 2 {
				flags[parts[0]] = parts[1]
			}
		case strings.HasPrefix(line, "LOGFILE="):
			parts := strings.SplitN(strings.TrimPrefix(line, "LOGFILE="), "|", 2)
			if len(parts) == 2 {
				logs = append(logs, map[string]any{"path": parts[0], "bytes": parts[1]})
			}
		case strings.HasPrefix(line, "PROTOCOL="):
			parts := strings.SplitN(strings.TrimPrefix(line, "PROTOCOL="), "|", 6)
			if len(parts) != 6 {
				continue
			}
			protocols = append(protocols, map[string]any{
				"id":                  parts[0],
				"label":               parts[1],
				"status":              parts[2],
				"configPresent":       parts[3] == "present",
				"subscriptionPresent": parts[4] == "present",
				"port":                parts[5],
			})
		}
	}
	protocolPresent := 0
	protocolPartial := 0
	for _, item := range protocols {
		switch item["status"] {
		case "complete":
			protocolPresent++
		case "partial":
			protocolPartial++
		}
	}
	return map[string]any{
		"ok":        true,
		"items":     items,
		"protocols": protocols,
		"services":  services,
		"flags":     flags,
		"logFiles":  logs,
		"summary": map[string]any{
			"present":         present,
			"tmpLogCount":     flags["tmpLogCount"],
			"singboxOwned":    flags["singboxOwned"] == "yes",
			"protocolPresent": protocolPresent,
			"protocolPartial": protocolPartial,
		},
		"checkedAt": time.Now().Format(time.RFC3339),
	}
}

func probeTCP(host string, port int, attempts int, timeout time.Duration) (int, string) {
	if attempts <= 0 {
		attempts = 1
	}
	var total time.Duration
	success := 0
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for i := 0; i < attempts; i++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			continue
		}
		_ = conn.Close()
		total += time.Since(start)
		success++
	}
	if success == 0 {
		return 0, "timeout"
	}
	return int((total / time.Duration(success)).Milliseconds()), "ok"
}

func probeHTTP(url string, timeout time.Duration) (int, string) {
	client := http.Client{Timeout: timeout}
	start := time.Now()
	resp, err := client.Get(url)
	if err != nil {
		return 0, "error"
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	status := "ok"
	if resp.StatusCode >= 400 {
		status = fmt.Sprintf("http_%d", resp.StatusCode)
	}
	return int(time.Since(start).Milliseconds()), status
}

func ensureLocalSingBox() (string, error) {
	if bin, err := findLocalSingBox(); err == nil {
		return bin, nil
	}
	bin, err := downloadLocalSingBox()
	if err != nil {
		return "", fmt.Errorf("本机未找到 sing-box，自动下载也失败: %w", err)
	}
	return bin, nil
}

func (a *App) installSingBoxViaUpload(client *ssh.Client) error {
	arch, err := detectRemoteSingBoxArch(client)
	if err != nil {
		return err
	}
	a.emit("log", 34, "远端架构: linux-"+arch)
	localPath, err := downloadLinuxSingBox(arch)
	if err != nil {
		return err
	}
	a.emit("log", 34, "本机已准备 sing-box: "+localPath)
	if err := uploadSingBoxBinary(client, localPath); err != nil {
		return err
	}
	a.emit("log", 34, "已上传 sing-box 到 /usr/local/bin/sing-box")
	return nil
}

func detectRemoteSingBoxArch(client *ssh.Client) (string, error) {
	result, err := runCommand(client, `uname -s; uname -m`, 10*time.Second)
	if err != nil {
		return "", err
	}
	lines := strings.Fields(strings.ToLower(result.Stdout))
	if len(lines) < 2 || lines[0] != "linux" {
		return "", fmt.Errorf("远端系统不是 Linux 或无法识别: %s", strings.TrimSpace(result.Stdout))
	}
	switch lines[1] {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	case "armv7l", "armv7":
		return "armv7", nil
	default:
		return "", fmt.Errorf("暂不支持远端架构: %s", lines[1])
	}
}

func downloadLinuxSingBox(arch string) (string, error) {
	dir, err := localSingBoxDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "linux-"+arch)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "sing-box")
	if stat, err := os.Stat(target); err == nil && !stat.IsDir() && stat.Size() > 1024*1024 {
		return target, nil
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/SagerNet/sing-box/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ProxyInstaller/1.0")
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("本机访问 GitHub Release API 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("本机 GitHub Release API HTTP %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&release); err != nil {
		return "", err
	}
	want := fmt.Sprintf("linux-%s.tar.gz", arch)
	assetURL := ""
	var checksumURL string
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, want) && !strings.Contains(name, "legacy") && asset.BrowserDownloadURL != "" {
			assetURL = asset.BrowserDownloadURL
		}
		if name == "sha256sum.txt" && asset.BrowserDownloadURL != "" {
			checksumURL = asset.BrowserDownloadURL
		}
		if assetURL != "" && checksumURL != "" {
			break
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf("未找到 sing-box Linux %s release 资源", arch)
	}
	archivePath := filepath.Join(dir, "sing-box-linux-"+arch+".tar.gz")
	if err := downloadFile(archivePath, assetURL, 120*1024*1024); err != nil {
		return "", err
	}
	defer os.Remove(archivePath)

	// SHA256 校验：下载 sha256sum.txt 并验证压缩包完整性
	if checksumURL != "" {
		if err := verifySHASums(archivePath, checksumURL); err != nil {
			return "", fmt.Errorf("SHA256 校验失败: %w （若持续失败可跳过校验手动安装 sing-box）", err)
		}
	}

	if err := extractSingBoxTarGz(archivePath, target); err != nil {
		return "", err
	}
	return target, nil
}

func uploadSingBoxBinary(client *ssh.Client, localPath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	script := `
set -e
tmp="$(mktemp /tmp/proxy-installer-singbox-upload.XXXXXX)"
base64 -d > "$tmp"
chmod 755 "$tmp"
mkdir -p /usr/local/bin
mv "$tmp" /usr/local/bin/sing-box
/usr/local/bin/sing-box version
`
	if err := session.Start("bash -lc " + shellQuote(script)); err != nil {
		return err
	}

	copyDone := make(chan error, 1)
	go func() {
		encoder := base64.NewEncoder(base64.StdEncoding, stdin)
		_, copyErr := io.Copy(encoder, file)
		closeErr := encoder.Close()
		stdinCloseErr := stdin.Close()
		if copyErr != nil {
			copyDone <- copyErr
			return
		}
		if closeErr != nil {
			copyDone <- closeErr
			return
		}
		copyDone <- stdinCloseErr
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- session.Wait() }()

	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-time.After(4 * time.Minute):
		_ = session.Signal(ssh.SIGKILL)
		return fmt.Errorf("上传 sing-box 超时")
	}
	copyErr := <-copyDone
	if waitErr != nil {
		return fmt.Errorf("远端安装上传的 sing-box 失败: %w stdout=%s stderr=%s", waitErr, trimForMessage(stdout.String(), 400), trimForMessage(stderr.String(), 400))
	}
	if copyErr != nil {
		return fmt.Errorf("上传 sing-box 数据失败: %w", copyErr)
	}
	return nil
}

type githubRelease struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func downloadLocalSingBox() (string, error) {
	if goruntime.GOOS != "windows" {
		return "", fmt.Errorf("当前系统暂不支持自动下载，请把 sing-box 放入 PATH")
	}
	arch := goruntime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("当前架构 %s 暂不支持自动下载", arch)
	}
	dir, err := localSingBoxDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "sing-box.exe")
	if stat, err := os.Stat(target); err == nil && !stat.IsDir() {
		return target, nil
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/SagerNet/sing-box/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ProxyInstaller/1.0")
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("GitHub release API HTTP %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&release); err != nil {
		return "", err
	}
	assetURL := ""
	want := fmt.Sprintf("windows-%s.zip", arch)
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, want) && !strings.Contains(name, "legacy") && asset.BrowserDownloadURL != "" {
			assetURL = asset.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf("未找到 sing-box Windows %s release 资产", arch)
	}
	zipPath := filepath.Join(dir, "sing-box-latest.zip")
	if err := downloadFile(zipPath, assetURL, 120*1024*1024); err != nil {
		return "", err
	}
	defer os.Remove(zipPath)
	if err := extractSingBoxZip(zipPath, target); err != nil {
		return "", err
	}
	return target, nil
}

// verifySHASums 从 checksumURL 下载 sha256sum.txt 并验证 archivePath 的完整性
func verifySHASums(archivePath, checksumURL string) error {
	archiveName := filepath.Base(archivePath)

	req, err := http.NewRequest(http.MethodGet, checksumURL, nil)
	if err != nil {
		return fmt.Errorf("创建校验和请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "ProxyInstaller/1.0")
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载 sha256sum.txt 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("sha256sum.txt HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return fmt.Errorf("读取 sha256sum.txt 失败: %w", err)
	}

	// 解析 sha256sum.txt，格式: "<sha256>  <filename>"
	wantChecksum := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, archiveName) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				wantChecksum = parts[0]
				break
			}
		}
	}
	if wantChecksum == "" {
		return fmt.Errorf("sha256sum.txt 中未找到 %s 的校验值", archiveName)
	}

	// 计算本地文件 SHA256
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("打开文件计算 SHA256 失败: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算 SHA256 失败: %w", err)
	}
	gotChecksum := hex.EncodeToString(h.Sum(nil))

	if !strings.EqualFold(gotChecksum, wantChecksum) {
		return fmt.Errorf("SHA256 不匹配: 期望 %s, 实际 %s", wantChecksum, gotChecksum)
	}
	return nil
}

func localSingBoxDir() (string, error) {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "proxy-installer", "runtime"), nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "proxy-installer", "runtime"), nil
}

func downloadFile(path, rawURL string, maxBytes int64) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ProxyInstaller/1.0")
	client := http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("下载 sing-box HTTP %d", resp.StatusCode)
	}
	tmp := path + ".download"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if n > maxBytes {
		_ = os.Remove(tmp)
		return fmt.Errorf("下载文件超过大小限制")
	}
	return os.Rename(tmp, path)
}

func extractSingBoxZip(zipPath, target string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if strings.EqualFold(filepath.Base(file.Name), "sing-box.exe") {
			src, err := file.Open()
			if err != nil {
				return err
			}
			defer src.Close()
			tmp := target + ".download"
			dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(dst, src)
			closeErr := dst.Close()
			if copyErr != nil {
				_ = os.Remove(tmp)
				return copyErr
			}
			if closeErr != nil {
				_ = os.Remove(tmp)
				return closeErr
			}
			return os.Rename(tmp, target)
		}
	}
	return fmt.Errorf("zip 内未找到 sing-box.exe")
}

func extractSingBoxTarGz(archivePath, target string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header == nil || header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "sing-box" {
			continue
		}
		tmp := target + ".download"
		dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(dst, tr)
		closeErr := dst.Close()
		if copyErr != nil {
			_ = os.Remove(tmp)
			return copyErr
		}
		if closeErr != nil {
			_ = os.Remove(tmp)
			return closeErr
		}
		return os.Rename(tmp, target)
	}
	return fmt.Errorf("tar.gz 内未找到 sing-box")
}

func findLocalSingBox() (string, error) {
	if bin, err := exec.LookPath("sing-box"); err == nil {
		return bin, nil
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, "sing-box.exe"), filepath.Join(dir, "sing-box"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "sing-box.exe"),
			filepath.Join(wd, "build", "bin", "sing-box.exe"),
			filepath.Join(wd, "build", "bin", "sing-box"),
		)
	}
	if dir, err := localSingBoxDir(); err == nil {
		candidates = append(candidates, filepath.Join(dir, "sing-box.exe"))
	}
	candidates = append(candidates,
		`C:\Program Files\sing-box\sing-box.exe`,
		`C:\Program Files (x86)\sing-box\sing-box.exe`,
	)
	for _, item := range candidates {
		if stat, err := os.Stat(item); err == nil && !stat.IsDir() {
			return item, nil
		}
	}
	return "", fmt.Errorf("本机未找到 sing-box，请把 sing-box.exe 放到程序同目录或加入 PATH 后再测试节点速度")
}

func filteredProxyEnv(values []string) []string {
	blocked := map[string]bool{
		"http_proxy":  true,
		"https_proxy": true,
		"all_proxy":   true,
		"no_proxy":    true,
	}
	filtered := make([]string, 0, len(values))
	for _, item := range values {
		key := strings.ToLower(strings.SplitN(item, "=", 2)[0])
		if blocked[key] {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func killProcessTree(process *os.Process) {
	if process == nil {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(process.Pid), "/T", "/F").Run()
	_ = process.Kill()
}

func getFreeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func waitLocalPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(180 * time.Millisecond)
	}
	return lastErr
}

func runHTTPDownloadSpeed(proxyAddr string, timeout time.Duration) (map[string]any, error) {
	transport := &http.Transport{}
	if proxyAddr != "" {
		parsed, err := url.Parse(proxyAddr)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	client := http.Client{Timeout: timeout, Transport: transport}
	start := time.Now()
	resp, err := client.Get("https://speed.cloudflare.com/__down?bytes=10000000")
	if err != nil {
		return nil, fmt.Errorf("节点下载测速失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("节点下载测速 HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(io.Discard, resp.Body)
	warning := ""
	if err != nil && n == 0 {
		return nil, err
	}
	if err != nil {
		warning = err.Error()
	}
	elapsed := time.Since(start)
	if elapsed <= 0 {
		elapsed = time.Millisecond
	}
	speedBytes := float64(n) / elapsed.Seconds()
	return map[string]any{
		"target":       "speed.cloudflare.com",
		"httpCode":     strconv.Itoa(resp.StatusCode),
		"timeSeconds":  elapsed.Seconds(),
		"downloadMbps": speedBytes * 8 / 1000 / 1000,
		"downloadMBps": speedBytes / 1000 / 1000,
		"bytes":        n,
		"warning":      warning,
	}, nil
}

func runHTTPProbe(proxyAddr string, timeout time.Duration) (map[string]any, error) {
	transport := &http.Transport{}
	if proxyAddr != "" {
		parsed, err := url.Parse(proxyAddr)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	client := http.Client{Timeout: timeout, Transport: transport}
	start := time.Now()
	resp, err := client.Get("https://www.cloudflare.com/cdn-cgi/trace")
	if err != nil {
		return nil, fmt.Errorf("节点代理连通性预检失败: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	return map[string]any{
		"target":      "www.cloudflare.com/cdn-cgi/trace",
		"httpCode":    strconv.Itoa(resp.StatusCode),
		"timeSeconds": time.Since(start).Seconds(),
	}, nil
}

func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

func flattenJSON(prefix string, value any, out map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flattenJSON(next, item, out)
		}
	case map[string]string:
		for key, item := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			out[next] = strings.TrimSpace(stripANSI(item))
		}
	case []any:
		for index, item := range typed {
			flattenJSON(fmt.Sprintf("%s[%d]", prefix, index), item, out)
		}
	case string:
		out[prefix] = strings.TrimSpace(stripANSI(typed))
	case float64:
		out[prefix] = strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		out[prefix] = strconv.FormatBool(typed)
	case nil:
	default:
		out[prefix] = fmt.Sprint(typed)
	}
}

func stripANSI(s string) string {
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

func trimForMessage(s string, max int) string {
	s = strings.TrimSpace(stripANSI(s))
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func buildSubscriptionURL(host string, config DeployConfig, client string) string {
	token := safeToken(config.Token)
	rule := config.Rule
	if rule == "" {
		rule = DefaultSubRule
	}
	path := strings.ReplaceAll(rule, "{token}", token)
	path = strings.ReplaceAll(path, "{client}", client)
	path = safeLocationPath(path, token, client)
	return fmt.Sprintf("http://%s:%d%s", formatHostForURL(host), publicWebPortOrDefault(config), path)
}

func normalizeHostLiteral(host string) string {
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

func formatHostForURI(host string) string {
	host = normalizeHostLiteral(host)
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func formatHostForURL(host string) string {
	host = normalizeHostLiteral(host)
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func protocolLabel(id string) string {
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

func usesUDPProtocol(selected []string) bool {
	for _, id := range selected {
		if id == "hy2" || id == "tuic" {
			return true
		}
	}
	return false
}

func realityKeys(seed string) (string, string, string) {
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

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func b64(s string) string        { return base64.StdEncoding.EncodeToString([]byte(s)) }
func b64JSON(v any) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return b64(string(data))
}
func portOrDefault(ports map[string]int, key string, def int) int {
	if ports != nil && ports[key] > 0 {
		return ports[key]
	}
	return def
}
func publicPortOrDefault(config DeployConfig, key string, def int) int {
	if config.PublicPorts != nil && config.PublicPorts[key] > 0 {
		return config.PublicPorts[key]
	}
	return portOrDefault(config.Ports, key, def)
}
func publicWebPortOrDefault(config DeployConfig) int {
	if config.PublicWebPort > 0 {
		return config.PublicWebPort
	}
	if config.WebPort > 0 {
		return config.WebPort
	}
	return DefaultWebPort
}
func selectedPorts(selected []string, ports map[string]int) []int {
	var out []int
	defaults := protocolDefaults()
	for _, id := range selected {
		def, ok := defaults[id]
		if !ok {
			continue
		}
		out = append(out, portOrDefault(ports, id, def))
	}
	return out
}
func protocolDefaults() map[string]int {
	return ProtocolDefaultPorts
}
func filterSupportedProtocols(selected []string) []string {
	defaults := protocolDefaults()
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
func safeToken(s string) string {
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
		return DefaultToken
	}
	return out
}
func safeName(s, fallback string) string {
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
func safeDomain(s, fallback string) string {
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

// ─── VPS Cost Management ─────────────────────────────────────────────────

const extraKeyCostV2 = "cost_v2"

type VPSInstance struct {
	ID            string  `json:"id"`
	VPSName       string  `json:"vpsName"`
	Host          string  `json:"host,omitempty"`
	CPU           int     `json:"cpu"`
	MemoryGB      float64 `json:"memory_gb"`
	DiskGB        int     `json:"disk_gb"`
	BandwidthMbps int     `json:"bandwidth_mbps"`
	TrafficGB     int     `json:"traffic_gb"`
	IPv4Count     int     `json:"ipv4Count"`
	Price         float64 `json:"price"`
	Currency      string  `json:"currency"`
	BillingCycle  string  `json:"billingCycle"`
	PurchaseDate  string  `json:"purchaseDate"`
	NextRenewal   string  `json:"nextRenewal"`
	ManualRenewal bool    `json:"manualRenewal"`
	ProviderName  string  `json:"providerName,omitempty"`
	ProviderURL   string  `json:"providerURL,omitempty"`
	PlanName      string  `json:"planName,omitempty"`
	OS            string  `json:"os,omitempty"`
	ProfileID     string  `json:"profileId,omitempty"`
	Notes         string  `json:"notes,omitempty"`
}

type CostV2Data struct {
	Instances []VPSInstance `json:"instances"`
}

func getCostV2(extra map[string]any) CostV2Data {
	raw, ok := extra[extraKeyCostV2]
	if !ok || raw == nil {
		return CostV2Data{Instances: []VPSInstance{}}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return CostV2Data{Instances: []VPSInstance{}}
	}
	var c CostV2Data
	if err := json.Unmarshal(data, &c); err != nil {
		return CostV2Data{Instances: []VPSInstance{}}
	}
	if c.Instances == nil {
		c.Instances = []VPSInstance{}
	}
	return c
}

func setCostV2(extra map[string]any, data CostV2Data) {
	extra[extraKeyCostV2] = data
}

func newInstanceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("inst_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("inst_%x", b)
}

func calcNextRenewal(purchaseDate, billingCycle string) string {
	if billingCycle == "lifetime" {
		return ""
	}
	purchase, err := time.Parse("2006-01-02", purchaseDate)
	if err != nil {
		return ""
	}
	var months int
	switch billingCycle {
	case "monthly":
		months = 1
	case "quarterly":
		months = 3
	case "semiannual":
		months = 6
	case "annual":
		months = 12
	default:
		return ""
	}
	now := time.Now()
	next := purchase
	for !next.After(now) {
		next = next.AddDate(0, months, 0)
	}
	return next.Format("2006-01-02")
}

func (a *App) loadCostV2() (CostV2Data, map[string]any, error) {
	state, err := a.LoadAppState()
	if err != nil {
		return CostV2Data{}, nil, err
	}
	extra, _ := state["extra"].(map[string]any)
	if extra == nil {
		extra = map[string]any{}
	}
	return getCostV2(extra), extra, nil
}

func (a *App) saveCostV2(extra map[string]any) error {
	_, err := a.SaveAppState(AppState{Extra: extra})
	return err
}

// GetCostV2Instances 获取所有 VPS 实例
func (a *App) GetCostV2Instances() (map[string]any, error) {
	c, _, err := a.loadCostV2()
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "instances": c.Instances}, nil
}

// SaveCostVPSInstance 新增或更新 VPS 实例
func (a *App) SaveCostVPSInstance(instance VPSInstance) (map[string]any, error) {
	c, extra, err := a.loadCostV2()
	if err != nil {
		return nil, err
	}
	if instance.ID == "" {
		instance.ID = newInstanceID()
	}
	if !instance.ManualRenewal {
		instance.NextRenewal = calcNextRenewal(instance.PurchaseDate, instance.BillingCycle)
	}
	found := false
	for i, v := range c.Instances {
		if v.ID == instance.ID {
			c.Instances[i] = instance
			found = true
			break
		}
	}
	if !found {
		c.Instances = append(c.Instances, instance)
	}
	setCostV2(extra, c)
	return map[string]any{"ok": true, "id": instance.ID}, a.saveCostV2(extra)
}

// DeleteCostVPSInstance 删除 VPS 实例
func (a *App) DeleteCostVPSInstance(id string) (map[string]any, error) {
	c, extra, err := a.loadCostV2()
	if err != nil {
		return nil, err
	}
	for i, v := range c.Instances {
		if v.ID == id {
			c.Instances = append(c.Instances[:i], c.Instances[i+1:]...)
			break
		}
	}
	setCostV2(extra, c)
	return map[string]any{"ok": true}, a.saveCostV2(extra)
}

// GetCostV2Summary 聚合统计
func (a *App) GetCostV2Summary() (map[string]any, error) {
	c, _, err := a.loadCostV2()
	if err != nil {
		return nil, err
	}

	providerSet := map[string]bool{}
	monthlyByCurrency := map[string]float64{}
	for _, inst := range c.Instances {
		if inst.ProviderName != "" {
			providerSet[inst.ProviderName] = true
		}
		if inst.BillingCycle == "lifetime" {
			continue
		}
		var months int
		switch inst.BillingCycle {
		case "monthly":
			months = 1
		case "quarterly":
			months = 3
		case "semiannual":
			months = 6
		case "annual":
			months = 12
		default:
			continue
		}
		monthly := inst.Price / float64(months)
		cur := inst.Currency
		if cur == "" {
			cur = "CNY"
		}
		monthlyByCurrency[cur] += monthly
	}

	return map[string]any{
		"ok":       true,
		"vendors":  len(providerSet),
		"total":    len(c.Instances),
		"monthly":  monthlyByCurrency,
	}, nil
}

// LinkVPSProfile 关联 SSH 配置到实例
func (a *App) LinkVPSProfile(instanceID, profileID string) (map[string]any, error) {
	c, extra, err := a.loadCostV2()
	if err != nil {
		return nil, err
	}
	found := false
	for i, v := range c.Instances {
		if v.ID == instanceID {
			c.Instances[i].ProfileID = profileID
			found = true
			break
		}
	}
	if !found {
		return map[string]any{"ok": false}, fmt.Errorf("实例 %s 不存在", instanceID)
	}
	setCostV2(extra, c)
	return map[string]any{"ok": true}, a.saveCostV2(extra)
}
