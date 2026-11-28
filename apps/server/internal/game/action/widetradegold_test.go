/*
===========================================================================

widetradegold_test.go - the trade table's gold past the u32 range (#4264)

SRO_WIDE_TRADE_GOLD is port-only: off, the exchange window holds the
native u32 of gold and a wide amount is refused; on, the wide gold move
carries the amount, the echo and the partner's 0x30BB grow to a u64, and
the swap moves the whole amount.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// wideTradeAmount is past the native u32 table (4,294,967,295).
const wideTradeAmount uint64 = 5_000_000_000

/*
================
openWideTrade

An open exchange between the fixture's two players, b holding ten billion
gold, with the shard's wide trade rule set as given.
================
*/
func openWideTrade(t *testing.T, wide bool) (*Runtime, *enterworld.Character, *enterworld.Character, map[string][]wire.Frame) {
	t.Helper()
	rt, a, b, pushed := exchangeFixture(t)
	rt.WideTradeGold = wide
	setGold(b, 10_000_000_000)
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(b)).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	if !rt.Exchanges.Trading(testDivision, b.Name) {
		t.Fatal("the exchange did not open")
	}
	return rt, a, b, pushed
}

/*
================
TestWideTradeGoldOffRefusesTheWideForm

Native: a table amount above the u32 range is refused, and nothing reaches
the partner.
================
*/
func TestWideTradeGoldOffRefusesTheWideForm(t *testing.T) {
	rt, a, b, pushed := openWideTrade(t, false)
	before := len(pushed[a.Name])
	got := rt.HandleItemMove(testDivision, b, wire.NewWriter(9).U8(wire.MoveTypeExchangeGold).U64(wideTradeAmount).Payload())
	if len(got.Frames) != 1 || got.Frames[0].Payload[0] != wire.ResultError {
		t.Fatalf("a wide amount with the rule off answered %+v", got.Frames)
	}
	if len(pushed[a.Name]) != before {
		t.Fatalf("the partner heard %d frames for a refused amount", len(pushed[a.Name])-before)
	}
	// The native bound itself still fits the table.
	narrow := rt.HandleItemMove(testDivision, b, wire.NewWriter(5).U8(wire.MoveTypeExchangeGold).U32(0xffffffff).Payload())
	if len(narrow.Frames) != 1 || !bytes.Equal(narrow.Frames[0].Payload, []byte{1, wire.MoveTypeExchangeGold, 0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("the u32 bound answered %+v", narrow.Frames)
	}
}

/*
================
TestWideTradeGoldOnMovesTheWholeAmount

On: the five-billion table answers in the wide form, the partner sees the
wide 0x30BB, and the swap credits the whole amount.
================
*/
func TestWideTradeGoldOnMovesTheWholeAmount(t *testing.T) {
	rt, a, b, pushed := openWideTrade(t, true)
	got := rt.HandleItemMove(testDivision, b, wire.NewWriter(9).U8(wire.MoveTypeExchangeGold).U64(wideTradeAmount).Payload())
	wantEcho := wire.NewWriter(10).U8(1).U8(wire.MoveTypeExchangeGold).U64(wideTradeAmount).Payload()
	if len(got.Frames) != 1 || !bytes.Equal(got.Frames[0].Payload, wantEcho) {
		t.Fatalf("the wide amount answered %+v", got.Frames)
	}
	wantPartner := wire.NewWriter(9).U8(2).U64(wideTradeAmount).Payload()
	if partner := lastFrame(t, pushed[a.Name], wire.OpExchangePartnerGold); !bytes.Equal(partner.Payload, wantPartner) {
		t.Fatalf("the partner heard % X", partner.Payload)
	}
	startA := goldOf(a)
	rt.HandleExchangeConfirm(testDivision, a, nil)
	rt.HandleExchangeConfirm(testDivision, b, nil)
	rt.HandleExchangeApprove(testDivision, a, nil)
	rt.HandleExchangeApprove(testDivision, b, nil)
	lastFrame(t, pushed[a.Name], wire.OpExchangeSucceeded)
	if goldOf(a) != startA+wideTradeAmount || goldOf(b) != 10_000_000_000-wideTradeAmount {
		t.Fatalf("gold after the swap: %d / %d", goldOf(a), goldOf(b))
	}
}

/*
================
TestWideTradeGoldOnKeepsNarrowAmountsNative

On: an amount the u32 holds still travels and echoes in the native form,
so a native client's partner frame is unchanged.
================
*/
func TestWideTradeGoldOnKeepsNarrowAmountsNative(t *testing.T) {
	rt, a, b, pushed := openWideTrade(t, true)
	got := rt.HandleItemMove(testDivision, b, wire.NewWriter(5).U8(wire.MoveTypeExchangeGold).U32(1000).Payload())
	if len(got.Frames) != 1 || !bytes.Equal(got.Frames[0].Payload, []byte{1, wire.MoveTypeExchangeGold, 0xe8, 3, 0, 0}) {
		t.Fatalf("a narrow amount answered %+v", got.Frames)
	}
	if partner := lastFrame(t, pushed[a.Name], wire.OpExchangePartnerGold); !bytes.Equal(partner.Payload, []byte{2, 0xe8, 3, 0, 0}) {
		t.Fatalf("the partner heard % X", partner.Payload)
	}
}
