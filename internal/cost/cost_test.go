package cost

import (
	"math"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// CalcNextRenewal
// ---------------------------------------------------------------------------

func TestCalcNextRenewal(t *testing.T) {
	// Use a date far in the past so the "roll forward past now" logic is deterministic.
	pastDate := "2020-01-15"
	now := time.Now()

	tests := []struct {
		name         string
		purchaseDate string
		billingCycle string
		wantEmpty    bool
		checkFn      func(t *testing.T, result string) // optional custom check
	}{
		{
			name:         "lifetime returns empty",
			purchaseDate: "2024-06-01",
			billingCycle: "lifetime",
			wantEmpty:    true,
		},
		{
			name:         "invalid date returns empty",
			purchaseDate: "not-a-date",
			billingCycle: "monthly",
			wantEmpty:    true,
		},
		{
			name:         "unknown cycle returns empty",
			purchaseDate: "2024-01-01",
			billingCycle: "biweekly",
			wantEmpty:    true,
		},
		{
			name:         "empty date returns empty",
			purchaseDate: "",
			billingCycle: "monthly",
			wantEmpty:    true,
		},
		{
			name:         "monthly from past date rolls forward",
			purchaseDate: pastDate,
			billingCycle: "monthly",
			checkFn: func(t *testing.T, result string) {
				t.Helper()
				d, err := time.Parse("2006-01-02", result)
				if err != nil {
					t.Fatalf("parse result %q: %v", result, err)
				}
				if !d.After(now) {
					t.Errorf("expected date after now, got %v", d)
				}
				// Day of month should be 15 (same as purchase day).
				if d.Day() != 15 {
					t.Errorf("expected day 15, got %d", d.Day())
				}
			},
		},
		{
			name:         "quarterly from past date",
			purchaseDate: pastDate,
			billingCycle: "quarterly",
			checkFn: func(t *testing.T, result string) {
				t.Helper()
				d, err := time.Parse("2006-01-02", result)
				if err != nil {
					t.Fatalf("parse result %q: %v", result, err)
				}
				if !d.After(now) {
					t.Errorf("expected date after now, got %v", d)
				}
			},
		},
		{
			name:         "semiannual from past date",
			purchaseDate: pastDate,
			billingCycle: "semiannual",
			checkFn: func(t *testing.T, result string) {
				t.Helper()
				d, err := time.Parse("2006-01-02", result)
				if err != nil {
					t.Fatalf("parse result %q: %v", result, err)
				}
				if !d.After(now) {
					t.Errorf("expected date after now, got %v", d)
				}
			},
		},
		{
			name:         "annual from past date",
			purchaseDate: pastDate,
			billingCycle: "annual",
			checkFn: func(t *testing.T, result string) {
				t.Helper()
				d, err := time.Parse("2006-01-02", result)
				if err != nil {
					t.Fatalf("parse result %q: %v", result, err)
				}
				if !d.After(now) {
					t.Errorf("expected date after now, got %v", d)
				}
				// Should be Jan 15 of some future year.
				if d.Month() != time.January || d.Day() != 15 {
					t.Errorf("expected Jan 15, got %v", d)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcNextRenewal(tt.purchaseDate, tt.billingCycle)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty string, got %q", got)
				}
				return
			}
			if got == "" {
				t.Fatal("expected non-empty result, got empty")
			}
			if tt.checkFn != nil {
				tt.checkFn(t, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SaveInstance
// ---------------------------------------------------------------------------

func TestSaveInstance(t *testing.T) {
	t.Run("new instance gets auto ID", func(t *testing.T) {
		data := CostV2Data{Instances: []VPSInstance{}}
		inst := VPSInstance{
			VPSName:      "test-vps",
			Price:        10.0,
			Currency:     "USD",
			BillingCycle: "monthly",
			PurchaseDate: "2024-01-01",
			ProviderName: "TestProvider",
		}

		result := SaveInstance(data, inst)

		if len(result.Instances) != 1 {
			t.Fatalf("expected 1 instance, got %d", len(result.Instances))
		}
		saved := result.Instances[0]
		if !strings.HasPrefix(saved.ID, "inst_") {
			t.Errorf("expected auto-generated ID with 'inst_' prefix, got %q", saved.ID)
		}
		if saved.VPSName != "test-vps" {
			t.Errorf("VPSName = %q, want %q", saved.VPSName, "test-vps")
		}
		// NextRenewal should be computed (not ManualRenewal).
		if saved.NextRenewal == "" {
			t.Error("expected NextRenewal to be set for non-manual, non-lifetime instance")
		}
	})

	t.Run("update existing instance by ID", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{ID: "inst_abc", VPSName: "old-name", Price: 5.0},
			},
		}
		updated := VPSInstance{
			ID:            "inst_abc",
			VPSName:       "new-name",
			Price:         15.0,
			ManualRenewal: true,
		}

		result := SaveInstance(data, updated)

		if len(result.Instances) != 1 {
			t.Fatalf("expected 1 instance, got %d", len(result.Instances))
		}
		if result.Instances[0].VPSName != "new-name" {
			t.Errorf("VPSName = %q, want %q", result.Instances[0].VPSName, "new-name")
		}
		if result.Instances[0].Price != 15.0 {
			t.Errorf("Price = %v, want %v", result.Instances[0].Price, 15.0)
		}
	})

	t.Run("manual renewal skips NextRenewal calculation", func(t *testing.T) {
		data := CostV2Data{Instances: []VPSInstance{}}
		inst := VPSInstance{
			ID:            "inst_manual",
			ManualRenewal: true,
			BillingCycle:  "monthly",
			PurchaseDate:  "2024-01-01",
		}

		result := SaveInstance(data, inst)
		if result.Instances[0].NextRenewal != "" {
			t.Errorf("expected empty NextRenewal for manual renewal, got %q", result.Instances[0].NextRenewal)
		}
	})
}

// ---------------------------------------------------------------------------
// DeleteInstance
// ---------------------------------------------------------------------------

func TestDeleteInstance(t *testing.T) {
	t.Run("delete existing", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{ID: "inst_1", VPSName: "one"},
				{ID: "inst_2", VPSName: "two"},
				{ID: "inst_3", VPSName: "three"},
			},
		}

		result := DeleteInstance(data, "inst_2")

		if len(result.Instances) != 2 {
			t.Fatalf("expected 2 instances, got %d", len(result.Instances))
		}
		for _, inst := range result.Instances {
			if inst.ID == "inst_2" {
				t.Error("inst_2 should have been deleted")
			}
		}
	})

	t.Run("delete non-existing leaves data unchanged", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{ID: "inst_1", VPSName: "one"},
			},
		}

		result := DeleteInstance(data, "inst_nonexistent")

		if len(result.Instances) != 1 {
			t.Fatalf("expected 1 instance, got %d", len(result.Instances))
		}
		if result.Instances[0].ID != "inst_1" {
			t.Errorf("remaining instance ID = %q, want %q", result.Instances[0].ID, "inst_1")
		}
	})

	t.Run("delete from empty list", func(t *testing.T) {
		data := CostV2Data{Instances: []VPSInstance{}}
		result := DeleteInstance(data, "anything")
		if len(result.Instances) != 0 {
			t.Errorf("expected 0 instances, got %d", len(result.Instances))
		}
	})
}

// ---------------------------------------------------------------------------
// CalcSummary
// ---------------------------------------------------------------------------

func almostEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestCalcSummary(t *testing.T) {
	t.Run("multi-currency monthly", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{Price: 10.0, Currency: "USD", BillingCycle: "monthly", ProviderName: "AWS"},
				{Price: 20.0, Currency: "USD", BillingCycle: "monthly", ProviderName: "AWS"},
				{Price: 100.0, Currency: "CNY", BillingCycle: "monthly", ProviderName: "Aliyun"},
			},
		}

		summary := CalcSummary(data)

		if summary.Total != 3 {
			t.Errorf("Total = %d, want 3", summary.Total)
		}
		if summary.Vendors != 2 {
			t.Errorf("Vendors = %d, want 2", summary.Vendors)
		}
		if !almostEqual(summary.Monthly["USD"], 30.0, 0.001) {
			t.Errorf("Monthly[USD] = %v, want 30.0", summary.Monthly["USD"])
		}
		if !almostEqual(summary.Monthly["CNY"], 100.0, 0.001) {
			t.Errorf("Monthly[CNY] = %v, want 100.0", summary.Monthly["CNY"])
		}
	})

	t.Run("lifetime excluded from monthly", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{Price: 500.0, Currency: "USD", BillingCycle: "lifetime", ProviderName: "BuyOnce"},
				{Price: 12.0, Currency: "USD", BillingCycle: "monthly", ProviderName: "MonthlyCo"},
			},
		}

		summary := CalcSummary(data)

		// Total counts all instances including lifetime.
		if summary.Total != 2 {
			t.Errorf("Total = %d, want 2", summary.Total)
		}
		// Monthly should only include the monthly instance.
		if !almostEqual(summary.Monthly["USD"], 12.0, 0.001) {
			t.Errorf("Monthly[USD] = %v, want 12.0", summary.Monthly["USD"])
		}
	})

	t.Run("annual divided by 12", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{Price: 120.0, Currency: "EUR", BillingCycle: "annual", ProviderName: "EuroHost"},
			},
		}

		summary := CalcSummary(data)

		if !almostEqual(summary.Monthly["EUR"], 10.0, 0.001) {
			t.Errorf("Monthly[EUR] = %v, want 10.0", summary.Monthly["EUR"])
		}
	})

	t.Run("quarterly divided by 3", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{Price: 30.0, Currency: "USD", BillingCycle: "quarterly", ProviderName: "QHost"},
			},
		}

		summary := CalcSummary(data)

		if !almostEqual(summary.Monthly["USD"], 10.0, 0.001) {
			t.Errorf("Monthly[USD] = %v, want 10.0", summary.Monthly["USD"])
		}
	})

	t.Run("semiannual divided by 6", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{Price: 60.0, Currency: "USD", BillingCycle: "semiannual", ProviderName: "SHost"},
			},
		}

		summary := CalcSummary(data)

		if !almostEqual(summary.Monthly["USD"], 10.0, 0.001) {
			t.Errorf("Monthly[USD] = %v, want 10.0", summary.Monthly["USD"])
		}
	})

	t.Run("empty currency defaults to CNY", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{Price: 50.0, Currency: "", BillingCycle: "monthly", ProviderName: "NoCur"},
			},
		}

		summary := CalcSummary(data)

		if !almostEqual(summary.Monthly["CNY"], 50.0, 0.001) {
			t.Errorf("Monthly[CNY] = %v, want 50.0", summary.Monthly["CNY"])
		}
	})

	t.Run("empty data", func(t *testing.T) {
		data := CostV2Data{Instances: []VPSInstance{}}
		summary := CalcSummary(data)

		if summary.Total != 0 {
			t.Errorf("Total = %d, want 0", summary.Total)
		}
		if summary.Vendors != 0 {
			t.Errorf("Vendors = %d, want 0", summary.Vendors)
		}
		if len(summary.Monthly) != 0 {
			t.Errorf("Monthly should be empty, got %v", summary.Monthly)
		}
	})

	t.Run("vendor count ignores empty provider name", func(t *testing.T) {
		data := CostV2Data{
			Instances: []VPSInstance{
				{Price: 10.0, Currency: "USD", BillingCycle: "monthly", ProviderName: ""},
				{Price: 20.0, Currency: "USD", BillingCycle: "monthly", ProviderName: "AWS"},
			},
		}

		summary := CalcSummary(data)

		if summary.Vendors != 1 {
			t.Errorf("Vendors = %d, want 1", summary.Vendors)
		}
	})
}

// ---------------------------------------------------------------------------
// LinkProfile
// ---------------------------------------------------------------------------

func TestLinkProfile(t *testing.T) {
	data := CostV2Data{
		Instances: []VPSInstance{
			{ID: "inst_1", VPSName: "one"},
			{ID: "inst_2", VPSName: "two"},
		},
	}

	t.Run("found instance", func(t *testing.T) {
		result, ok := LinkProfile(data, "inst_1", "profile_abc")
		if !ok {
			t.Fatal("expected ok=true for existing instance")
		}
		if result.Instances[0].ProfileID != "profile_abc" {
			t.Errorf("ProfileID = %q, want %q", result.Instances[0].ProfileID, "profile_abc")
		}
		// Second instance should be unchanged.
		if result.Instances[1].ProfileID != "" {
			t.Errorf("second instance ProfileID = %q, want empty", result.Instances[1].ProfileID)
		}
	})

	t.Run("not found instance", func(t *testing.T) {
		result, ok := LinkProfile(data, "inst_nonexistent", "profile_xyz")
		if ok {
			t.Error("expected ok=false for non-existing instance")
		}
		// Data should be unchanged.
		if len(result.Instances) != 2 {
			t.Errorf("expected 2 instances unchanged, got %d", len(result.Instances))
		}
	})
}

// ---------------------------------------------------------------------------
// GetCostV2 / SetCostV2
// ---------------------------------------------------------------------------

func TestGetSetCostV2(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		extra := map[string]any{}
		original := CostV2Data{
			Instances: []VPSInstance{
				{
					ID:           "inst_1",
					VPSName:      "my-vps",
					Price:        25.50,
					Currency:     "USD",
					BillingCycle: "monthly",
					ProviderName: "TestCloud",
				},
			},
		}

		SetCostV2(extra, original)
		retrieved := GetCostV2(extra)

		if len(retrieved.Instances) != 1 {
			t.Fatalf("expected 1 instance, got %d", len(retrieved.Instances))
		}
		got := retrieved.Instances[0]
		if got.ID != "inst_1" {
			t.Errorf("ID = %q, want %q", got.ID, "inst_1")
		}
		if got.VPSName != "my-vps" {
			t.Errorf("VPSName = %q, want %q", got.VPSName, "my-vps")
		}
		if got.Price != 25.50 {
			t.Errorf("Price = %v, want 25.50", got.Price)
		}
		if got.Currency != "USD" {
			t.Errorf("Currency = %q, want %q", got.Currency, "USD")
		}
	})

	t.Run("empty extra returns empty instances", func(t *testing.T) {
		extra := map[string]any{}
		data := GetCostV2(extra)
		if len(data.Instances) != 0 {
			t.Errorf("expected 0 instances, got %d", len(data.Instances))
		}
	})

	t.Run("nil value in extra returns empty instances", func(t *testing.T) {
		extra := map[string]any{ExtraKeyCostV2: nil}
		data := GetCostV2(extra)
		if len(data.Instances) != 0 {
			t.Errorf("expected 0 instances, got %d", len(data.Instances))
		}
	})

	t.Run("invalid JSON type returns empty instances", func(t *testing.T) {
		// Store something that cannot be unmarshalled into CostV2Data.
		extra := map[string]any{ExtraKeyCostV2: 42}
		data := GetCostV2(extra)
		// json.Marshal(42) produces "42", json.Unmarshal into CostV2Data will fail.
		if len(data.Instances) != 0 {
			t.Errorf("expected 0 instances for invalid data, got %d", len(data.Instances))
		}
	})

	t.Run("multiple instances round-trip", func(t *testing.T) {
		extra := map[string]any{}
		original := CostV2Data{
			Instances: []VPSInstance{
				{ID: "a", VPSName: "alpha"},
				{ID: "b", VPSName: "beta"},
				{ID: "c", VPSName: "gamma"},
			},
		}

		SetCostV2(extra, original)
		retrieved := GetCostV2(extra)

		if len(retrieved.Instances) != 3 {
			t.Fatalf("expected 3 instances, got %d", len(retrieved.Instances))
		}
		for i, inst := range retrieved.Instances {
			if inst.ID != original.Instances[i].ID {
				t.Errorf("instance[%d].ID = %q, want %q", i, inst.ID, original.Instances[i].ID)
			}
		}
	})
}
