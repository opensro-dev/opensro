/*
===========================================================================

tradequotation_test.go - trader arithmetic against original machine code

Frozen values execute the pinned server's instructions in Unicorn. Cases
vary stock, rates, float32 boundaries and signed multiplication overflow.

===========================================================================
*/
package commerce

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

/*
================
TestTradeQuotationsAgainstNativeInstructions
================
*/
func TestTradeQuotationsAgainstNativeInstructions(t *testing.T) {
	data, err := os.ReadFile("testdata/native-trade-quotations.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Precision  int
		Quotations []struct {
			TradeQuotation
			BasePrice uint32
			Price     uint32
		}
		Thieves []struct {
			BasePrice uint32
			Quantity  uint16
			Credit    int64
			Profit    int64
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Precision != 53 || len(corpus.Quotations) != 891 || len(corpus.Thieves) != 72 {
		t.Fatal("incomplete native quotation matrix")
	}
	for _, row := range corpus.Quotations {
		got, err := TradeQuotationPrice(row.BasePrice, row.TradeQuotation)
		if err != nil || got != row.Price {
			t.Fatalf("base=%d quotation=%+v: got %d/%v, native %d", row.BasePrice, row.TradeQuotation, got, err, row.Price)
		}
	}
	for _, row := range corpus.Thieves {
		credit, profit := ThiefGoodsValue(row.BasePrice, row.Quantity)
		if credit != row.Credit || profit != row.Profit {
			t.Fatalf("thief base=%d quantity=%d: got %d/%d, native %d/%d", row.BasePrice, row.Quantity, credit, profit, row.Credit, row.Profit)
		}
	}
}

/*
================
TestTradeQuotationRejectsMalformedAuthority
================
*/
func TestTradeQuotationRejectsMalformedAuthority(t *testing.T) {
	valid := TradeQuotation{Base: 1.2, Lower: 1.1, Upper: 1.3, BaseStock: 50000, Step: 250, Stock: 50000}
	for _, change := range []func(*TradeQuotation){
		func(q *TradeQuotation) { q.Step = 0 },
		func(q *TradeQuotation) { q.Step = -1 },
		func(q *TradeQuotation) { q.BaseStock = 0 },
		func(q *TradeQuotation) { q.Stock = -1 },
		func(q *TradeQuotation) { q.Base = float32(math.NaN()) },
		func(q *TradeQuotation) { q.Upper = float32(math.Inf(1)) },
		func(q *TradeQuotation) { q.Lower = 0 },
		func(q *TradeQuotation) { q.Lower = 2 },
		func(q *TradeQuotation) { q.Base, q.Lower, q.Upper = 1e30, 1e30, 1e30 },
		func(q *TradeQuotation) { q.Base, q.Lower, q.Upper = 0.001, 0.001, 0.001 },
	} {
		q := valid
		change(&q)
		if price, err := TradeQuotationPrice(100, q); err == nil {
			t.Fatalf("malformed authority %+v returned %d", q, price)
		}
	}
	if _, err := TradeQuotationPrice(0, valid); err == nil {
		t.Fatal("accepted a zero base price")
	}
}

/*
================
TestImportedQuotationRowsRemainDistinctAndPriceable
================
*/
func TestImportedQuotationRowsRemainDistinctAndPriceable(t *testing.T) {
	data, err := os.ReadFile(".generated/item-quotations.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Rows []struct {
			TradeQuotation
			ID      int32
			Service int32
			NPC     int32
			Item    int32
		}
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Rows) != 418 {
		t.Fatalf("active quotation rows: got %d, want 418", len(catalog.Rows))
	}
	seen := map[[2]int32]bool{}
	for _, row := range catalog.Rows {
		key := [2]int32{row.NPC, row.Item}
		if row.ID <= 0 || row.Service == 0 || row.NPC <= 0 || row.Item <= 0 || seen[key] {
			t.Fatalf("invalid or duplicate merchant/item row: %+v", row)
		}
		seen[key] = true
		if _, err := TradeQuotationPrice(100, row.TradeQuotation); err != nil {
			t.Fatalf("row %d: %v", row.ID, err)
		}
	}
}
