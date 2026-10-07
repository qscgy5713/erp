// Package money 集中金額與數量的捨入規則。
// 一律使用 decimal,禁止 float;捨入採四捨五入(half away from zero)。
package money

import "github.com/shopspring/decimal"

const (
	AmountPlaces    = 0 // 新台幣金額到元
	QuantityPlaces  = 4
	UnitPricePlaces = 6
	RatePlaces      = 6 // 匯率
)

// Amount 將本位幣金額捨入到元。
func Amount(d decimal.Decimal) decimal.Decimal { return d.Round(AmountPlaces) }

// Round 依指定小數位四捨五入(用於外幣金額等)。
func Round(d decimal.Decimal, places int32) decimal.Decimal { return d.Round(places) }

// TaxRate5 為營業稅 5%。
var TaxRate5 = decimal.RequireFromString("0.05")

// Tax 由未稅合計計算稅額(依單頭合計計算,再捨入到元)。
func Tax(untaxed, rate decimal.Decimal) decimal.Decimal {
	return Amount(untaxed.Mul(rate))
}

// SplitTaxIncluded 將含稅金額拆成未稅與稅額;稅額 = 含稅 − 未稅,確保兩者相加等於原金額。
func SplitTaxIncluded(included, rate decimal.Decimal) (untaxed, tax decimal.Decimal) {
	untaxed = Amount(included.Div(decimal.NewFromInt(1).Add(rate)))
	return untaxed, included.Sub(untaxed)
}
