package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"proxy-installer/internal/quality"
)

func TestParseIPQualitySourcesSummary(t *testing.T) {
	ippure := base64.StdEncoding.EncodeToString([]byte(`{"ip":"203.0.113.7","fraudScore":35,"isResidential":true,"isBroadcast":false}`))
	html := base64.StdEncoding.EncodeToString([]byte(`<!doctype html><html><body>not an API response</body></html>`))
	out := "SOURCE=ippure|" + ippure + "\n" +
		"SOURCE_TEXT=ping0|" + html + "\n" +
		"SOURCE_TEXT=iplark|" + base64.StdEncoding.EncodeToString([]byte("ip=203.0.113.7\nloc=US\ncolo=LAX\nwarp=off")) + "\n"

	raw, errs := quality.ParseIPQualitySources(out)
	if errs["ping0"] != "html_response" {
		t.Fatalf("expected html_response for ping0, got %q", errs["ping0"])
	}
	summary, sites, sections := quality.BuildQualityReport(raw, errs)
	if summary["sourceSuccess"] != 2 || summary["sourceTotal"] != 3 {
		t.Fatalf("unexpected source summary: %+v", summary)
	}
	if len(sites) != 3 {
		t.Fatalf("expected three site reports, got %d", len(sites))
	}
	if len(sections) != 6 {
		t.Fatalf("expected six report sections, got %d", len(sections))
	}
}

func TestSanitizeLogMessage(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		redact []string // substrings that must NOT appear
		keep   []string // substrings that must remain
	}{
		{
			name:   "password equals",
			input:  "login password=SuperSecret123 done",
			redact: []string{"SuperSecret123"},
			keep:   []string{"password="},
		},
		{
			name:   "password colon",
			input:  `auth password:"myP@ssw0rd"`,
			redact: []string{"myP@ssw0rd"},
			keep:   []string{"password:"},
		},
		{
			name:   "token equals",
			input:  "api token=abc123def456_ghi",
			redact: []string{"abc123def456_ghi"},
			keep:   []string{"token="},
		},
		{
			name:   "private_key equals",
			input:  "key private_key=AAAABBBBCCCCDDDD end",
			redact: []string{"AAAABBBBCCCCDDDD"},
			keep:   []string{"private_key="},
		},
		{
			name:   "public-key equals",
			input:  "cert public-key=XXXX1234YYYY end",
			redact: []string{"XXXX1234YYYY"},
			keep:   []string{"public-key="},
		},
		{
			name:   "UUID v4",
			input:  "id 550e8400-e29b-41d4-a716-446655440000 ok",
			redact: []string{"550e8400-e29b-41d4-a716-446655440000"},
			keep:   []string{"id"},
		},
		{
			name:   "mixed sensitive",
			input:  "deploy password=abc123 token=tok_xyz789 key=550e8400-e29b-41d4-a716-446655440000",
			redact: []string{"abc123", "tok_xyz789", "550e8400-e29b-41d4-a716-446655440000"},
			keep:   []string{"password=", "token="},
		},
		{
			name:   "no sensitive data",
			input:  "normal log message without secrets",
			redact: []string{},
			keep:   []string{"normal log message without secrets"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := sanitizeLogMessage(tt.input)
			for _, s := range tt.redact {
				if strings.Contains(out, s) {
					t.Errorf("redacted string still contains %q in output %q", s, out)
				}
			}
			for _, s := range tt.keep {
				if !strings.Contains(out, s) {
					t.Errorf("output %q missing expected substring %q", out, s)
				}
			}
		})
	}
}
