package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
==================
TestBlockRateWritesFollowTheLaneMask

The buff installer: br 7 (physical, both attack kinds) raises 0x88 and 0x89 only; br 11
(magical, both) raises 0x8A and 0x8B only.
==================
*/
func TestBlockRateWritesFollowTheLaneMask(t *testing.T) {
	params := func(writes []paramkeeper.Write) []uint16 {
		var out []uint16
		for _, w := range writes {
			if w.Channel != paramkeeper.Flat || w.Value != 10 {
				t.Fatalf("write %+v", w)
			}
			out = append(out, w.Parameter)
		}
		return out
	}
	if got := params(BlockRateWrites(7, 10)); len(got) != 2 || got[0] != 0x88 || got[1] != 0x89 {
		t.Fatalf("br 7 writes %x", got)
	}
	if got := params(BlockRateWrites(11, 10)); len(got) != 2 || got[0] != 0x8a || got[1] != 0x8b {
		t.Fatalf("br 11 writes %x", got)
	}
}

/*
==================
TestBlockChanceAddsTheLaneBonusAndIgnoresNothingElse

The server's block test: base block rate plus the attack's lane bonus, divided by
(1 + attacker param 0x38 / 100) and truncated. A physical basic hit reads
0x88 and not 0x89; a magical hit reads neither physical bonus; ck removes
the chance.
==================
*/
func TestBlockChanceAddsTheLaneBonusAndIgnoresNothingElse(t *testing.T) {
	c := &domain.Character{Level: pointer(1), Strength: pointer(20), Intellect: pointer(20)}
	writes := BlockRateWrites(7, 10)
	for i := range writes {
		writes[i].Source = uint32(10 + i) // an installed effect binds its own source
	}
	writes = append(writes, paramkeeper.Write{Parameter: 0x89, Channel: paramkeeper.Flat, Source: 1, Value: 5})
	defender, _, err := PlayerStatsWithModifiers(c, Catalogs{Items: itemRefs{}}, writes, nil)
	if err != nil {
		t.Fatal(err)
	}
	defender.BlockRate = 20
	attacker, _, err := PlayerStatsWithModifiers(c, Catalogs{Items: itemRefs{}}, []paramkeeper.Write{{Parameter: 0x38, Channel: paramkeeper.Flat, Source: 2, Value: 50}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, _ := PlayerStats(c, Catalogs{Items: itemRefs{}})

	for _, tc := range []struct {
		name     string
		attacker Stats
		flags    uint32
		ck       bool
		want     uint8
	}{
		{"physical basic", plain, 5, false, 30},           // 20 + 0x88 10
		{"physical skill", plain, 6, false, 35},           // 20 + 0x89 (10 + 5)
		{"magical basic", plain, 9, false, 20},            // no magical bonus written
		{"attacker ignores half", attacker, 5, false, 20}, // 30 / 1.5
		{"ck", plain, 5, true, 0},
	} {
		if got := BlockChance(tc.attacker, defender, tc.flags, tc.ck); got != tc.want {
			t.Errorf("%s: chance %d, want %d", tc.name, got, tc.want)
		}
	}
}
