package payroll

import "testing"

func ptr(value int64) *int64 { return &value }

func TestCalculateUsesEffectivePolicyConfiguration(t *testing.T) {
	policy := Policy{
		BPJSHealthEmployeeRate:     0.01,
		BPJSEmploymentEmployeeRate: 0.02,
		BPJSMonthlyWageCap:         12_000_000,
		AnnualNonTaxableIncome:     map[string]int64{"TK/0": 54_000_000},
		ProgressiveTaxBrackets: []ProgressiveBracket{
			{UpTo: ptr(60_000_000), Rate: 0.05},
			{UpTo: nil, Rate: 0.15},
		},
	}
	got := Calculate(Input{MonthlyGross: 10_000_000, TaxStatus: "TK/0", BPJSHealthEnabled: true, BPJSEmploymentEnabled: true}, policy)
	if got.BPJSDeduction != 300_000 {
		t.Fatalf("BPJS = %d, want 300000", got.BPJSDeduction)
	}
	if got.PPh21Deduction != 325_000 {
		t.Fatalf("PPh21 = %d, want 325000", got.PPh21Deduction)
	}
	if got.NetPay != 9_375_000 {
		t.Fatalf("net = %d, want 9375000", got.NetPay)
	}
}
