package action

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/wire"
)

func alchemyRuntime() (*Runtime, *enterworld.Character) {
	character := testCharacter()
	rt, _ := newTestRuntime(character, testItems())
	weapon := character.MissionInventory[0]
	rt.Alchemy = &alchemy.Catalog{Items: map[string]alchemy.Reference{
		weapon.Codename: {ID: weapon.RefObjID, Name: weapon.Codename, Flags: weapon.TypeFlags, Class: 1, MaxMagic: 9},
		"elixir":        {ID: 2, Name: "elixir", Flags: wire.PackTypeFlags(3, 3, 10, 1), Params: [5]uint32{6 << 24, 420744970, 168430090, 168101125}},
	}, Magic: map[uint16]alchemy.Magic{}}
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{Slot: 21, RefObjID: 2, Codename: "elixir", TypeFlags: wire.PackTypeFlags(3, 3, 10, 1), StackCount: 1})
	rt.AlchemyRoll = func() (uint32, error) { return 0, nil }
	return rt, character
}

func TestAlchemyConcurrentAttemptsCannotSpendOneElixirTwice(t *testing.T) {
	rt, character := alchemyRuntime()
	var wg sync.WaitGroup
	results := make(chan []wire.Frame, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- rt.HandleAlchemyReinforce(testDivision, character, []byte{2, 20, 21})
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for frames := range results {
		last := frames[len(frames)-1]
		if last.Opcode != alchemy.OpReinforceResult {
			t.Fatal("wrong reply family")
		}
		if last.Payload[0] == 1 {
			wins++
			if len(frames) != 2 || frames[0].Opcode != 0x3645 || !reflect.DeepEqual(frames[0].Payload, []byte{21, 8, 0, 0}) {
				t.Fatalf("material/result ordering %+v", frames)
			}
		}
	}
	if wins != 1 || len(character.MissionInventory) != 1 || character.MissionInventory[0].Plus != 1 {
		t.Fatalf("double spend: wins=%d inventory=%+v", wins, character.MissionInventory)
	}
}

/*
================
TestAlchemyBreakNoticeFollowsTheHandler

#1738 end to end: a reinforcement the 2% table and the drawn 99 make
fail at risk plus destroys the weapon (the immortal/cash protections are absent
from the fixture). With the shard flag on and the character opted in,
the committed break queues one alias notice naming the item the bag held
before the commit; with the flag off the same break queues none. Either
way the outcome is identical: the weapon and the elixir are gone.
================
*/
func TestAlchemyBreakNoticeFollowsTheHandler(t *testing.T) {
	for _, on := range []bool{true, false} {
		rt, character := alchemyRuntime()
		rates, err := alchemy.ParseReinforceRates("2")
		if err != nil {
			t.Fatal(err)
		}
		rt.Alchemy.Rates = rates
		// 99 fails the 2% chance; 0 is under the destroy percent.
		rolls := []uint32{99, 0}
		rt.AlchemyRoll = func() (uint32, error) {
			if len(rolls) == 0 {
				return 0, nil
			}
			n := rolls[0]
			rolls = rolls[1:]
			return n, nil
		}
		character.MissionInventory[0].Plus = alchemy.ReinforceRiskPlus
		character.BreakNotice = true
		character.Job.Alias = "leChiffre"
		rt.Announce.Break = on
		frames := rt.HandleAlchemyReinforce(testDivision, character, []byte{2, 20, 21})
		if len(frames) == 0 || frames[len(frames)-1].Opcode != alchemy.OpReinforceResult || frames[len(frames)-1].Payload[0] != 1 {
			t.Fatalf("on=%v: the break did not commit: %+v", on, frames)
		}
		if len(character.MissionInventory) != 0 {
			t.Fatalf("on=%v: inventory %+v, want the weapon destroyed and the elixir spent", on, character.MissionInventory)
		}
		got := drainedNotices(t, rt)
		if on {
			if len(got) != 1 || !strings.HasPrefix(got[0], "leChiffre broke ") ||
				!strings.Contains(got[0], "ITEM_CH_SWORD_01_A_RARE") || strings.Contains(got[0], character.Name) {
				t.Fatalf("on: %q, want one alias break notice naming the destroyed item, never the character", got)
			}
		} else if len(got) != 0 {
			t.Fatalf("off: %q, want no notice; the break itself already matched", got)
		}
	}
}

func TestAlchemyEntropyFailureCannotMutateOrClaimSuccess(t *testing.T) {
	rt, character := alchemyRuntime()
	before := append([]enterworld.InventoryRow(nil), character.MissionInventory...)
	rt.AlchemyRoll = func() (uint32, error) { return 0, errors.New("entropy failed") }
	frames := rt.HandleAlchemyReinforce(testDivision, character, []byte{2, 20, 21})
	if !reflect.DeepEqual(character.MissionInventory, before) || len(frames) != 1 || frames[0].Payload[0] == 1 {
		t.Fatal("failed transaction changed inventory or published success")
	}
}

func TestAlchemyPreservesUnrelatedPersistedFields(t *testing.T) {
	rt, character := alchemyRuntime()
	legacy := enterworld.InventoryRow{Slot: 30, RefObjID: 999, Codename: "unrelated", VarianceBits: "000123", StackCount: 0}
	character.MissionInventory = append(character.MissionInventory, legacy)
	rt.HandleAlchemyReinforce(testDivision, character, []byte{2, 20, 21})
	if !reflect.DeepEqual(character.MissionInventory[1], legacy) {
		t.Fatalf("unrelated row normalized: %+v", character.MissionInventory[1])
	}
}

func TestAlchemyMalformedAndDuplicateSlotsDoNotDraw(t *testing.T) {
	rt, character := alchemyRuntime()
	rt.AlchemyRoll = func() (uint32, error) { t.Fatal("invalid request drew randomness"); return 0, nil }
	for _, p := range [][]byte{nil, {3, 20, 21}, {2, 20, 20}, {2, 0, 21}, {2, 20, 21, 0}} {
		frames := rt.HandleAlchemyReinforce(testDivision, character, p)
		if len(frames) != 1 || frames[0].Payload[0] == 1 {
			t.Fatalf("accepted %x", p)
		}
	}
}
