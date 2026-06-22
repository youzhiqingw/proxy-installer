package deploy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"proxy-installer/internal/config"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testProfile returns a config.SSHProfile with sensible test values.
func testProfile() config.SSHProfile {
	return config.SSHProfile{
		ID:   "test-profile-1",
		Name: "test-vps",
		Host: "203.0.113.10",
		User: "root",
		Port: 22,
	}
}

// testConfig returns a config.DeployConfig with default SS-only selection.
func testConfig() config.DeployConfig {
	return config.DeployConfig{
		ProfileID: "test-profile-1",
		NodeName:  "test-node",
		Selected:  []string{"ss"},
		Ports:     map[string]int{"ss": 8388},
		WebPort:   8080,
		Token:     "testtoken123",
		Rule:      "/sub/{token}/{client}",
		SNI:       "www.example.com",
	}
}

// extractB64FromScript extracts the base64 payload from a rendered deploy
// script line that matches `write_b64 "<payload>" <target>`.
func extractB64FromScript(script, target string) (string, error) {
	marker := fmt.Sprintf(`write_b64 "`)
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, marker) && strings.Contains(line, target) {
			start := strings.Index(line, marker) + len(marker)
			end := strings.Index(line[start:], `"`)
			if end < 0 {
				return "", fmt.Errorf("malformed write_b64 line: %s", line)
			}
			return line[start : start+end], nil
		}
	}
	return "", fmt.Errorf("write_b64 line for target %q not found", target)
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_DefaultConfig
// ---------------------------------------------------------------------------

func TestBuildDeployScript_DefaultConfig(t *testing.T) {
	profile := testProfile()
	cfg := testConfig()

	script, err := BuildDeployScript(profile, cfg)
	if err != nil {
		t.Fatalf("BuildDeployScript returned error: %v", err)
	}

	checks := []struct {
		label    string
		contains string
	}{
		{"safety header", "set -euo pipefail"},
		{"file writing stage", "emit progress 42"},
		{"validation stage", "sing-box check"},
		{"service start", "systemctl restart sing-box"},
		{"token value", "testtoken123"},
		{"SNI value", "www.example.com"},
	}
	for _, c := range checks {
		t.Run(c.label, func(t *testing.T) {
			if !strings.Contains(script, c.contains) {
				t.Errorf("script does not contain %q", c.contains)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_MultiProtocol
// ---------------------------------------------------------------------------

func TestBuildDeployScript_MultiProtocol(t *testing.T) {
	profile := testProfile()
	cfg := config.DeployConfig{
		ProfileID: "test-profile-1",
		NodeName:  "multi-node",
		Selected:  []string{"vless-reality", "hy2", "ss"},
		Ports: map[string]int{
			"vless-reality": 443,
			"hy2":           8443,
			"ss":            8388,
		},
		WebPort: 8080,
		Token:   "multiproto",
		SNI:     "www.example.com",
	}

	script, err := BuildDeployScript(profile, cfg)
	if err != nil {
		t.Fatalf("BuildDeployScript returned error: %v", err)
	}

	// All three protocol ports should appear in the port list or config.
	for _, port := range []string{"443", "8443", "8388"} {
		t.Run("port_"+port, func(t *testing.T) {
			if !strings.Contains(script, port) {
				t.Errorf("script does not contain port %s", port)
			}
		})
	}

	// The server config is base64-encoded; verify it is non-empty and decodable.
	t.Run("server_config_is_base64", func(t *testing.T) {
		b64, err := extractB64FromScript(script, "/etc/sing-box/config.json")
		if err != nil {
			t.Fatalf("failed to extract server config b64: %v", err)
		}
		if b64 == "" {
			t.Fatal("server config base64 is empty")
		}
		decoded, decErr := base64.StdEncoding.DecodeString(b64)
		if decErr != nil {
			t.Fatalf("server config base64 decode failed: %v", decErr)
		}
		if len(decoded) == 0 {
			t.Fatal("decoded server config is empty")
		}
	})
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_IPv6Host
// ---------------------------------------------------------------------------

func TestBuildDeployScript_IPv6Host(t *testing.T) {
	profile := testProfile()
	profile.Host = "2001:db8::1"
	cfg := testConfig()

	script, err := BuildDeployScript(profile, cfg)
	if err != nil {
		t.Fatalf("BuildDeployScript with IPv6 host returned error: %v", err)
	}

	// The IPv6 address should appear in client configs. The SS URI inner-encodes
	// the address in base64, so we check the mihomo YAML config where the
	// server IP appears in plaintext.
	b64, err := extractB64FromScript(script, "mihomo.yaml")
	if err != nil {
		t.Fatalf("failed to extract mihomo b64: %v", err)
	}
	decoded, decErr := base64.StdEncoding.DecodeString(b64)
	if decErr != nil {
		t.Fatalf("mihomo base64 decode failed: %v", decErr)
	}
	if !strings.Contains(string(decoded), "2001:db8::1") {
		t.Errorf("decoded mihomo config does not contain IPv6 address, got:\n%s", string(decoded))
	}
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_EmptyHost
// ---------------------------------------------------------------------------

func TestBuildDeployScript_EmptyHost(t *testing.T) {
	profile := testProfile()
	profile.Host = ""
	cfg := testConfig()

	_, err := BuildDeployScript(profile, cfg)
	if err == nil {
		t.Fatal("expected error for empty host, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_UnsupportedProtocol
// ---------------------------------------------------------------------------

func TestBuildDeployScript_UnsupportedProtocol(t *testing.T) {
	profile := testProfile()

	tests := []struct {
		name     string
		selected []string
	}{
		{"all unsupported", []string{"wireguard", "openvpn"}},
		{"single unsupported", []string{"unknown-proto"}},
		{"empty selection", []string{}},
		{"nil selection", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Selected = tt.selected
			_, err := BuildDeployScript(profile, cfg)
			if err == nil {
				t.Fatal("expected error for unsupported protocols, got nil")
			}
			if !strings.Contains(err.Error(), "请选择至少一个支持协议") {
				t.Errorf("unexpected error message: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_InvalidPort
// ---------------------------------------------------------------------------

func TestBuildDeployScript_InvalidPort(t *testing.T) {
	profile := testProfile()

	tests := []struct {
		name      string
		ports     map[string]int
		wantError bool
	}{
		{"port zero falls back to default", map[string]int{"ss": 0}, false},
		{"port negative falls back to default", map[string]int{"ss": -1}, false},
		{"port 70000 out of range", map[string]int{"ss": 70000}, true},
		{"port 65536 out of range", map[string]int{"ss": 65536}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Ports = tt.ports
			_, err := BuildDeployScript(profile, cfg)
			if tt.wantError && err == nil {
				t.Fatal("expected error for invalid port, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_ShellInjectionSafety
// ---------------------------------------------------------------------------

func TestBuildDeployScript_ShellInjectionSafety(t *testing.T) {
	profile := testProfile()

	tests := []struct {
		name          string
		token         string
		wantSanitized string
	}{
		{
			"command substitution",
			"$(rm -rf /)",
			"rm-rf",
		},
		{
			"semicolon injection",
			"; rm -rf /",
			"rm-rf",
		},
		{
			"backtick injection",
			"`rm -rf /`",
			"rm-rf",
		},
		{
			"pipe injection",
			"token|cat /etc/passwd",
			"tokencatetcpasswd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify SafeToken strips dangerous characters.
			sanitized := SafeToken(tt.token)
			if sanitized != tt.wantSanitized {
				t.Errorf("SafeToken(%q) = %q, want %q", tt.token, sanitized, tt.wantSanitized)
			}

			// Verify no shell metacharacters survive sanitization.
			for _, ch := range []string{"$", "(", ")", ";", "`", "|", "&", ">", "<", "!", "\\", "\"", "'", " "} {
				if strings.Contains(sanitized, ch) {
					t.Errorf("sanitized token %q still contains shell metacharacter %q", sanitized, ch)
				}
			}

			cfg := testConfig()
			cfg.Token = tt.token

			script, err := BuildDeployScript(profile, cfg)
			if err != nil {
				t.Fatalf("BuildDeployScript returned error: %v", err)
			}

			// Verify the FULL original malicious token string does NOT appear
			// in the generated script. The sanitized version is used instead.
			if strings.Contains(script, tt.token) {
				t.Errorf("generated script contains the raw malicious token %q", tt.token)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_TokenSanitization
// ---------------------------------------------------------------------------

func TestBuildDeployScript_TokenSanitization(t *testing.T) {
	profile := testProfile()
	cfg := testConfig()
	cfg.Token = "abc@#$%"

	script, err := BuildDeployScript(profile, cfg)
	if err != nil {
		t.Fatalf("BuildDeployScript returned error: %v", err)
	}

	// SafeToken("abc@#$%") should produce "abc".
	sanitized := SafeToken("abc@#$%")
	if sanitized != "abc" {
		t.Errorf("SafeToken(\"abc@#$%%\") = %q, want %q", sanitized, "abc")
	}

	// The sanitized token "abc" should appear in the script.
	if !strings.Contains(script, "abc") {
		t.Error("script does not contain sanitized token \"abc\"")
	}

	// The raw special characters should not appear as part of the token.
	// (They may appear in other contexts like regex, so we check the token
	// path specifically.)
	if strings.Contains(script, "abc@#$%") {
		t.Error("script contains unsanitized token \"abc@#$%\"")
	}
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_ConfigStructure
// ---------------------------------------------------------------------------

func TestBuildDeployScript_ConfigStructure(t *testing.T) {
	profile := testProfile()
	cfg := config.DeployConfig{
		ProfileID: "test-profile-1",
		NodeName:  "struct-node",
		Selected:  []string{"vless-reality", "ss"},
		Ports: map[string]int{
			"vless-reality": 443,
			"ss":            8388,
		},
		WebPort: 8080,
		Token:   "structtoken",
		SNI:     "www.example.com",
	}

	script, err := BuildDeployScript(profile, cfg)
	if err != nil {
		t.Fatalf("BuildDeployScript returned error: %v", err)
	}

	// Extract and decode the server config base64.
	b64, err := extractB64FromScript(script, "/etc/sing-box/config.json")
	if err != nil {
		t.Fatalf("failed to extract server config b64: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	var serverConfig map[string]any
	if err := json.Unmarshal(decoded, &serverConfig); err != nil {
		t.Fatalf("JSON unmarshal of server config failed: %v", err)
	}

	// Verify top-level structure keys.
	for _, key := range []string{"inbounds", "outbounds", "route"} {
		t.Run("has_"+key, func(t *testing.T) {
			if _, ok := serverConfig[key]; !ok {
				t.Errorf("server config missing key %q", key)
			}
		})
	}

	// Verify inbounds is a non-empty array.
	t.Run("inbounds_non_empty", func(t *testing.T) {
		inbounds, ok := serverConfig["inbounds"].([]any)
		if !ok {
			t.Fatal("inbounds is not an array")
		}
		if len(inbounds) == 0 {
			t.Fatal("inbounds is empty")
		}
		// We selected vless-reality + ss, so expect 2 inbounds.
		if len(inbounds) != 2 {
			t.Errorf("expected 2 inbounds, got %d", len(inbounds))
		}
	})

	// Verify outbounds has direct and block.
	t.Run("outbounds_structure", func(t *testing.T) {
		outbounds, ok := serverConfig["outbounds"].([]any)
		if !ok {
			t.Fatal("outbounds is not an array")
		}
		if len(outbounds) < 2 {
			t.Fatalf("expected at least 2 outbounds, got %d", len(outbounds))
		}
		first, _ := outbounds[0].(map[string]any)
		if first["tag"] != "direct" {
			t.Errorf("first outbound tag = %v, want \"direct\"", first["tag"])
		}
	})

	// Verify route has final key.
	t.Run("route_has_final", func(t *testing.T) {
		route, ok := serverConfig["route"].(map[string]any)
		if !ok {
			t.Fatal("route is not an object")
		}
		if route["final"] != "direct" {
			t.Errorf("route.final = %v, want \"direct\"", route["final"])
		}
	})
}

// ---------------------------------------------------------------------------
// TestBuildDeployScript_NginxConfig
// ---------------------------------------------------------------------------

func TestBuildDeployScript_NginxConfig(t *testing.T) {
	profile := testProfile()
	cfg := testConfig()
	cfg.WebPort = 9090
	cfg.Token = "nginxtoken"

	script, err := BuildDeployScript(profile, cfg)
	if err != nil {
		t.Fatalf("BuildDeployScript returned error: %v", err)
	}

	// Extract and decode the nginx config base64.
	b64, err := extractB64FromScript(script, "/etc/nginx/conf.d/proxy-installer.conf")
	if err != nil {
		t.Fatalf("failed to extract nginx config b64: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}
	nginxConf := string(decoded)

	// Verify the web port appears in the nginx config.
	t.Run("web_port_in_nginx", func(t *testing.T) {
		if !strings.Contains(nginxConf, "listen 9090") {
			t.Errorf("nginx config does not contain 'listen 9090', got:\n%s", nginxConf)
		}
	})

	// Verify the token path appears in the nginx config.
	t.Run("token_path_in_nginx", func(t *testing.T) {
		if !strings.Contains(nginxConf, "nginxtoken") {
			t.Errorf("nginx config does not contain token 'nginxtoken', got:\n%s", nginxConf)
		}
	})

	// Verify the script contains nginx-related orchestration commands.
	t.Run("nginx_service_commands", func(t *testing.T) {
		if !strings.Contains(script, "systemctl restart nginx") {
			t.Error("script does not contain 'systemctl restart nginx'")
		}
		if !strings.Contains(script, "nginx -t") {
			t.Error("script does not contain 'nginx -t' validation")
		}
	})
}
