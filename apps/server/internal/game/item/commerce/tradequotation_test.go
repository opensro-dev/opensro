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
	itemRows := map[int32]int{}
	for _, row := range catalog.Rows {
		key := [2]int32{row.NPC, row.Item}
		if row.ID <= 0 || row.Service == 0 || row.NPC <= 0 || row.Item <= 0 || seen[key] {
			t.Fatalf("invalid or duplicate merchant/item row: %+v", row)
		}
		seen[key] = true
		itemRows[row.Item]++
		if _, err := TradeQuotationPrice(100, row.TradeQuotation); err != nil {
			t.Fatalf("row %d: %v", row.ID, err)
		}
	}
	// Every v1.150 stocked specialty trade good (CH_01..07, WC_01..07,
	// KT_01..04, TK_01..03, RM_01..03, EU_01..04 10388..10391 and
	// CA_01..04 10392..10395) must resolve to quotation rows, and origin
	// merchants must not quote their own town's goods.
	for _, itemID := range []int32{
		2147, 2148, 2149, 2150,
		2151, 2152, 2153, 2154,
		2155, 2156, 2157, 2158,
		7570, 7571, 7572,
		7573, 7574, 7575,
		7576, 7577, 7578,
		7579, 7580, 7581,
		10388, 10389, 10390, 10391,
		10392, 10393, 10394, 10395,
	} {
		if itemRows[itemID] == 0 {
			t.Fatalf("v1.150 specialty trade item %d resolved to zero quotation rows", itemID)
		}
	}
	for _, itemID := range []int32{10388, 10389, 10390, 10391} {
		if !seen[[2]int32{2010, itemID}] || seen[[2]int32{7500, itemID}] {
			t.Fatalf("Constantinople trade item %d misrouted in quotations", itemID)
		}
	}
	for _, itemID := range []int32{10392, 10393, 10394, 10395} {
		if !seen[[2]int32{2010, itemID}] || !seen[[2]int32{7500, itemID}] || seen[[2]int32{7535, itemID}] {
			t.Fatalf("Samarkand trade item %d misrouted in quotations", itemID)
		}
	}
}
