// Package quality 负责 IP 质量检测：脚本生成、结果解析和报告构建。
// 从 app.go 提取，不依赖 Wails runtime。
package quality

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"proxy-installer/internal/util"
)

// QualityField 定义质量报告中的字段映射（Label 为显示名，Key 为数据键）
type QualityField struct {
	Label string
	Key   string
}

// enablePing0Check 控制是否启用 ping0.cc IP 检测源（隐私原因默认关闭，恢复时改为 true）
const enablePing0Check = false

// IPQualityScript 返回用于远程执行的 IP 质量检测 bash 脚本
func IPQualityScript() string {
	script := `
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
`
	if enablePing0Check {
		script += `fetch_text "20" "ping0" "https://ipv4.ping0.cc/geo" "https://ipv6.ping0.cc/geo" "https://ping0.cc/geo" &
`
	}
	script += `fetch_text "30" "iplark" "https://iplark.com/cdn-cgi/trace" "https://iplark.com" &
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
	return script
}

// ParseIPQualitySources 解析远程脚本输出，返回结构化数据和来源错误
func ParseIPQualitySources(text string) (map[string]any, map[string]string) {
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
			if util.LooksLikeHTML(string(data)) {
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
	text = strings.TrimSpace(util.StripANSI(text))
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

// BuildQualityReport 从原始检测数据构建质量报告：摘要、站点卡片和报告段落
func BuildQualityReport(raw map[string]any, errors map[string]string) (map[string]any, []map[string]any, []map[string]any) {
	sites := []map[string]any{
		buildQualitySite("ippure", "IPPure", "https://ippure.com/", raw["ippure"], errors["ippure"], []QualityField{
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
	}
	if enablePing0Check {
		sites = append(sites, buildQualitySite("ping0", "ping0", "https://ping0.cc/", raw["ping0"], errors["ping0"], []QualityField{
			{"IP", "ip"},
			{"位置", "location"},
			{"ASN", "asn"},
			{"组织", "org"},
		}))
	}
	sites = append(sites, buildQualitySite("iplark", "IPLark", "https://iplark.com/", raw["iplark"], errors["iplark"], []QualityField{
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
	)
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
	sections := BuildQualitySections(raw, errors)
	checkTotal := 0
	checkOK := 0
	flat := util.FlattenAny(raw)
	purityPercent := -1
	puritySource := ""
	if scoreText := util.FirstValue(flat, "ippure.fraudScore"); scoreText != "" {
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

func buildQualitySite(id, name, siteURL string, data any, errText string, fields []QualityField) map[string]any {
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
	util.FlattenJSON("", data, flat)
	var rows []map[string]string
	for _, field := range fields {
		if value := strings.TrimSpace(flat[field.Key]); value != "" {
			rows = append(rows, map[string]string{"label": field.Label, "value": value})
		}
	}
	if len(rows) == 0 {
		for _, key := range util.SortedKeys(flat) {
			if value := strings.TrimSpace(flat[key]); value != "" {
				rows = append(rows, map[string]string{"label": key, "value": value})
			}
		}
	}
	site["status"] = "success"
	site["rows"] = rows
	ip := util.FirstValue(flat, "ip", "query")
	site["ip"] = ip
	switch id {
	case "ippure":
		score := util.FirstValue(flat, "fraudScore")
		if score != "" {
			site["metric"] = "Fraud " + score
		}
		site["summary"] = util.CompactJoin(util.FirstValue(flat, "country"), util.FirstValue(flat, "city"), util.FirstValue(flat, "asOrganization"))
	case "ping0":
		site["metric"] = util.FirstValue(flat, "asn")
		site["summary"] = util.CompactJoin(util.FirstValue(flat, "location"), util.FirstValue(flat, "org"))
	case "iplark":
		site["metric"] = util.CompactJoin(util.FirstValue(flat, "colo"), util.FirstValue(flat, "loc"))
		site["summary"] = util.CompactJoin(util.FirstValue(flat, "http"), util.FirstValue(flat, "tls"), "WARP "+util.FirstValue(flat, "warp"))
	}
	if site["metric"] == "" {
		site["metric"] = ip
	}
	if site["summary"] == "" {
		site["summary"] = "已返回结果"
	}
	return site
}

// BuildQualitySections 构建 6 个报告段落：基础信息、IP 类型、风险评分、风险因子、流媒体/AI、邮局/黑名单
func BuildQualitySections(raw map[string]any, errors map[string]string) []map[string]any {
	flat := util.FlattenAny(raw)
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
		return util.CompactJoin(status, detail), status
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
		row("公网 IPv4", util.FirstValue(flat, "checks.base.public_ip.detail", "ippure.ip", "ping0.ip", "iplark.ip", "ip-api.query", "ipinfo.ip", "ipapi.ip", "ipwhois.ip", "dbip.ipAddress"), "多源", "ok"),
		row("国家/城市", util.CompactJoin(util.FirstValue(flat, "ippure.country", "ip-api.country", "ipinfo.country", "ipapi.country_name", "ipwhois.country"), util.FirstValue(flat, "ippure.city", "ip-api.city", "ipapi.city", "ipwhois.city")), "IPPure / ip-api", sourceStatus("ippure")),
		row("ASN/组织", util.CompactJoin(util.FirstValue(flat, "ippure.asn", "ip-api.as", "ping0.asn"), util.FirstValue(flat, "ippure.asOrganization", "ipinfo.org", "ping0.org", "ip-api.org")), "IPPure / ping0", sourceStatus("ping0")),
		row("Cloudflare 边缘", util.CompactJoin(util.FirstValue(flat, "iplark.colo", "cloudflare.colo"), util.FirstValue(flat, "iplark.loc", "cloudflare.loc")), "IPLark / Cloudflare", sourceStatus("iplark")),
		row("时区", util.FirstValue(flat, "ip-api.timezone", "ipwhois.timezone.id", "ipinfo.timezone"), "ip-api / ipwhois", sourceStatus("ip-api")),
		row("坐标", util.CompactJoin(util.FirstValue(flat, "ip-api.lat", "ipwhois.latitude"), util.FirstValue(flat, "ip-api.lon", "ipwhois.longitude"), util.FirstValue(flat, "ipinfo.loc")), "ip-api / ipinfo", sourceStatus("ip-api")),
		row("地区代码", util.CompactJoin(util.FirstValue(flat, "ip-api.countryCode", "ipapi.country_code", "ipwhois.country_code"), util.FirstValue(flat, "ip-api.regionName", "ipapi.region", "dbip.stateProv")), "ip-api / ipapi", sourceStatus("ip-api")),
		row("邮编", util.FirstValue(flat, "ip-api.zip", "ipapi.postal", "ipwhois.postal"), "ip-api / ipapi", sourceStatus("ip-api")),
		row("运营商", util.FirstValue(flat, "ip-api.isp", "ipapi.org", "ipwhois.connection.isp"), "ip-api / ipapi", sourceStatus("ip-api")),
		row("反查/主机名", util.FirstValue(flat, "ip-api.reverse", "ipinfo.hostname"), "ip-api / ipinfo", sourceStatus("ip-api")),
	}

	ipType := []map[string]string{
		row("住宅 IP", util.FirstValue(flat, "ippure.isResidential"), "IPPure", sourceStatus("ippure")),
		row("广播 IP", util.FirstValue(flat, "ippure.isBroadcast"), "IPPure", sourceStatus("ippure")),
		row("Proxy", util.FirstValue(flat, "ip-api.proxy"), "ip-api", sourceStatus("ip-api")),
		row("Hosting", util.FirstValue(flat, "ip-api.hosting"), "ip-api", sourceStatus("ip-api")),
		row("Mobile", util.FirstValue(flat, "ip-api.mobile"), "ip-api", sourceStatus("ip-api")),
		row("ISP", util.FirstValue(flat, "ip-api.isp", "ipapi.org", "ipwhois.connection.isp"), "ip-api / ipapi", sourceStatus("ip-api")),
	}

	risk := []map[string]string{
		row("IPPure FraudScore", util.FirstValue(flat, "ippure.fraudScore"), "IPPure", sourceStatus("ippure")),
		row("Scamalytics", sourceValue("scamalytics"), "Scamalytics", sourceStatus("scamalytics")),
		row("ip-api Proxy/Hosting", util.CompactJoin("proxy="+util.FirstValue(flat, "ip-api.proxy"), "hosting="+util.FirstValue(flat, "ip-api.hosting")), "ip-api", sourceStatus("ip-api")),
		row("db-ip", util.CompactJoin(util.FirstValue(flat, "dbip.ipAddress"), util.FirstValue(flat, "dbip.countryName"), util.FirstValue(flat, "dbip.stateProv")), "DB-IP Free", sourceStatus("dbip")),
	}

	factors := []map[string]string{
		row("DNSBL 黑名单", fmt.Sprintf("%d listed / %d checked", dnsListed, dnsTotal), "DNSBL", dnsStatus),
		row("SMTP 25", func() string { v, _ := check("mail.gmail"); return v }(), "Gmail MX", func() string { _, s := check("mail.gmail"); return s }()),
		row("WARP", util.FirstValue(flat, "iplark.warp", "cloudflare.warp"), "IPLark / Cloudflare", sourceStatus("iplark")),
		row("Gateway", util.FirstValue(flat, "iplark.gateway"), "IPLark", sourceStatus("iplark")),
		row("HTTP/TLS 指纹", util.CompactJoin(util.FirstValue(flat, "iplark.http", "cloudflare.http"), util.FirstValue(flat, "iplark.tls")), "IPLark", sourceStatus("iplark")),
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
