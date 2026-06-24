// Package cost 负责 VPS 成本管理：实例数据模型、CRUD 操作和聚合统计。
// 从 app.go 提取，通过 config.AppState 进行持久化（由调用方负责 LoadAppState/SaveAppState）。
package cost

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

// ExtraKeyCostV2 是 AppState.Extra 中存储成本数据的键名
const ExtraKeyCostV2 = "cost_v2"

// VPSInstance 表示一个 VPS 实例的完整信息（含定价、规格、供应商等）
type VPSInstance struct {
	ID            string  `json:"id"`
	VPSName       string  `json:"vpsName"`
	Host          string  `json:"host,omitempty"`
	CPU           int     `json:"cpu"`
	MemoryGB      float64 `json:"memory_gb"`
	DiskGB        float64 `json:"disk_gb"`
	BandwidthMbps int     `json:"bandwidth_mbps"`
	TrafficGB     int     `json:"traffic_gb"`
	IPv4Count     int     `json:"ipv4Count"`
	IPv4Address   string  `json:"ipv4Address,omitempty"`
	IPv6Count     int     `json:"ipv6Count"`
	IPv6Address   string  `json:"ipv6Address,omitempty"`
	Price         float64 `json:"price"`
	Currency      string  `json:"currency"`
	BillingCycle  string  `json:"billingCycle"`
	PurchaseDate  string  `json:"purchaseDate"`
	NextRenewal   string  `json:"nextRenewal"`
	ManualRenewal bool    `json:"manualRenewal"`
	ProviderName  string  `json:"providerName,omitempty"`
	ProviderURL   string  `json:"providerURL,omitempty"`
	PlanName      string  `json:"planName,omitempty"`
	OS            string  `json:"os,omitempty"`
	CpuModel      string  `json:"cpuModel,omitempty"`
	ProfileID     string  `json:"profileId,omitempty"`
	Notes         string  `json:"notes,omitempty"`
}

// CostV2Data 包装 VPS 实例列表
type CostV2Data struct {
	Instances []VPSInstance `json:"instances"`
}

// GetCostV2 从 AppState.Extra 中提取成本数据
func GetCostV2(extra map[string]any) CostV2Data {
	raw, ok := extra[ExtraKeyCostV2]
	if !ok || raw == nil {
		return CostV2Data{Instances: []VPSInstance{}}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return CostV2Data{Instances: []VPSInstance{}}
	}
	var c CostV2Data
	if err := json.Unmarshal(data, &c); err != nil {
		return CostV2Data{Instances: []VPSInstance{}}
	}
	if c.Instances == nil {
		c.Instances = []VPSInstance{}
	}
	return c
}

// SetCostV2 将成本数据写入 AppState.Extra
func SetCostV2(extra map[string]any, data CostV2Data) {
	extra[ExtraKeyCostV2] = data
}

// NewInstanceID 生成唯一的实例 ID
func NewInstanceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("inst_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("inst_%x", b)
}

// CalcNextRenewal 根据购买日期和计费周期计算下次续费日期
func CalcNextRenewal(purchaseDate, billingCycle string) string {
	if billingCycle == "lifetime" {
		return ""
	}
	purchase, err := time.Parse("2006-01-02", purchaseDate)
	if err != nil {
		return ""
	}
	var months int
	switch billingCycle {
	case "monthly":
		months = 1
	case "quarterly":
		months = 3
	case "semiannual":
		months = 6
	case "annual":
		months = 12
	default:
		return ""
	}
	now := time.Now()
	next := purchase
	for !next.After(now) {
		next = next.AddDate(0, months, 0)
	}
	return next.Format("2006-01-02")
}

// SaveInstance 新增或更新 VPS 实例，返回更新后的数据
func SaveInstance(data CostV2Data, instance VPSInstance) CostV2Data {
	if instance.ID == "" {
		instance.ID = NewInstanceID()
	}
	if !instance.ManualRenewal {
		instance.NextRenewal = CalcNextRenewal(instance.PurchaseDate, instance.BillingCycle)
	}
	found := false
	for i, v := range data.Instances {
		if v.ID == instance.ID {
			data.Instances[i] = instance
			found = true
			break
		}
	}
	if !found {
		data.Instances = append(data.Instances, instance)
	}
	return data
}

// DeleteInstance 根据 ID 删除 VPS 实例，返回更新后的数据
func DeleteInstance(data CostV2Data, id string) CostV2Data {
	for i, v := range data.Instances {
		if v.ID == id {
			data.Instances = append(data.Instances[:i], data.Instances[i+1:]...)
			break
		}
	}
	return data
}

// Summary 聚合统计结果
type Summary struct {
	Vendors         int                `json:"vendors"`
	Total           int                `json:"total"`
	Monthly         map[string]float64 `json:"monthly"`
}

// CalcSummary 计算成本聚合统计：供应商数量、实例总数、按币种月均费用
func CalcSummary(data CostV2Data) Summary {
	providerSet := map[string]bool{}
	monthlyByCurrency := map[string]float64{}
	for _, inst := range data.Instances {
		if inst.ProviderName != "" {
			providerSet[inst.ProviderName] = true
		}
		if inst.BillingCycle == "lifetime" {
			continue
		}
		var months int
		switch inst.BillingCycle {
		case "monthly":
			months = 1
		case "quarterly":
			months = 3
		case "semiannual":
			months = 6
		case "annual":
			months = 12
		default:
			continue
		}
		monthly := inst.Price / float64(months)
		cur := inst.Currency
		if cur == "" {
			cur = "CNY"
		}
		monthlyByCurrency[cur] += monthly
	}
	return Summary{
		Vendors: len(providerSet),
		Total:   len(data.Instances),
		Monthly: monthlyByCurrency,
	}
}

// LinkProfile 关联 SSH 配置到 VPS 实例
func LinkProfile(data CostV2Data, instanceID, profileID string) (CostV2Data, bool) {
	for i, v := range data.Instances {
		if v.ID == instanceID {
			data.Instances[i].ProfileID = profileID
			return data, true
		}
	}
	return data, false
}
