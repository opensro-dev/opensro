/*
===========================================================================

cape_transfer_test.go - combat holds a worn cape through real inventory moves

Both legs of an inventory swap can remove the slot-eight occupant. Exercise
the packet handler so a job-suit-only helper cannot hide a missing cape gate.

===========================================================================
*/
package action

import (
	"fmt"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestWornCapeCannotComeOffInBattle
================
*/
func TestWornCapeCannotComeOffInBattle(t *testing.T) {
	for _, battle := range []bool{false, true} {
		for _, swap := range []bool{false, true} {
			t.Run(fmt.Sprintf("battle=%t/swap=%t", battle, swap), func(t *testing.T) {
				rt, clock, c := jobFixture(t)
				items := rt.deps.ItemReferences().(staticItemSource)
				row, _ := inventoryRowAt(c, jobTestSuitSlot)
				cape := items[row.Codename]
				cape.TypeIDs[3] = 5
				cape.NativeFields = enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: 1})
				for i := range c.MissionInventory {
					if c.MissionInventory[i].Slot == int64(jobTestSuitSlot) {
						c.MissionInventory[i].Slot = int64(jobSuitSlot)
						c.MissionInventory[i].TypeFlags = cape.TypeFlags()
					}
				}
				source, dest := jobSuitSlot, jobTestSuitSlot
				if swap {
					row.TypeFlags = cape.TypeFlags()
					c.MissionInventory = append(c.MissionInventory, row)
					source, dest = dest, source
				}
				if battle {
					c.BattleUntilMs = clock.NowMs() + 10000
				}
				before := c.Snapshot().MissionInventory
				code, refused := moveRefusalCode(t, rt, c, source, dest)
				if battle {
					if !refused || code != capeStripErrBattle || !reflect.DeepEqual(before, c.MissionInventory) {
						t.Fatalf("cape move refused=%t code=%#x inventory changed=%t", refused, code, !reflect.DeepEqual(before, c.MissionInventory))
					}
				} else if refused {
					t.Fatalf("out-of-battle cape move refused: %#x", code)
				}
			})
		}
	}
}

/*
================
TestJobSuitBattleRemovalKeepsItsOwnRefusal
================
*/
func TestJobSuitBattleRemovalKeepsItsOwnRefusal(t *testing.T) {
	rt, clock, c := jobFixture(t)
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Slot == int64(jobTestSuitSlot) {
			c.MissionInventory[i].Slot = int64(jobSuitSlot)
		}
	}
	c.BattleUntilMs = clock.NowMs() + 10000
	if code, refused := moveRefusalCode(t, rt, c, jobSuitSlot, jobTestSuitSlot); !refused || code != jobWearErrBattle {
		t.Fatalf("suit refusal=%t code=%#x", refused, code)
	}
}
