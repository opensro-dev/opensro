package bot_test

import (
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/item/wire"
)

// TestParseBaseStatsMaxHP reads the maximum HP from the block the server
// encoder builds, so the offset follows the wire layout.
func TestParseBaseStatsMaxHP(t *testing.T) {
	block := wire.BaseStats{
		PhysicalAttackMin: 11, PhysicalAttackMax: 12, MagicalAttackMin: 13, MagicalAttackMax: 14,
		PhysicalDefense: 15, MagicalDefense: 16, HitRate: 17, ParryRate: 18,
		MaxHP: 4321, MaxMP: 987, StrWord: 40, IntWord: 20,
	}.Encode()
	if maxHP, ok := bot.ParseBaseStatsMaxHP(block); !ok || maxHP != 4321 {
		t.Fatalf("max HP = %d %v, want 4321", maxHP, ok)
	}
	if _, ok := bot.ParseBaseStatsMaxHP(block[:len(block)-1]); ok {
		t.Fatal("a short block was accepted")
	}
	if _, ok := bot.ParseBaseStatsMaxHP(wire.BaseStats{MaxMP: 5}.Encode()); ok {
		t.Fatal("a zero maximum was accepted")
	}
}
