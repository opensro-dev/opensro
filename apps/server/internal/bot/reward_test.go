package bot_test

import (
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/item/wire"
)

// TestParseExpUpdateRoundTrips checks the plain and the levelled 0x30D2 body
// against the server encoder (issue #175).
func TestParseExpUpdateRoundTrips(t *testing.T) {
	plain := wire.EncodeExpUpdate(452883, 137, 9, false, 0)
	if len(plain) != 13 {
		t.Fatalf("plain body = %d bytes, want 13", len(plain))
	}
	update, ok := bot.ParseExpUpdate(plain)
	if !ok {
		t.Fatal("the plain body was rejected")
	}
	if update.SourceGID != 452883 || update.ExpDelta != 137 || update.SkillExpDelta != 9 {
		t.Fatalf("update = %+v", update)
	}
	if update.Levelled {
		t.Fatal("the plain body was read as levelled")
	}

	levelled := wire.EncodeExpUpdate(452883, 5000, 0, true, 3)
	if len(levelled) != 15 {
		t.Fatalf("levelled body = %d bytes, want 15", len(levelled))
	}
	update, ok = bot.ParseExpUpdate(levelled)
	if !ok {
		t.Fatal("the levelled body was rejected")
	}
	if !update.Levelled || update.StatPoints != 3 {
		t.Fatalf("levelled update = %+v", update)
	}
}

// TestParseExpUpdateRejectsShortBodies checks the fixed-size guard (issue #175).
func TestParseExpUpdateRejectsShortBodies(t *testing.T) {
	if _, ok := bot.ParseExpUpdate(make([]byte, 12)); ok {
		t.Fatal("a 12-byte body was accepted")
	}
	if _, ok := bot.ParseExpUpdate(nil); ok {
		t.Fatal("an empty body was accepted")
	}
}

// TestIsMonsterKill checks the kill predicate: a monster entity id with a
// positive delta only (issue #175).
func TestIsMonsterKill(t *testing.T) {
	cases := []struct {
		name string
		up   bot.ExpUpdate
		want bool
	}{
		{"monster kill", bot.ExpUpdate{SourceGID: 452883, ExpDelta: 1}, true},
		{"player kill", bot.ExpUpdate{SourceGID: 100123, ExpDelta: 50}, false},
		{"quest grant", bot.ExpUpdate{SourceGID: 0, ExpDelta: 50}, false},
		{"death penalty", bot.ExpUpdate{SourceGID: 452883, ExpDelta: -50}, false},
	}
	for _, tc := range cases {
		if got := tc.up.IsMonsterKill(); got != tc.want {
			t.Fatalf("%s: IsMonsterKill() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestParsePickupGrantGold checks the gold grant body (issue #175).
func TestParsePickupGrantGold(t *testing.T) {
	grant, ok := bot.ParsePickupGrant(wire.EncodePickupGoldResult(250))
	if !ok {
		t.Fatal("the gold grant was rejected")
	}
	if !grant.Gold || grant.Amount != 250 || grant.Slot != wire.PickupGoldSlot {
		t.Fatalf("grant = %+v", grant)
	}
}

// TestParsePickupGrantItem checks the fixed prefix of an item grant.
func TestParsePickupGrantItem(t *testing.T) {
	grant, ok := bot.ParsePickupGrant([]byte{wire.ResultSuccess, wire.MoveTypePickup, 0x05, 0x00})
	if !ok {
		t.Fatal("the item grant was rejected")
	}
	if grant.Gold || grant.Slot != 0x05 {
		t.Fatalf("grant = %+v", grant)
	}
}

// TestParsePickupGrantRejectsBadBodies checks the refusal, the other move
// types and the short bodies (issue #175).
func TestParsePickupGrantRejectsBadBodies(t *testing.T) {
	bad := [][]byte{
		// A refusal body.
		wire.EncodeItemMoveError(0x39),
		// Other move types.
		{wire.ResultSuccess, wire.MoveTypeInventory, 0x00},
		{wire.ResultSuccess, wire.MoveTypeGroundDrop, 0x00},
		// A short pickup prefix.
		{wire.ResultSuccess, wire.MoveTypePickup},
		// A gold sentinel without the amount.
		{wire.ResultSuccess, wire.MoveTypePickup, wire.PickupGoldSlot, 0x00},
		nil,
	}
	for _, payload := range bad {
		if _, ok := bot.ParsePickupGrant(payload); ok {
			t.Fatalf("a bad body was accepted: %x", payload)
		}
	}
}
