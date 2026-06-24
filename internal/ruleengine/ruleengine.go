package ruleengine

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

type RuleAction string

const (
	ActionDirect RuleAction = "direct"
	ActionProxy  RuleAction = "proxy"
	ActionBlock  RuleAction = "block"
)

type ruleEntry struct {
	DomainSuffix []string `json:"domain_suffix,omitempty"`
	IPCIDR       []string `json:"ip_cidr,omitempty"`
	Outbound     string   `json:"outbound"`
}

type rulesOutput struct {
	Rules []ruleEntry `json:"rules"`
}

func isDomainValid(d string) bool {
	if len(d) == 0 || len(d) > 255 {
		return false
	}
	for i, c := range d {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.':
		case c == '-':
		case c == '_':
		case c == '*' && i == 0:
		default:
			return false
		}
		if c == '.' && (i == 0 || i == len(d)-1) {
			return false
		}
		if c == '-' && (i == 0 || i == len(d)-1) {
			return false
		}
	}
	return true
}

func ValidateInput(domains, cidrs []string) []string {
	var warnings []string

	for i, d := range domains {
		d = strings.TrimSpace(d)
		line := i + 1
		if d == "" {
			continue
		}
		clean := d
		if strings.HasPrefix(clean, "*.") {
			clean = clean[2:]
		}
		if !isDomainValid(clean) {
			warnings = append(warnings, fmt.Sprintf("域名第 %d 行: 格式无效 — %s", line, d))
		}
	}

	for i, c := range cidrs {
		c = strings.TrimSpace(c)
		line := i + 1
		if c == "" {
			continue
		}
		if _, err := netip.ParsePrefix(c); err != nil {
			warnings = append(warnings, fmt.Sprintf("CIDR 第 %d 行: 格式无效 — %s (%v)", line, c, err))
		}
	}

	return warnings
}

func BuildRules(domains, cidrs []string, action RuleAction) (string, []string, error) {
	warnings := ValidateInput(domains, cidrs)

	var suffixes []string
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		clean := d
		if strings.HasPrefix(clean, "*.") {
			clean = clean[2:]
		}
		if isDomainValid(clean) {
			suffixes = append(suffixes, clean)
		}
	}

	var validCIDRs []string
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, err := netip.ParsePrefix(c); err == nil {
			validCIDRs = append(validCIDRs, c)
		}
	}

	if len(suffixes) == 0 && len(validCIDRs) == 0 {
		return "", warnings, fmt.Errorf("没有有效的域名或 CIDR 输入")
	}

	var rules []ruleEntry
	if len(suffixes) > 0 {
		rules = append(rules, ruleEntry{
			DomainSuffix: suffixes,
			Outbound:     string(action),
		})
	}
	if len(validCIDRs) > 0 {
		rules = append(rules, ruleEntry{
			IPCIDR:   validCIDRs,
			Outbound: string(action),
		})
	}

	data, err := json.MarshalIndent(rulesOutput{Rules: rules}, "", "  ")
	if err != nil {
		return "", warnings, fmt.Errorf("序列化规则失败: %w", err)
	}

	return string(data), warnings, nil
}
