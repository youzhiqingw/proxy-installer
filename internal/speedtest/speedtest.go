// Package speedtest 负责节点测速和延迟检测功能。
// 从 app.go 提取，不依赖 Wails runtime。
package speedtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"proxy-installer/internal/config"
	"proxy-installer/internal/deploy"
	"proxy-installer/internal/sshclient"
)

// MeasureLatency 对各协议端口和订阅 URL 进行延迟探测
func MeasureLatency(sshClient *sshclient.Client, profile config.SSHProfile, cfg config.DeployConfig) (map[string]any, error) {
	host := deploy.NormalizeHostLiteral(profile.Host)
	if host == "" {
		return nil, fmt.Errorf("请输入 VPS 主机/IP")
	}
	if len(cfg.Selected) == 0 {
		cfg.Selected = []string{"ss"}
	}
	cfg.Selected = deploy.FilterSupportedProtocols(cfg.Selected)
	if cfg.WebPort == 0 {
		cfg.WebPort = config.DefaultWebPort
	}
	if cfg.PublicWebPort == 0 {
		cfg.PublicWebPort = cfg.WebPort
	}
	if cfg.Token == "" {
		cfg.Token = config.DefaultToken
	}
	if cfg.Rule == "" {
		cfg.Rule = config.DefaultSubRule
	}

	var items []map[string]any
	for _, id := range cfg.Selected {
		def := deploy.ProtocolDefaults()[id]
		port := deploy.PublicPortOrDefault(cfg, id, def)
		latency, status := probeTCP(host, port, 3, 4*time.Second)
		items = append(items, map[string]any{
			"kind":      "node",
			"protocol":  deploy.ProtocolLabel(id),
			"target":    net.JoinHostPort(host, strconv.Itoa(port)),
			"port":      port,
			"latencyMs": latency,
			"status":    status,
		})
	}

	subURL := deploy.BuildSubscriptionURL(host, cfg, "shadowrocket")
	latency, status := probeHTTP(subURL, 6*time.Second)
	items = append(items, map[string]any{
		"kind":      "subscription",
		"protocol":  "Shadowrocket 订阅",
		"target":    subURL,
		"port":      deploy.PublicWebPortOrDefault(cfg),
		"latencyMs": latency,
		"status":    status,
	})

	return map[string]any{"ok": true, "items": items, "checkedAt": time.Now().Format(time.RFC3339)}, nil
}

// RunSpeedTest 通过 SSH 在远端执行 curl 测速
func RunSpeedTest(sshClient *sshclient.Client, profile config.SSHProfile) (map[string]any, error) {
	client, err := sshClient.Connect(profile)
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
	result, err := sshclient.RunCommand(client, "bash -lc "+deploy.ShellQuote(script), 32*time.Second)
	if err != nil {
		return nil, err
	}
	kv := deploy.ParseKeyValue(result.Stdout)
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

// RunNodeSpeedTest 启动本地 sing-box 代理进行节点测速
// singBoxBin: sing-box 可执行文件路径，由调用方通过 deploy.EnsureLocalSingBox() 获取
func RunNodeSpeedTest(sshClient *sshclient.Client, profile config.SSHProfile, cfg config.DeployConfig, singBoxBin string) (map[string]any, error) {
	host := deploy.NormalizeHostLiteral(profile.Host)
	if host == "" {
		return nil, fmt.Errorf("请输入 VPS 主机/IP")
	}
	cfg.Selected = deploy.FilterSupportedProtocols(cfg.Selected)
	if len(cfg.Selected) == 0 {
		return nil, fmt.Errorf("请选择至少一个协议")
	}

	if cfg.Token == "" {
		cfg.Token = config.DefaultToken
	}
	if cfg.SNI == "" {
		cfg.SNI = config.DefaultSNI
	}

	nodeName := deploy.SafeName(cfg.NodeName, config.DefaultNodeName)
	token := deploy.SafeToken(cfg.Token)
	password := config.PasswordPrefix + token + config.PasswordSuffix
	uuid := deploy.StableUUID(token)
	_, realityPublic, realityShortID := deploy.RealityKeys(token)

	var protocols []map[string]any
	var best map[string]any
	for _, id := range cfg.Selected {
		item := runSingleNodeSpeed(singBoxBin, host, cfg, id, nodeName, password, uuid, realityPublic, realityShortID)
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
			"singBox":   singBoxBin,
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
		"singBox":        singBoxBin,
		"checkedAt":      time.Now().Format(time.RFC3339),
	}, nil
}

func runSingleNodeSpeed(bin, host string, cfg config.DeployConfig, protocolID, nodeName, password, uuid, realityPublic, realityShortID string) map[string]any {
	proxyPort, err := getFreeLocalPort()
	if err != nil {
		return nodeSpeedFailure(protocolID, cfg, fmt.Errorf("获取本地代理端口失败: %w", err), "", "")
	}

	single := cfg
	single.Selected = []string{protocolID}
	singCfg := deploy.BuildSingboxClientWithListen(host, single, nodeName, password, uuid, realityPublic, realityShortID, proxyPort)
	cfgSource := "generated-protocol"
	cfgWarning := ""

	dir, err := os.MkdirTemp("", "proxy-installer-singbox-*")
	if err != nil {
		return nodeSpeedFailure(protocolID, cfg, err, cfgSource, cfgWarning)
	}
	defer os.RemoveAll(dir)

	configPath := filepath.Join(dir, "client.json")
	if err := os.WriteFile(configPath, []byte(singCfg), 0600); err != nil {
		return nodeSpeedFailure(protocolID, cfg, err, cfgSource, cfgWarning)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "-c", configPath)
	cmd.Env = filteredProxyEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nodeSpeedFailure(protocolID, cfg, fmt.Errorf("启动本地 sing-box 失败: %w", err), cfgSource, cfgWarning)
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
			return nodeSpeedFailure(protocolID, cfg, fmt.Errorf("本地 sing-box 退出: %v %s", runErr, strings.TrimSpace(stderr.String())), cfgSource, cfgWarning)
		default:
		}
		return nodeSpeedFailure(protocolID, cfg, fmt.Errorf("本地代理未启动: %w %s", err, strings.TrimSpace(stderr.String())), cfgSource, cfgWarning)
	}

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	probe, err := runHTTPProbe(proxyURL, 12*time.Second)
	if err != nil {
		extra := ""
		if cfgWarning != "" {
			extra += "；" + cfgWarning
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			extra += "；sing-box: " + deploy.TrimForMessage(msg, 400)
		}
		if deploy.UsesUDPProtocol([]string{protocolID}) {
			extra += "；HY2/TUIC 需要公网 UDP 转发，如果是 NAT 机器，请确认公网 UDP 端口已映射到 VPS 内部端口"
		}
		return nodeSpeedFailure(protocolID, cfg, fmt.Errorf("%w%s", err, extra), cfgSource, cfgWarning)
	}
	speed, err := runHTTPDownloadSpeed(proxyURL, 60*time.Second)
	if err != nil {
		return nodeSpeedFailure(protocolID, cfg, err, cfgSource, cfgWarning)
	}
	speed["ok"] = true
	speed["via"] = "node"
	speed["protocolID"] = protocolID
	speed["protocol"] = deploy.ProtocolLabel(protocolID)
	speed["port"] = deploy.PublicPortOrDefault(cfg, protocolID, deploy.ProtocolDefaults()[protocolID])
	speed["status"] = "ok"
	speed["probe"] = probe
	speed["configSource"] = cfgSource
	speed["configWarning"] = cfgWarning
	speed["proxy"] = proxyURL
	speed["singBox"] = bin
	speed["checkedAt"] = time.Now().Format(time.RFC3339)
	return speed
}

func nodeSpeedFailure(protocolID string, cfg config.DeployConfig, err error, cfgSource, cfgWarning string) map[string]any {
	return map[string]any{
		"ok":            false,
		"via":           "node",
		"protocolID":    protocolID,
		"protocol":      deploy.ProtocolLabel(protocolID),
		"port":          deploy.PublicPortOrDefault(cfg, protocolID, deploy.ProtocolDefaults()[protocolID]),
		"status":        "failed",
		"error":         deploy.TrimForMessage(strings.TrimSpace(err.Error()), 800),
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

func probeHTTP(targetURL string, timeout time.Duration) (int, string) {
	client := http.Client{Timeout: timeout}
	start := time.Now()
	resp, err := client.Get(targetURL)
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
