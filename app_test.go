package main

import (
	"encoding/base64"
	"testing"
)

func TestParseIPQualitySourcesSummary(t *testing.T) {
	ippure := base64.StdEncoding.EncodeToString([]byte(`{"ip":"203.0.113.7","fraudScore":35,"isResidential":true,"isBroadcast":false}`))
	html := base64.StdEncoding.EncodeToString([]byte(`<!doctype html><html><body>not an API response</body></html>`))
	out := "SOURCE=ippure|" + ippure + "\n" +
		"SOURCE_TEXT=ping0|" + html + "\n" +
		"SOURCE_TEXT=iplark|" + base64.StdEncoding.EncodeToString([]byte("ip=203.0.113.7\nloc=US\ncolo=LAX\nwarp=off")) + "\n"

	raw, errs := parseIPQualitySources(out)
	if errs["ping0"] != "html_response" {
		t.Fatalf("expected html_response for ping0, got %q", errs["ping0"])
	}
	summary, sites, sections := buildQualityReport(raw, errs)
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
