package deploy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"proxy-installer/internal/config"
)

// BuildServerConfig 构建 sing-box 服务端配置（inbounds + outbounds + route）
func BuildServerConfig(selected []string, ports map[string]int, nodeName, password, uuid, realityPrivate, realityShortID, sni string) map[string]any {
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
		inbounds = append(inbounds, map[string]any{"type": "hysteria2", "tag": "hy2-in", "listen": "::", "listen_port": PortOrDefault(ports, "hy2", 8443), "users": []map[string]string{{"name": nodeName, "password": password}}, "tls": tls, "masquerade": "https://" + config.DefaultSNI + "/"})
	}
	if has("vless-reality") {
		inbounds = append(inbounds, map[string]any{
			"type":        "vless",
			"tag":         "vless-reality-in",
			"listen":      "::",
			"listen_port": PortOrDefault(ports, "vless-reality", 443),
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
		inbounds = append(inbounds, map[string]any{"type": "trojan", "tag": "trojan-in", "listen": "::", "listen_port": PortOrDefault(ports, "trojan", 8445), "users": []map[string]string{{"name": nodeName, "password": password}}, "tls": tls})
	}
	if has("ss") {
		inbounds = append(inbounds, map[string]any{"type": "shadowsocks", "tag": "ss-in", "listen": "::", "listen_port": PortOrDefault(ports, "ss", 8388), "method": "aes-256-gcm", "password": password})
	}
	if has("vmess") {
		inbounds = append(inbounds, map[string]any{"type": "vmess", "tag": "vmess-in", "listen": "::", "listen_port": PortOrDefault(ports, "vmess", 2083), "users": []map[string]any{{"name": nodeName, "uuid": uuid, "alterId": 0}}, "tls": tls})
	}
	if has("tuic") {
		inbounds = append(inbounds, map[string]any{"type": "tuic", "tag": "tuic-in", "listen": "::", "listen_port": PortOrDefault(ports, "tuic", 8444), "users": []map[string]string{{"name": nodeName, "uuid": uuid, "password": password}}, "congestion_control": "bbr", "tls": tls})
	}
	return map[string]any{"log": map[string]any{"level": "warn", "timestamp": true}, "inbounds": inbounds, "outbounds": []map[string]any{{"type": "direct", "tag": "direct"}, {"type": "block", "tag": "block"}}, "route": map[string]any{"final": "direct"}}
}

// BuildClientFiles 构建各客户端的订阅文件和配置文件内容
func BuildClientFiles(host string, cfg config.DeployConfig, name, password, uuid, realityPublic, realityShortID string) map[string]string {
	host = NormalizeHostLiteral(host)
	uriHost := FormatHostForURI(host)
	sni := SafeDomain(cfg.SNI, config.DefaultSNI)
	raw := []string{}
	has := func(id string) bool {
		for _, item := range cfg.Selected {
			if item == id {
				return true
			}
		}
		return false
	}
	if has("hy2") {
		raw = append(raw, fmt.Sprintf("hysteria2://%s@%s:%d/?insecure=1&sni=%s#%s", urlEsc(password), uriHost, PublicPortOrDefault(cfg, "hy2", 8443), urlEsc(sni), urlEsc(name+"-HY2")))
	}
	if has("vless-reality") {
		raw = append(raw, fmt.Sprintf("vless://%s@%s:%d?encryption=none&flow=xtls-rprx-vision&security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&type=tcp#%s", uuid, uriHost, PublicPortOrDefault(cfg, "vless-reality", 443), urlEsc(sni), urlEsc(realityPublic), urlEsc(realityShortID), urlEsc(name+"-Reality")))
	}
	if has("trojan") {
		raw = append(raw, fmt.Sprintf("trojan://%s@%s:%d?security=tls&sni=%s&allowInsecure=1#%s", urlEsc(password), uriHost, PublicPortOrDefault(cfg, "trojan", 8445), urlEsc(sni), urlEsc(name+"-Trojan")))
	}
	if has("ss") {
		ss := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("aes-256-gcm:%s@%s:%d", password, uriHost, PublicPortOrDefault(cfg, "ss", 8388))))
		raw = append(raw, fmt.Sprintf("ss://%s#%s", ss, urlEsc(name+"-SS")))
	}
	if has("vmess") {
		vmess, _ := json.Marshal(map[string]string{"v": "2", "ps": name + "-VMess", "add": host, "port": strconv.Itoa(PublicPortOrDefault(cfg, "vmess", 2083)), "id": uuid, "aid": "0", "scy": "auto", "net": "tcp", "type": "none", "host": "", "path": "", "tls": "tls", "sni": sni})
		raw = append(raw, "vmess://"+base64.StdEncoding.EncodeToString(vmess))
	}
	if has("tuic") {
		raw = append(raw, fmt.Sprintf("tuic://%s:%s@%s:%d?congestion_control=bbr&udp_relay_mode=native&sni=%s&allow_insecure=1#%s", uuid, urlEsc(password), uriHost, PublicPortOrDefault(cfg, "tuic", 8444), urlEsc(sni), urlEsc(name+"-TUIC")))
	}
	rawText := strings.Join(raw, "\n")
	return map[string]string{
		"raw":     rawText,
		"mihomo":  buildMihomo(host, cfg, name, password, uuid, realityPublic, realityShortID),
		"singbox": BuildSingboxClient(host, cfg, name, password, uuid, realityPublic, realityShortID),
	}
}

func buildMihomo(host string, cfg config.DeployConfig, name, password, uuid, realityPublic, realityShortID string) string {
	host = NormalizeHostLiteral(host)
	var proxies []string
	var names []string
	for _, id := range cfg.Selected {
		switch id {
		case "hy2":
			names = append(names, name+"-HY2")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-HY2'\n    type: hysteria2\n    server: '%s'\n    port: %d\n    password: '%s'\n    sni: '%s'\n    skip-cert-verify: true", name, host, PublicPortOrDefault(cfg, "hy2", 8443), password, SafeDomain(cfg.SNI, config.DefaultSNI)))
		case "vless-reality":
			names = append(names, name+"-Reality")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-Reality'\n    type: vless\n    server: '%s'\n    port: %d\n    uuid: %s\n    network: tcp\n    tls: true\n    udp: true\n    flow: xtls-rprx-vision\n    servername: '%s'\n    reality-opts:\n      public-key: '%s'\n      short-id: '%s'\n    client-fingerprint: chrome", name, host, PublicPortOrDefault(cfg, "vless-reality", 443), uuid, SafeDomain(cfg.SNI, config.DefaultSNI), realityPublic, realityShortID))
		case "trojan":
			names = append(names, name+"-Trojan")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-Trojan'\n    type: trojan\n    server: '%s'\n    port: %d\n    password: '%s'\n    sni: '%s'\n    skip-cert-verify: true", name, host, PublicPortOrDefault(cfg, "trojan", 8445), password, SafeDomain(cfg.SNI, config.DefaultSNI)))
		case "ss":
			names = append(names, name+"-SS")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-SS'\n    type: ss\n    server: '%s'\n    port: %d\n    cipher: aes-256-gcm\n    password: '%s'", name, host, PublicPortOrDefault(cfg, "ss", 8388), password))
		case "vmess":
			names = append(names, name+"-VMess")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-VMess'\n    type: vmess\n    server: '%s'\n    port: %d\n    uuid: %s\n    alterId: 0\n    cipher: auto\n    tls: true\n    servername: '%s'\n    skip-cert-verify: true", name, host, PublicPortOrDefault(cfg, "vmess", 2083), uuid, SafeDomain(cfg.SNI, config.DefaultSNI)))
		case "tuic":
			names = append(names, name+"-TUIC")
			proxies = append(proxies, fmt.Sprintf("  - name: '%s-TUIC'\n    type: tuic\n    server: '%s'\n    port: %d\n    uuid: %s\n    password: '%s'\n    sni: '%s'\n    skip-cert-verify: true\n    congestion-controller: bbr\n    udp-relay-mode: native", name, host, PublicPortOrDefault(cfg, "tuic", 8444), uuid, password, SafeDomain(cfg.SNI, config.DefaultSNI)))
		}
	}
	var groupItems []string
	for _, item := range names {
		groupItems = append(groupItems, "      - '"+item+"'")
	}
	groupItems = append(groupItems, "      - DIRECT")
	return "mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: warning\nipv6: true\n\nproxies:\n" + strings.Join(proxies, "\n") + "\n\nproxy-groups:\n  - name: PROXY\n    type: select\n    proxies:\n" + strings.Join(groupItems, "\n") + "\n\nrules:\n  - MATCH,PROXY\n"
}

// BuildSingboxClient 构建 sing-box 客户端配置（默认监听端口 2080）
func BuildSingboxClient(host string, cfg config.DeployConfig, name, password, uuid, realityPublic, realityShortID string) string {
	return BuildSingboxClientWithListen(host, cfg, name, password, uuid, realityPublic, realityShortID, 2080)
}

// BuildSingboxClientWithListen 构建 sing-box 客户端配置（指定监听端口）
func BuildSingboxClientWithListen(host string, cfg config.DeployConfig, name, password, uuid, realityPublic, realityShortID string, listenPort int) string {
	host = NormalizeHostLiteral(host)
	outbounds := []map[string]any{}
	for _, id := range cfg.Selected {
		switch id {
		case "hy2":
			outbounds = append(outbounds, map[string]any{"type": "hysteria2", "tag": name + "-HY2", "server": host, "server_port": PublicPortOrDefault(cfg, "hy2", 8443), "password": password, "tls": map[string]any{"enabled": true, "server_name": SafeDomain(cfg.SNI, config.DefaultSNI), "insecure": true}})
		case "vless-reality":
			outbounds = append(outbounds, map[string]any{"type": "vless", "tag": name + "-Reality", "server": host, "server_port": PublicPortOrDefault(cfg, "vless-reality", 443), "uuid": uuid, "flow": "xtls-rprx-vision", "tls": map[string]any{"enabled": true, "server_name": SafeDomain(cfg.SNI, config.DefaultSNI), "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}, "reality": map[string]any{"enabled": true, "public_key": realityPublic, "short_id": realityShortID}}})
		case "trojan":
			outbounds = append(outbounds, map[string]any{"type": "trojan", "tag": name + "-Trojan", "server": host, "server_port": PublicPortOrDefault(cfg, "trojan", 8445), "password": password, "tls": map[string]any{"enabled": true, "server_name": SafeDomain(cfg.SNI, config.DefaultSNI), "insecure": true}})
		case "ss":
			outbounds = append(outbounds, map[string]any{"type": "shadowsocks", "tag": name + "-SS", "server": host, "server_port": PublicPortOrDefault(cfg, "ss", 8388), "method": "aes-256-gcm", "password": password})
		case "vmess":
			outbounds = append(outbounds, map[string]any{"type": "vmess", "tag": name + "-VMess", "server": host, "server_port": PublicPortOrDefault(cfg, "vmess", 2083), "uuid": uuid, "security": "auto", "tls": map[string]any{"enabled": true, "server_name": SafeDomain(cfg.SNI, config.DefaultSNI), "insecure": true}})
		case "tuic":
			outbounds = append(outbounds, map[string]any{"type": "tuic", "tag": name + "-TUIC", "server": host, "server_port": PublicPortOrDefault(cfg, "tuic", 8444), "uuid": uuid, "password": password, "congestion_control": "bbr", "udp_relay_mode": "native", "tls": map[string]any{"enabled": true, "server_name": SafeDomain(cfg.SNI, config.DefaultSNI), "insecure": true}})
		}
	}
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})
	data, _ := json.MarshalIndent(map[string]any{"log": map[string]string{"level": "warn"}, "inbounds": []map[string]any{{"type": "mixed", "listen": "127.0.0.1", "listen_port": listenPort}}, "outbounds": outbounds, "route": map[string]any{"final": outbounds[0]["tag"], "auto_detect_interface": true}}, "", "  ")
	return string(data)
}

// RewriteSingboxListenPort 重写 sing-box 配置中 mixed inbound 的监听地址和端口
func RewriteSingboxListenPort(data []byte, listenPort int) (string, error) {
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

// BuildNginxConfig 生成 nginx 订阅分发站点配置
func BuildNginxConfig(webPort int, token, rule string) string {
	if rule == "" {
		rule = config.DefaultSubRule
	}
	path := func(client string) string {
		out := strings.ReplaceAll(rule, "{token}", token)
		out = strings.ReplaceAll(out, "{client}", client)
		return SafeLocationPath(out, token, client)
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

// SafeLocationPath 验证并修正 nginx location 路径，防止路径遍历攻击
func SafeLocationPath(path, token, client string) string {
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

// BuildSubscriptionURL 构建订阅链接地址
func BuildSubscriptionURL(host string, cfg config.DeployConfig, client string) string {
	token := SafeToken(cfg.Token)
	rule := cfg.Rule
	if rule == "" {
		rule = config.DefaultSubRule
	}
	path := strings.ReplaceAll(rule, "{token}", token)
	path = strings.ReplaceAll(path, "{client}", client)
	path = SafeLocationPath(path, token, client)
	return fmt.Sprintf("http://%s:%d%s", FormatHostForURL(host), PublicWebPortOrDefault(cfg), path)
}

// NodeSpeedClientConfig 为节点测速获取 sing-box 客户端配置：优先从订阅 URL 下载，失败则本地推导
func NodeSpeedClientConfig(host string, cfg config.DeployConfig, name, password, uuid, realityPublic, realityShortID string, listenPort int) (string, string, string) {
	subURL := BuildSubscriptionURL(host, cfg, "sing-box.json")
	client := http.Client{Timeout: 12 * time.Second}
	resp, err := client.Get(subURL)
	if err == nil && resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
			if readErr == nil && len(strings.TrimSpace(string(body))) > 0 {
				if cfgStr, rewriteErr := RewriteSingboxListenPort(body, listenPort); rewriteErr == nil {
					return cfgStr, "subscription", ""
				} else {
					return BuildSingboxClientWithListen(host, cfg, name, password, uuid, realityPublic, realityShortID, listenPort), "generated", "订阅配置解析失败，已回退到本地推导配置: " + rewriteErr.Error()
				}
			}
			return BuildSingboxClientWithListen(host, cfg, name, password, uuid, realityPublic, realityShortID, listenPort), "generated", "订阅端口返回空内容，已回退到本地推导配置"
		}
		return BuildSingboxClientWithListen(host, cfg, name, password, uuid, realityPublic, realityShortID, listenPort), "generated", fmt.Sprintf("订阅请求 HTTP %d，已回退到本地推导配置", resp.StatusCode)
	}
	warning := "订阅请求失败，已回退到本地推导配置"
	if err != nil {
		warning += ": " + err.Error()
	}
	return BuildSingboxClientWithListen(host, cfg, name, password, uuid, realityPublic, realityShortID, listenPort), "generated", warning
}
