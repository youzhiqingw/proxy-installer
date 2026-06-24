package deploy

import (
	"fmt"
	"testing"

	"proxy-installer/internal/config"
)

// ---------------------------------------------------------------------------
// SafeLocationPath
// ---------------------------------------------------------------------------

func TestSafeLocationPath(t *testing.T) {
	const tok = "mytoken"
	const cli = "v2rayng"
	fallback := fmt.Sprintf("/sub/%s/%s", tok, cli)

	tests := []struct {
		name  string
		path  string
		token string
		cl    string
		want  string
	}{
		{"normal path", "/sub/mytoken/v2rayng", tok, cli, "/sub/mytoken/v2rayng"},
		{"empty falls back", "", tok, cli, fallback},
		{"whitespace only falls back", "   ", tok, cli, fallback},
		{"path traversal blocked", "/sub/../etc/passwd", tok, cli, fallback},
		{"dot-dot anywhere blocked", "/a/b..c/d", tok, cli, fallback},
		{"missing leading slash added", "sub/mytoken/v2rayng", tok, cli, "/sub/mytoken/v2rayng"},
		{"slash only falls back", "/", tok, cli, fallback},
		{"special chars stripped", "/sub/my token!/v2rayng", tok, cli, "/sub/mytoken/v2rayng"},
		{"dots allowed in path", "/sub/v1.0/client", tok, cli, "/sub/v1.0/client"},
		{"hyphens and underscores kept", "/sub/my-tok/my_cli", tok, cli, "/sub/my-tok/my_cli"},
		{"unicode stripped", "/sub/测试/v2rayng", tok, cli, "/sub//v2rayng"},
		{"deep path kept", "/a/b/c/d", tok, cli, "/a/b/c/d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeLocationPath(tt.path, tt.token, tt.cl)
			if got != tt.want {
				t.Errorf("SafeLocationPath(%q, %q, %q) = %q, want %q",
					tt.path, tt.token, tt.cl, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BuildSubscriptionURL
// ---------------------------------------------------------------------------

func TestBuildSubscriptionURL(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		cfg    config.DeployConfig
		client string
		want   string
	}{
		{
			name: "IPv4 with default port and default rule",
			host: "1.2.3.4",
			cfg: config.DeployConfig{
				Token: "mytoken",
			},
			client: "v2rayng",
			want:   "http://1.2.3.4:8080/sub/mytoken/v2rayng",
		},
		{
			name: "IPv4 with custom web port",
			host: "10.0.0.1",
			cfg: config.DeployConfig{
				Token:   "tok123",
				WebPort: 9090,
			},
			client: "shadowrocket",
			want:   "http://10.0.0.1:9090/sub/tok123/shadowrocket",
		},
		{
			name: "IPv4 with public web port",
			host: "10.0.0.1",
			cfg: config.DeployConfig{
				Token:         "tok",
				WebPort:       9090,
				PublicWebPort: 443,
			},
			client: "sing-box.json",
			want:   "http://10.0.0.1:443/sub/tok/sing-box.json",
		},
		{
			name: "IPv6 gets brackets",
			host: "2001:db8::1",
			cfg: config.DeployConfig{
				Token: "abc",
			},
			client: "mihomo.yaml",
			want:   "http://[2001:db8::1]:8080/sub/abc/mihomo.yaml",
		},
		{
			name: "IPv6 already bracketed",
			host: "[::1]",
			cfg: config.DeployConfig{
				Token:   "t1",
				WebPort: 3000,
			},
			client: "v2rayng",
			want:   "http://[::1]:3000/sub/t1/v2rayng",
		},
		{
			name: "custom rule template",
			host: "example.com",
			cfg: config.DeployConfig{
				Token: "secret",
				Rule:  "/api/{token}/get/{client}",
			},
			client: "sing-box.json",
			want:   "http://example.com:8080/api/secret/get/sing-box.json",
		},
		{
			name: "token with special chars sanitized",
			host: "1.2.3.4",
			cfg: config.DeployConfig{
				Token: "bad!token@#",
			},
			client: "v2rayng",
			want:   "http://1.2.3.4:8080/sub/badtoken/v2rayng",
		},
		{
			name: "empty token falls back to default",
			host: "1.2.3.4",
			cfg: config.DeployConfig{
				Token: "",
			},
			client: "v2rayng",
			want:   fmt.Sprintf("http://1.2.3.4:8080/sub/%s/v2rayng", config.DefaultToken),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildSubscriptionURL(tt.host, tt.cfg, tt.client)
			if got != tt.want {
				t.Errorf("BuildSubscriptionURL(%q, _, %q) = %q, want %q",
					tt.host, tt.client, got, tt.want)
			}
		})
	}
}
