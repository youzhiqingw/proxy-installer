// Package deploy 负责部署脚本生成、配置文件构建和远程部署编排。
// 从 app.go 提取，不依赖 Wails runtime（通过 sshclient.EmitFn 回调解耦）。
package deploy

import (
	"bytes"
	"fmt"
	"text/template"

	"golang.org/x/crypto/ssh"

	"proxy-installer/internal/config"
	apperr "proxy-installer/internal/errors"
	"proxy-installer/internal/logger"
	"proxy-installer/internal/singbox"
	"proxy-installer/internal/sshclient"
)

// Deploy 执行远程部署：构建脚本 → 远程执行 → 失败时尝试本机上传 sing-box 兜底
func Deploy(client *ssh.Client, emit sshclient.EmitFn, profile config.SSHProfile, cfg config.DeployConfig) (map[string]any, error) {
	logger.Info("开始部署", "host", profile.Host, "protocols", cfg.Selected, "sni", cfg.SNI)
	if cfg.SNI == "" {
		cfg.SNI = config.DefaultSNI
	}
	if cfg.WebPort == 0 {
		cfg.WebPort = config.DefaultWebPort
	}
	if cfg.Token == "" {
		cfg.Token = config.DefaultToken
	}
	if cfg.Rule == "" {
		cfg.Rule = config.DefaultSubRule
	}
	if len(cfg.Selected) == 0 {
		cfg.Selected = []string{"ss"}
	}

	script, err := BuildDeployScript(profile, cfg)
	if err != nil {
		emit("error", 0, err.Error())
		return nil, apperr.NewDeploy("构建部署脚本失败", err)
	}

	emit("progress", 2, "连接成功，开始远程部署")
	code, err := sshclient.RunStreaming(client, "bash -lc "+ShellQuote(script), emit)
	if err != nil {
		emit("error", 0, err.Error())
		return nil, apperr.NewSSH("远程执行部署脚本失败", err)
	}
	if code == 11 {
		emit("log", 34, "远端下载 sing-box 失败，尝试由本机上传 Linux 二进制")
		if uploadErr := singbox.InstallSingBoxViaUpload(client, emit); uploadErr != nil {
			msg := "本机上传 sing-box 兜底失败: " + uploadErr.Error()
			emit("error", 34, msg)
			return map[string]any{"ok": false, "code": code, "uploadError": uploadErr.Error()}, nil
		}
		emit("progress", 36, "sing-box 已上传，重新执行远程部署")
		code, err = sshclient.RunStreaming(client, "bash -lc "+ShellQuote(script), emit)
		if err != nil {
			emit("error", 0, err.Error())
			return nil, apperr.NewSSH("重试部署脚本失败", err)
		}
	}
	if code != 0 {
		msg := fmt.Sprintf("部署失败，退出码 %d", code)
		emit("error", 0, msg)
		return map[string]any{"ok": false, "code": code}, nil
	}
	emit("done", 100, "部署完成")
	return map[string]any{"ok": true, "code": 0}, nil
}

// deployTemplateData 传递给部署脚本模板的结构化数据
type deployTemplateData struct {
	Token             string
	SNI               string
	PortList          string
	ServerConfigB64   string
	NginxConfigB64    string
	ShadowrocketB64   string
	V2rayngB64        string
	MihomoB64         string
	SingboxB64        string
	WebPort           int
}

// deployScriptTpl 是部署 bash 脚本的 Go text/template（取代 fmt.Sprintf 拼接）
var deployScriptTpl = template.Must(template.New("deploy").Parse(`
set -euo pipefail
emit(){ msg="$(printf '%s' "$3" | base64 | tr -d '\n')"; printf '__VPS_STARTER_EVENT__|%s|%s|%s\n' "$1" "$2" "$msg"; }
write_b64(){ printf '%s' "$1" | base64 -d > "$2"; }
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
          sed -i -E 's#bullseye/updates#bullseye-security#g; /^[[:space:]]*deb(-src)?[[:space:]]bullseye-backports([[:space:]]|$)/ s#^#\# disabled by proxy-installer: #' "$f"
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
mkdir -p /etc/proxy-installer /etc/sing-box /etc/nginx/conf.d /var/www/proxy-installer/{{.Token}}
chmod 755 /etc/proxy-installer /var/www/proxy-installer /var/www/proxy-installer/{{.Token}}
if [ ! -f /etc/proxy-installer/server.crt ] || [ ! -f /etc/proxy-installer/server.key ]; then
  openssl req -x509 -nodes -newkey rsa:2048 -keyout /etc/proxy-installer/server.key -out /etc/proxy-installer/server.crt -subj "/CN={{.SNI}}" -days 3650 >/dev/null 2>&1
  chmod 600 /etc/proxy-installer/server.key
  chmod 644 /etc/proxy-installer/server.crt
fi
emit progress 52 "Checking ports"
systemctl stop sing-box 2>/dev/null || true
busy=0
for port in {{.PortList}}; do
  if (ss -lntup 2>/dev/null || true) | grep -Eq "[:.]$port([[:space:]]|$)"; then emit error 52 "Port $port is busy"; busy=1; fi
done
if [ "$busy" -eq 1 ]; then exit 12; fi
emit progress 64 "Writing config files"
write_b64 "{{.ServerConfigB64}}" /etc/sing-box/config.json
write_b64 "{{.NginxConfigB64}}" /etc/nginx/conf.d/proxy-installer.conf
write_b64 "{{.ShadowrocketB64}}" /var/www/proxy-installer/{{.Token}}/shadowrocket
write_b64 "{{.V2rayngB64}}" /var/www/proxy-installer/{{.Token}}/v2rayng
write_b64 "{{.MihomoB64}}" /var/www/proxy-installer/{{.Token}}/mihomo.yaml
write_b64 "{{.SingboxB64}}" /var/www/proxy-installer/{{.Token}}/sing-box.json
chmod 600 /etc/sing-box/config.json
chmod -R 644 /var/www/proxy-installer/{{.Token}}/* 2>/dev/null || true
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
if command -v ufw >/dev/null 2>&1; then for port in {{.PortList}} {{.WebPort}}; do ufw allow "$port/tcp" >/dev/null 2>&1 || true; ufw allow "$port/udp" >/dev/null 2>&1 || true; done; fi
if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then for port in {{.PortList}} {{.WebPort}}; do firewall-cmd --add-port="$port/tcp" >/dev/null 2>&1 || true; firewall-cmd --add-port="$port/udp" >/dev/null 2>&1 || true; firewall-cmd --permanent --add-port="$port/tcp" >/dev/null 2>&1 || true; firewall-cmd --permanent --add-port="$port/udp" >/dev/null 2>&1 || true; done; firewall-cmd --reload >/dev/null 2>&1 || true; fi
emit progress 88 "Starting nginx"
nginx -t >/tmp/proxy-installer-nginx-check.log 2>&1 || { while IFS= read -r line; do emit log 88 "$line"; done </tmp/proxy-installer-nginx-check.log; emit error 88 "nginx config invalid"; exit 14; }
systemctl enable nginx >/dev/null 2>&1 || true
systemctl restart nginx >/tmp/proxy-installer-nginx-service.log 2>&1 || { while IFS= read -r line; do emit log 88 "$line"; done </tmp/proxy-installer-nginx-service.log; emit error 88 "nginx failed"; exit 15; }
emit progress 94 "Starting sing-box"
systemctl enable sing-box >/dev/null 2>&1 || true
systemctl restart sing-box >/tmp/proxy-installer-singbox-service.log 2>&1 || { while IFS= read -r line; do emit log 94 "$line"; done </tmp/proxy-installer-singbox-service.log; journalctl -u sing-box --no-pager -n 40 2>/dev/null | while IFS= read -r line; do emit log 94 "$line"; done; emit error 94 "sing-box failed"; exit 16; }
systemctl is-active --quiet sing-box || { emit error 96 "sing-box is not active"; exit 17; }
emit result 100 "Services are running"
`))

// BuildDeployScript 根据 profile 和 config 生成完整的远程部署 bash 脚本（使用 text/template）
func BuildDeployScript(profile config.SSHProfile, cfg config.DeployConfig) (string, error) {
	host := NormalizeHostLiteral(profile.Host)
	if host == "" {
		return "", apperr.NewValidation("host is empty")
	}
	cfg.Selected = FilterSupportedProtocols(cfg.Selected)
	if len(cfg.Selected) == 0 {
		return "", apperr.NewValidation("请选择至少一个支持协议")
	}
	for _, id := range cfg.Selected {
		port := PortOrDefault(cfg.Ports, id, ProtocolDefaults()[id])
		if port < 1 || port > 65535 {
			return "", apperr.NewValidation(fmt.Sprintf("%s 内部端口必须在 1-65535 之间", id))
		}
		publicPort := PublicPortOrDefault(cfg, id, port)
		if publicPort < 1 || publicPort > 65535 {
			return "", apperr.NewValidation(fmt.Sprintf("%s 公网端口必须在 1-65535 之间", id))
		}
	}
	nodeName := SafeName(cfg.NodeName, config.DefaultNodeName)
	token := SafeToken(cfg.Token)
	sni := SafeDomain(cfg.SNI, config.DefaultSNI)
	password := config.PasswordPrefix + token + config.PasswordSuffix
	uuid := stableUUID(token)
	realityPrivate, realityPublic, realityShortID := RealityKeys(token)
	webPort := cfg.WebPort
	if webPort == 0 {
		webPort = config.DefaultWebPort
	}
	if cfg.PublicWebPort == 0 {
		cfg.PublicWebPort = webPort
	}

	serverConfig := BuildServerConfig(cfg.Selected, cfg.Ports, nodeName, password, uuid, realityPrivate, realityShortID, sni)
	files := BuildClientFiles(host, cfg, nodeName, password, uuid, realityPublic, realityShortID)
	nginxConfig := BuildNginxConfig(webPort, token, cfg.Rule)
	selPorts := SelectedPorts(cfg.Selected, cfg.Ports)
	portList := intsToShell(selPorts)

	data := deployTemplateData{
		Token:           token,
		SNI:             sni,
		PortList:        portList,
		ServerConfigB64: B64JSON(serverConfig),
		NginxConfigB64:  B64(nginxConfig),
		ShadowrocketB64: B64(files["raw"]),
		V2rayngB64:      B64(files["raw"]),
		MihomoB64:       B64(files["mihomo"]),
		SingboxB64:      B64(files["singbox"]),
		WebPort:         webPort,
	}

	var buf bytes.Buffer
	if err := deployScriptTpl.Execute(&buf, data); err != nil {
		return "", apperr.NewDeploy("渲染部署脚本模板失败", err)
	}
	return buf.String(), nil
}
