package deploy

import (
	"regexp"
	"strings"
	"testing"

	"proxy-installer/internal/config"
)

// ---------------------------------------------------------------------------
// SafeToken
// ---------------------------------------------------------------------------

func TestSafeToken(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"normal alphanumeric", "abc123", "abc123"},
		{"underscores and dashes", "a-b_c", "a-b_c"},
		{"empty falls back to default", "", config.DefaultToken},
		{"only special chars falls back", "@#$%^&", config.DefaultToken},
		{"special chars stripped", "he!lo@world", "heloworld"},
		{"keeps digits", "12345", "12345"},
		{"mixed case preserved", "AbCdEf", "AbCdEf"},
		{"spaces removed", "a b c", "abc"},
		{"max 64 chars", strings.Repeat("a", 100), strings.Repeat("a", 64)},
		{"exactly 64 chars unchanged", strings.Repeat("b", 64), strings.Repeat("b", 64)},
		{"unicode stripped", "hello世界", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeToken(tt.in)
			if got != tt.want {
				t.Errorf("SafeToken(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SafeName
// ---------------------------------------------------------------------------

func TestSafeName(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		fallback string
		want     string
	}{
		{"normal", "my-server", "fb", "my-server"},
		{"dots allowed", "node.v2", "fb", "node.v2"},
		{"underscores allowed", "my_node", "fb", "my_node"},
		{"illegal chars replaced with dash", "hello world!", "fb", "hello-world-"},
		{"empty string uses fallback", "", "fallback", "fallback"},
		{"whitespace only uses fallback", "   ", "fallback", "fallback"},
		{"truncation at 64", strings.Repeat("x", 100), "fb", strings.Repeat("x", 64)},
		{"exactly 64 unchanged", strings.Repeat("y", 64), "fb", strings.Repeat("y", 64)},
		{"chinese replaced", "服务器", "fb", "---"},
		{"mixed legal and illegal", "a@b#c.d", "fb", "a-b-c.d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeName(tt.in, tt.fallback)
			if got != tt.want {
				t.Errorf("SafeName(%q, %q) = %q, want %q", tt.in, tt.fallback, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SafeDomain
// ---------------------------------------------------------------------------

func TestSafeDomain(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		fallback string
		want     string
	}{
		{"normal domain", "example.com", "fb", "example.com"},
		{"subdomain", "a.b.c.example.com", "fb", "a.b.c.example.com"},
		{"hyphens allowed", "my-domain.co.uk", "fb", "my-domain.co.uk"},
		{"underscores removed", "my_domain.com", "fb", "mydomain.com"},
		{"spaces removed", "example .com", "fb", "example.com"},
		{"empty string uses fallback", "", "fallback.test", "fallback.test"},
		{"whitespace only uses fallback", "  ", "fallback.test", "fallback.test"},
		{"illegal chars removed not replaced", "a!b@c#d", "fb", "abcd"},
		{"max 253 chars", strings.Repeat("a", 300) + ".com", "fb", strings.Repeat("a", 253)},
		{"exactly 253 unchanged", strings.Repeat("z", 253), "fb", strings.Repeat("z", 253)},
		{"chinese removed", "测试.com", "fb", ".com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeDomain(tt.in, tt.fallback)
			if got != tt.want {
				t.Errorf("SafeDomain(%q, %q) = %q, want %q", tt.in, tt.fallback, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FilterSupportedProtocols
// ---------------------------------------------------------------------------

func TestFilterSupportedProtocols(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"all protocols in order",
			[]string{"vless-reality", "hy2", "tuic", "trojan", "ss", "vmess"},
			[]string{"vless-reality", "hy2", "tuic", "trojan", "ss", "vmess"}},
		{"unknown filtered out",
			[]string{"vless-reality", "unknown", "ss"},
			[]string{"vless-reality", "ss"}},
		{"deduplication",
			[]string{"hy2", "hy2", "trojan", "trojan"},
			[]string{"hy2", "trojan"}},
		{"empty input",
			[]string{},
			nil},
		{"nil input",
			nil,
			nil},
		{"all unknown",
			[]string{"foo", "bar"},
			nil},
		{"ordering preserved",
			[]string{"vmess", "ss", "hy2"},
			[]string{"vmess", "ss", "hy2"}},
		{"single valid",
			[]string{"tuic"},
			[]string{"tuic"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterSupportedProtocols(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("FilterSupportedProtocols(%v) = %v (len %d), want %v (len %d)",
					tt.input, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("FilterSupportedProtocols(%v)[%d] = %q, want %q",
						tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PortOrDefault
// ---------------------------------------------------------------------------

func TestPortOrDefault(t *testing.T) {
	tests := []struct {
		name  string
		ports map[string]int
		key   string
		def   int
		want  int
	}{
		{"found in map", map[string]int{"hy2": 9443}, "hy2", 8443, 9443},
		{"not found returns default", map[string]int{"ss": 8388}, "hy2", 8443, 8443},
		{"nil map returns default", nil, "hy2", 8443, 8443},
		{"zero value returns default", map[string]int{"hy2": 0}, "hy2", 8443, 8443},
		{"negative value returns default", map[string]int{"hy2": -1}, "hy2", 8443, 8443},
		{"empty map returns default", map[string]int{}, "hy2", 8443, 8443},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PortOrDefault(tt.ports, tt.key, tt.def)
			if got != tt.want {
				t.Errorf("PortOrDefault(%v, %q, %d) = %d, want %d",
					tt.ports, tt.key, tt.def, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PublicPortOrDefault
// ---------------------------------------------------------------------------

func TestPublicPortOrDefault(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.DeployConfig
		key  string
		def  int
		want int
	}{
		{
			"public port takes precedence",
			config.DeployConfig{
				PublicPorts: map[string]int{"hy2": 9443},
				Ports:       map[string]int{"hy2": 7443},
			},
			"hy2", 8443, 9443,
		},
		{
			"fallback to inner ports",
			config.DeployConfig{
				PublicPorts: map[string]int{"ss": 9999},
				Ports:       map[string]int{"hy2": 7443},
			},
			"hy2", 8443, 7443,
		},
		{
			"fallback to default when neither has key",
			config.DeployConfig{
				PublicPorts: map[string]int{},
				Ports:       map[string]int{},
			},
			"hy2", 8443, 8443,
		},
		{
			"nil public ports falls through to ports",
			config.DeployConfig{
				Ports: map[string]int{"trojan": 9445},
			},
			"trojan", 8445, 9445,
		},
		{
			"all nil returns default",
			config.DeployConfig{},
			"vmess", 2083, 2083,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PublicPortOrDefault(tt.cfg, tt.key, tt.def)
			if got != tt.want {
				t.Errorf("PublicPortOrDefault(_, %q, %d) = %d, want %d",
					tt.key, tt.def, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PublicWebPortOrDefault
// ---------------------------------------------------------------------------

func TestPublicWebPortOrDefault(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.DeployConfig
		want int
	}{
		{"public web port first", config.DeployConfig{PublicWebPort: 443, WebPort: 9090}, 443},
		{"fallback to web port", config.DeployConfig{WebPort: 9090}, 9090},
		{"fallback to default", config.DeployConfig{}, config.DefaultWebPort},
		{"zero public falls through", config.DeployConfig{PublicWebPort: 0, WebPort: 3000}, 3000},
		{"both zero returns default", config.DeployConfig{PublicWebPort: 0, WebPort: 0}, config.DefaultWebPort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PublicWebPortOrDefault(tt.cfg)
			if got != tt.want {
				t.Errorf("PublicWebPortOrDefault(_) = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ProtocolLabel
// ---------------------------------------------------------------------------

func TestProtocolLabel(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"vless-reality", "VLESS Reality"},
		{"hy2", "Hysteria2"},
		{"tuic", "TUIC"},
		{"trojan", "Trojan"},
		{"ss", "Shadowsocks"},
		{"vmess", "VMess"},
		{"unknown-proto", "unknown-proto"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := ProtocolLabel(tt.id)
			if got != tt.want {
				t.Errorf("ProtocolLabel(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// UsesUDPProtocol
// ---------------------------------------------------------------------------

func TestUsesUDPProtocol(t *testing.T) {
	tests := []struct {
		name     string
		selected []string
		want     bool
	}{
		{"hy2 present", []string{"vless-reality", "hy2"}, true},
		{"tuic present", []string{"tuic", "ss"}, true},
		{"both hy2 and tuic", []string{"hy2", "tuic"}, true},
		{"neither", []string{"vless-reality", "trojan", "ss", "vmess"}, false},
		{"empty", []string{}, false},
		{"nil", nil, false},
		{"only hy2", []string{"hy2"}, true},
		{"only tuic", []string{"tuic"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UsesUDPProtocol(tt.selected)
			if got != tt.want {
				t.Errorf("UsesUDPProtocol(%v) = %v, want %v", tt.selected, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NormalizeHostLiteral
// ---------------------------------------------------------------------------

func TestNormalizeHostLiteral(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain IPv4", "1.2.3.4", "1.2.3.4"},
		{"plain domain", "example.com", "example.com"},
		{"http prefix stripped", "http://example.com", "example.com"},
		{"https prefix stripped", "https://example.com", "example.com"},
		{"http with path stripped", "http://example.com/path", "example.com"},
		{"brackets removed", "[::1]", "::1"},
		{"IPv6 bare", "::1", "::1"},
		{"IPv6 with brackets in URL", "http://[::1]", "::1"},
		{"IPv6 full URL", "https://[2001:db8::1]:8080/path", "2001:db8::1"},
		{"whitespace trimmed", "  example.com  ", "example.com"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeHostLiteral(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeHostLiteral(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StableUUID — determinism regression test
// (regression: was previously random via crypto/rand, breaking re-deploy identity)
// ---------------------------------------------------------------------------

var uuidV4Regex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestStableUUID(t *testing.T) {
	// 1. Determinism: same seed must always produce the same UUID.
	if got1, got2 := StableUUID("abc"), StableUUID("abc"); got1 != got2 {
		t.Errorf("StableUUID not deterministic: %q != %q", got1, got2)
	}

	// 2. Distinctness: different seeds must produce different UUIDs.
	if StableUUID("alpha") == StableUUID("beta") {
		t.Error("StableUUID collision for distinct seeds")
	}

	// 3. Format: must be a spec-valid UUID v4 (version + variant bits).
	for _, seed := range []string{"abc", "", "some-long-token-123", "中文 token"} {
		if got := StableUUID(seed); !uuidV4Regex.MatchString(got) {
			t.Errorf("StableUUID(%q) = %q, not a valid UUID v4", seed, got)
		}
	}

	// 4. Empty seed is stable and well-formed.
	if StableUUID("") != StableUUID("") {
		t.Error("StableUUID(\"\") is not deterministic")
	}
}
