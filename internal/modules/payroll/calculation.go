package payroll

import (
	"encoding/json"
	"errors"
	"math"
)

type ProgressiveBracket struct {
	UpTo *int64  `json:"upTo"`
	Rate float64 `json:"rate"`
}

type Policy struct {
	BPJSHealthEmployeeRate     float64              `json:"bpjsHealthEmployeeRate"`
	BPJSEmploymentEmployeeRate float64              `json:"bpjsEmploymentEmployeeRate"`
	BPJSMonthlyWageCap         int64                `json:"bpjsMonthlyWageCap"`
	AnnualNonTaxableIncome     map[string]int64     `json:"annualNonTaxableIncome"`
	ProgressiveTaxBrackets     []ProgressiveBracket `json:"progressiveTaxBrackets"`
}

type Input struct {
	MonthlyGross          int64
	TaxStatus             string
	BPJSHealthEnabled     bool
	BPJSEmploymentEnabled bool
	OtherDeduction        int64
}

type Result struct {
	GrossPay       int64 `json:"grossPay"`
	BPJSDeduction  int64 `json:"bpjsDeduction"`
	PPh21Deduction int64 `json:"pph21Deduction"`
	OtherDeduction int64 `json:"otherDeduction"`
	NetPay         int64 `json:"netPay"`
}

func ParsePolicy(raw json.RawMessage) (Policy, error) {
	var policy Policy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return Policy{}, err
	}
	if policy.BPJSMonthlyWageCap < 0 || len(policy.ProgressiveTaxBrackets) == 0 {
		return Policy{}, errors.New("invalid payroll policy")
	}
	return policy, nil
}

func Calculate(input Input, policy Policy) Result {
	wageBase := input.MonthlyGross
	if policy.BPJSMonthlyWageCap > 0 && wageBase > policy.BPJSMonthlyWageCap {
		wageBase = policy.BPJSMonthlyWageCap
	}
	bpjs := int64(0)
	if input.BPJSHealthEnabled {
		bpjs += round(float64(wageBase) * policy.BPJSHealthEmployeeRate)
	}
	if input.BPJSEmploymentEnabled {
		bpjs += round(float64(wageBase) * policy.BPJSEmploymentEmployeeRate)
	}

	annualGross := input.MonthlyGross * 12
	nontaxable := policy.AnnualNonTaxableIncome[input.TaxStatus]
	if nontaxable == 0 {
		nontaxable = policy.AnnualNonTaxableIncome["TK/0"]
	}
	taxable := annualGross - nontaxable
	if taxable < 0 {
		taxable = 0
	}
	annualTax := progressiveTax(taxable, policy.ProgressiveTaxBrackets)
	pph21 := round(float64(annualTax) / 12)

	net := input.MonthlyGross - bpjs - pph21 - input.OtherDeduction
	if net < 0 {
		net = 0
	}
	return Result{GrossPay: input.MonthlyGross, BPJSDeduction: bpjs, PPh21Deduction: pph21, OtherDeduction: input.OtherDeduction, NetPay: net}
}

func progressiveTax(taxable int64, brackets []ProgressiveBracket) int64 {
	remaining := taxable
	previousCap := int64(0)
	total := float64(0)
	for _, bracket := range brackets {
		if remaining <= 0 {
			break
		}
		portion := remaining
		if bracket.UpTo != nil {
			width := *bracket.UpTo - previousCap
			if portion > width {
				portion = width
			}
			previousCap = *bracket.UpTo
		}
		total += float64(portion) * bracket.Rate
		remaining -= portion
	}
	return round(total)
}

func round(value float64) int64 { return int64(math.Round(value)) }
