package main

import (
	"strings"
	"testing"
)

func TestIPv6HostFormatting(t *testing.T) {
	host := "2001:db8::10"
	if got := normalizeHostLiteral("[" + host + "]"); got != host {
		t.Fatalf("normalizeHostLiteral() = %q, want %q", got, host)
	}
	if got := formatHostForURL(host); got != "["+host+"]" {
		t.Fatalf("formatHostForURL() = %q", got)
	}
	if got := formatHostForURI(host); got != "["+host+"]" {
		t.Fatalf("formatHostForURI() = %q", got)
	}
}

func TestClientFilesUseBracketedIPv6URIs(t *testing.T) {
	config := DeployConfig{
		Selected: []string{"vless-reality", "ss"},
		Ports: map[string]int{
			"vless-reality": 443,
			"ss":            8388,
		},
		Token: "starter2026",
		SNI:   "www.bing.com",
	}
	files := buildClientFiles("2001:db8::10", config, "node", "password", "00000000-0000-4000-8000-000000000000", "pub", "abcd")
	raw := files["raw"]
	if !strings.Contains(raw, "@[2001:db8::10]:443") {
		t.Fatalf("vless IPv6 URI is not bracketed:\n%s", raw)
	}
	if !strings.Contains(raw, "ss://") {
		t.Fatalf("missing shadowsocks URI:\n%s", raw)
	}
}

func TestDeployScriptIPv6NetworkFallbackTemplate(t *testing.T) {
	script, err := buildDeployScript(SSHProfile{Host: "2001:db8::10"}, DeployConfig{
		Selected: []string{"ss"},
		Ports:    map[string]int{"ss": 8388},
		Token:    "starter2026",
		SNI:      "www.bing.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Testing network",
		"install_sing_box",
		"has_global_ipv6",
		"sing-box install failed",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("deploy script missing %q", want)
		}
	}
	if strings.Contains(script, "%!") {
		t.Fatalf("deploy script contains fmt artifact: %s", script[strings.Index(script, "%!"):])
	}
	if nginx := buildNginxConfig(8080, "starter2026", ""); !strings.Contains(nginx, "listen [::]:8080;") {
		t.Fatalf("nginx config missing IPv6 listen:\n%s", nginx)
	}
}
