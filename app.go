package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"

	"proxy-installer/internal/config"
	"proxy-installer/internal/deploy"
	"proxy-installer/internal/speedtest"
	"proxy-installer/internal/logger"
	"proxy-installer/internal/sshclient"
	"proxy-installer/internal/vault"
)

// ── 常量别名（实际定义在 internal/config/types.go）──────────────
// 使用别名避免与函数参数名 config (DeployConfig) 的命名冲突
const (
	DefaultToken   = config.DefaultToken
	DefaultSNI     = config.DefaultSNI
	DefaultSubRule = config.DefaultSubRule
	DefaultWebPort = config.DefaultWebPort
	DefaultNodeName = config.DefaultNodeName
	PasswordPrefix = config.PasswordPrefix
	PasswordSuffix = config.PasswordSuffix
)

var ProtocolDefaultPorts = config.ProtocolDefaultPorts

type App struct {
	ctx          context.Context
	mu           sync.Mutex
	allowQuit    bool
	sshClient    *sshclient.Client
	vault        *vault.Vault
}

// ── 类型已迁移至 internal/config 和 internal/sshclient ──────────
// SSHProfile, DeployConfig, DeployEvent, CommandResult, AppState → config 包
// HostKeyStore, HostKeyEntry, ErrNewHostKey, PendingHostKey → sshclient 包
// 类型别名（向后兼容，供本文件内其余代码使用）
type SSHProfile = config.SSHProfile
type DeployConfig = config.DeployConfig
type DeployEvent = config.DeployEvent
type CommandResult = config.CommandResult
type AppState = config.AppState

// NewApp 创建 App 实例并初始化 SSH 客户端和 Vault
func NewApp() *App {
	var store *sshclient.HostKeyStore
	dirs, _ := proxyDirs()
	if dirs != nil {
		knownHosts := filepath.Join(dirs["data"], "known_hosts.json")
		store, _ = sshclient.NewHostKeyStore(knownHosts)
	}
	v, _ := newAppVault()
	return &App{
		sshClient: sshclient.NewClient(store),
		vault:     v,
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
	root, _ := proxyDataRoot()
	if err := logger.Init(root); err != nil {
		fmt.Println("日志初始化失败:", err)
	}
	logger.Info("应用启动", "version", "1.1.0")
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	allowQuit := a.allowQuit
	a.mu.Unlock()
	if allowQuit {
		logger.Info("应用退出")
		return false
	}
	logger.Debug("窗口隐藏到系统托盘")
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
					p.Password = []byte(dec)
				} else {
					logger.Warn("密码解密失败", "profile", p.Name, "error", err.Error())
					p.Password = []byte(p.PasswordEncrypted)
				}
			}
		}
	}

	// T-08: 清除内存中的明文密码，防止通过前端返回泄露
	// 密码仅在 SSH 操作时按需解密使用
	for i := range state.Profiles {
		state.Profiles[i].ClearPassword()
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
			if len(p.Password) > 0 {
				pwStr := string(p.Password)
				enc, err := a.vault.Encrypt(pwStr)
				p.ClearPassword()
				pwStr = "" // drop reference
				if err == nil {
					p.PasswordEncrypted = enc
				}
			}
			// If password is empty but PasswordEncrypted exists, keep it (no change)
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
	logger.Info("测试连接", "host", profile.Host, "port", profile.Port)
	client, err := a.connect(profile)
	if err != nil {
		logger.Error("测试连接失败", "host", profile.Host, "error", err.Error())
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, `printf 'ok:%s@%s\n' "$(whoami)" "$(hostname)"`, 10*time.Second)
	if err != nil {
		logger.Error("测试连接命令失败", "host", profile.Host, "error", err.Error())
		return nil, err
	}
	msg := strings.TrimSpace(result.Stdout)
	logger.Info("测试连接成功", "host", profile.Host, "result", msg)
	return map[string]any{"ok": true, "message": msg}, nil
}

func (a *App) InspectVPS(profile SSHProfile) (map[string]any, error) {
	logger.Info("VPS 体检", "host", profile.Host)
	client, err := a.connect(profile)
	if err != nil {
		logger.Error("VPS 体检连接失败", "host", profile.Host, "error", err.Error())
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+deploy.ShellQuote(detectScript()), 45*time.Second)
	if err != nil {
		logger.Error("VPS 体检命令失败", "host", profile.Host, "error", err.Error())
		return nil, err
	}
	logger.Info("VPS 体检完成", "host", profile.Host)
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
	result, err := runCommand(client, "bash -lc "+deploy.ShellQuote(script), 20*time.Second)
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

func (a *App) RunIPQuality(profile SSHProfile) (map[string]any, error) {
	logger.Info("IP 质量检测", "host", profile.Host)
	client, err := a.connect(profile)
	if err != nil {
		logger.Error("IP 质量检测连接失败", "host", profile.Host, "error", err.Error())
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+deploy.ShellQuote(ipQualityScript()), 110*time.Second)
	if err != nil {
		logger.Error("IP 质量检测命令失败", "host", profile.Host, "error", err.Error())
		return nil, err
	}
	if result.Code != 0 {
		kv := deploy.ParseKeyValue(result.Stdout)
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
	logger.Info("扫描部署残留", "host", profile.Host)
	client, err := a.connect(profile)
	if err != nil {
		logger.Error("扫描残留连接失败", "host", profile.Host, "error", err.Error())
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+deploy.ShellQuote(footprintScanScript()), 35*time.Second)
	if err != nil {
		return nil, err
	}
	return parseFootprint(result.Stdout), nil
}

func (a *App) UninstallStarter(profile SSHProfile, removeRuntime bool) (map[string]any, error) {
	logger.Info("一键清理", "host", profile.Host, "removeRuntime", removeRuntime)
	client, err := a.connect(profile)
	if err != nil {
		logger.Error("一键清理连接失败", "host", profile.Host, "error", err.Error())
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+deploy.ShellQuote(uninstallScript(removeRuntime)), 90*time.Second)
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
	after, _ := runCommand(client, "bash -lc "+deploy.ShellQuote(footprintScanScript()), 35*time.Second)
	report := parseFootprint(after.Stdout)
	report["logs"] = logs
	report["ok"] = true
	return report, nil
}

func (a *App) CleanupSelectedFootprint(profile SSHProfile, protocolIDs []string, removeRuntime bool) (map[string]any, error) {
	logger.Info("选择性清理", "host", profile.Host, "protocols", protocolIDs, "removeRuntime", removeRuntime)
	ids := deploy.FilterSupportedProtocols(protocolIDs)
	if len(ids) == 0 {
		return nil, fmt.Errorf("请选择要清理的协议")
	}
	client, err := a.connect(profile)
	if err != nil {
		logger.Error("选择性清理连接失败", "host", profile.Host, "error", err.Error())
		return nil, err
	}
	defer client.Close()

	result, err := runCommand(client, "bash -lc "+deploy.ShellQuote(cleanupSelectedScript(ids, removeRuntime)), 90*time.Second)
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
	after, _ := runCommand(client, "bash -lc "+deploy.ShellQuote(footprintScanScript()), 35*time.Second)
	report := parseFootprint(after.Stdout)
	report["logs"] = logs
	report["ok"] = true
	return report, nil
}

// AcceptHostKey 接受并存储待确认的 HostKey，前端在用户确认后调用
func (a *App) AcceptHostKey(host string, port int) error {
	return a.sshClient.AcceptHostKey(host, port)
}

// ── 委托方法：部署 ──────────────────────────────────────────

func (a *App) StartDeploy(profile SSHProfile, config DeployConfig) (map[string]any, error) {
	logger.Info("开始部署", "host", profile.Host, "protocols", config.Selected, "sni", config.SNI)
	client, err := a.connect(profile)
	if err != nil {
		logger.Error("部署连接失败", "host", profile.Host, "error", err.Error())
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
	return deploy.Deploy(client, a.emit, profile, config)
}

// ── 委托方法：测速 ──────────────────────────────────────────

func (a *App) MeasureLatency(profile SSHProfile, config DeployConfig) (map[string]any, error) {
	return speedtest.MeasureLatency(a.sshClient, profile, config)
}

func (a *App) RunSpeedTest(profile SSHProfile) (map[string]any, error) {
	return speedtest.RunSpeedTest(a.sshClient, profile)
}

func (a *App) RunNodeSpeedTest(profile SSHProfile, config DeployConfig) (map[string]any, error) {
	singBoxBin, err := deploy.EnsureLocalSingBox()
	if err != nil {
		return nil, err
	}
	return speedtest.RunNodeSpeedTest(a.sshClient, profile, config, singBoxBin)
}

// connect 建立 SSH 连接（委托给 sshclient.Client）
func (a *App) connect(profile SSHProfile) (*ssh.Client, error) {
	return a.sshClient.Connect(profile)
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
	message = sanitizeLogMessage(message)
	if kind == "error" {
		logger.Error("部署事件", "kind", kind, "percent", percent, "message", message)
	} else if percent >= 95 || kind == "done" {
		logger.Info("部署事件", "kind", kind, "percent", percent, "message", message)
	} else {
		logger.Debug("部署事件", "kind", kind, "percent", percent, "message", message)
	}
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "deploy:event", DeployEvent{Type: kind, Percent: percent, Message: message})
}

// runStreaming 委托给 sshclient.RunStreaming，注入 emit 回调
func (a *App) runStreaming(client *ssh.Client, command string) (int, error) {
	return sshclient.RunStreaming(client, command, a.emit)
}

// runCommand 委托给 sshclient.RunCommand
func runCommand(client *ssh.Client, command string, timeout time.Duration) (CommandResult, error) {
	return sshclient.RunCommand(client, command, timeout)
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
	ids := strings.Join(deploy.FilterSupportedProtocols(protocolIDs), " ")
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
	text = strings.TrimSpace(deploy.StripANSI(text))
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
	flat := deploy.FlattenAny(raw)
	purityPercent := -1
	puritySource := ""
	if scoreText := deploy.FirstValue(flat, "ippure.fraudScore"); scoreText != "" {
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
	deploy.FlattenJSON("", data, flat)
	var rows []map[string]string
	for _, field := range fields {
		if value := strings.TrimSpace(flat[field.Key]); value != "" {
			rows = append(rows, map[string]string{"label": field.Label, "value": value})
		}
	}
	if len(rows) == 0 {
		for _, key := range deploy.SortedKeys(flat) {
			if value := strings.TrimSpace(flat[key]); value != "" {
				rows = append(rows, map[string]string{"label": key, "value": value})
			}
		}
	}
	site["status"] = "success"
	site["rows"] = rows
	ip := deploy.FirstValue(flat, "ip", "query")
	site["ip"] = ip
	switch id {
	case "ippure":
		score := deploy.FirstValue(flat, "fraudScore")
		if score != "" {
			site["metric"] = "Fraud " + score
		}
		site["summary"] = deploy.CompactJoin(deploy.FirstValue(flat, "country"), deploy.FirstValue(flat, "city"), deploy.FirstValue(flat, "asOrganization"))
	case "ping0":
		site["metric"] = deploy.FirstValue(flat, "asn")
		site["summary"] = deploy.CompactJoin(deploy.FirstValue(flat, "location"), deploy.FirstValue(flat, "org"))
	case "iplark":
		site["metric"] = deploy.CompactJoin(deploy.FirstValue(flat, "colo"), deploy.FirstValue(flat, "loc"))
		site["summary"] = deploy.CompactJoin(deploy.FirstValue(flat, "http"), deploy.FirstValue(flat, "tls"), "WARP "+deploy.FirstValue(flat, "warp"))
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
	flat := deploy.FlattenAny(raw)
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
		return deploy.CompactJoin(status, detail), status
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
		row("公网 IPv4", deploy.FirstValue(flat, "checks.base.public_ip.detail", "ippure.ip", "ping0.ip", "iplark.ip", "ip-api.query", "ipinfo.ip", "ipapi.ip", "ipwhois.ip", "dbip.ipAddress"), "多源", "ok"),
		row("国家/城市", deploy.CompactJoin(deploy.FirstValue(flat, "ippure.country", "ip-api.country", "ipinfo.country", "ipapi.country_name", "ipwhois.country"), deploy.FirstValue(flat, "ippure.city", "ip-api.city", "ipapi.city", "ipwhois.city")), "IPPure / ip-api", sourceStatus("ippure")),
		row("ASN/组织", deploy.CompactJoin(deploy.FirstValue(flat, "ippure.asn", "ip-api.as", "ping0.asn"), deploy.FirstValue(flat, "ippure.asOrganization", "ipinfo.org", "ping0.org", "ip-api.org")), "IPPure / ping0", sourceStatus("ping0")),
		row("Cloudflare 边缘", deploy.CompactJoin(deploy.FirstValue(flat, "iplark.colo", "cloudflare.colo"), deploy.FirstValue(flat, "iplark.loc", "cloudflare.loc")), "IPLark / Cloudflare", sourceStatus("iplark")),
		row("时区", deploy.FirstValue(flat, "ip-api.timezone", "ipwhois.timezone.id", "ipinfo.timezone"), "ip-api / ipwhois", sourceStatus("ip-api")),
		row("坐标", deploy.CompactJoin(deploy.FirstValue(flat, "ip-api.lat", "ipwhois.latitude"), deploy.FirstValue(flat, "ip-api.lon", "ipwhois.longitude"), deploy.FirstValue(flat, "ipinfo.loc")), "ip-api / ipinfo", sourceStatus("ip-api")),
		row("地区代码", deploy.CompactJoin(deploy.FirstValue(flat, "ip-api.countryCode", "ipapi.country_code", "ipwhois.country_code"), deploy.FirstValue(flat, "ip-api.regionName", "ipapi.region", "dbip.stateProv")), "ip-api / ipapi", sourceStatus("ip-api")),
		row("邮编", deploy.FirstValue(flat, "ip-api.zip", "ipapi.postal", "ipwhois.postal"), "ip-api / ipapi", sourceStatus("ip-api")),
		row("运营商", deploy.FirstValue(flat, "ip-api.isp", "ipapi.org", "ipwhois.connection.isp"), "ip-api / ipapi", sourceStatus("ip-api")),
		row("反查/主机名", deploy.FirstValue(flat, "ip-api.reverse", "ipinfo.hostname"), "ip-api / ipinfo", sourceStatus("ip-api")),
	}

	ipType := []map[string]string{
		row("住宅 IP", deploy.FirstValue(flat, "ippure.isResidential"), "IPPure", sourceStatus("ippure")),
		row("广播 IP", deploy.FirstValue(flat, "ippure.isBroadcast"), "IPPure", sourceStatus("ippure")),
		row("Proxy", deploy.FirstValue(flat, "ip-api.proxy"), "ip-api", sourceStatus("ip-api")),
		row("Hosting", deploy.FirstValue(flat, "ip-api.hosting"), "ip-api", sourceStatus("ip-api")),
		row("Mobile", deploy.FirstValue(flat, "ip-api.mobile"), "ip-api", sourceStatus("ip-api")),
		row("ISP", deploy.FirstValue(flat, "ip-api.isp", "ipapi.org", "ipwhois.connection.isp"), "ip-api / ipapi", sourceStatus("ip-api")),
	}

	risk := []map[string]string{
		row("IPPure FraudScore", deploy.FirstValue(flat, "ippure.fraudScore"), "IPPure", sourceStatus("ippure")),
		row("Scamalytics", sourceValue("scamalytics"), "Scamalytics", sourceStatus("scamalytics")),
		row("ip-api Proxy/Hosting", deploy.CompactJoin("proxy="+deploy.FirstValue(flat, "ip-api.proxy"), "hosting="+deploy.FirstValue(flat, "ip-api.hosting")), "ip-api", sourceStatus("ip-api")),
		row("db-ip", deploy.CompactJoin(deploy.FirstValue(flat, "dbip.ipAddress"), deploy.FirstValue(flat, "dbip.countryName"), deploy.FirstValue(flat, "dbip.stateProv")), "DB-IP Free", sourceStatus("dbip")),
	}

	factors := []map[string]string{
		row("DNSBL 黑名单", fmt.Sprintf("%d listed / %d checked", dnsListed, dnsTotal), "DNSBL", dnsStatus),
		row("SMTP 25", func() string { v, _ := check("mail.gmail"); return v }(), "Gmail MX", func() string { _, s := check("mail.gmail"); return s }()),
		row("WARP", deploy.FirstValue(flat, "iplark.warp", "cloudflare.warp"), "IPLark / Cloudflare", sourceStatus("iplark")),
		row("Gateway", deploy.FirstValue(flat, "iplark.gateway"), "IPLark", sourceStatus("iplark")),
		row("HTTP/TLS 指纹", deploy.CompactJoin(deploy.FirstValue(flat, "iplark.http", "cloudflare.http"), deploy.FirstValue(flat, "iplark.tls")), "IPLark", sourceStatus("iplark")),
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

type githubRelease struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// verifySHASums 从 checksumURL 下载 sha256sum.txt 并验证 archivePath 的完整性
func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
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
