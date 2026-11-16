/*
===========================================================================

sookarena_degree9_test.go - arena gear under the degree-9 drop stop (#3669)

With SRO_ABOVE_DEGREE9_DROPS_OFF on, a level-90 arena pick that lands on
an item above degree 9 is drawn again a band lower instead of paying an
empty slot; off, the pick stands.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
raisedDegreeItems

The shipped items, with every degree-9 piece of equipment reading as
degree 10: the shape a cap-110 item shard gives the level-90 draw.
================
*/
type raisedDegreeItems struct {
	enterworld.ItemRefSource
}

func (r raisedDegreeItems) raise(ref *enterworld.ItemRef, ok bool) (*enterworld.ItemRef, bool) {
	if !ok || ref == nil || !enterworld.IsEquipment(ref) || enterworld.ItemDegree(ref) != 9 {
		return ref, ok
	}
	raised := *ref
	raised.NativeFields = ref.NativeFields.With("itemClass", degree10EquipmentClass)
	return &raised, true
}

func (r raisedDegreeItems) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	return r.raise(r.ItemRefSource.ItemRefByCodename(codename))
}

func (r raisedDegreeItems) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	return r.raise(r.ItemRefSource.ItemRefByID(id))
}

func TestSookGearRedrawsBelowTheDegree9DropStop(t *testing.T) {
	clientdata.RequireGameData(t)
	for _, env := range []string{"off", "on"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(progression.EnvAboveDegree9DropsOff, env)
			c := instanceWorldTestCharacter(11, "DegreeNine")
			rt, _ := sookFixture(t, c)
			rt.enterSookArena(testDivision, c)
			level := int64(90)
			c.Level = &level
			items := raisedDegreeItems{enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))}
			rt.deps.(*enterworld.Deps).Items = items
			rt.AboveDegree9Drops = progression.AboveDegree9DropsFromEnv()
			n := uint32(17)
			rt.DropRoll = func() (uint32, error) { n = n*1664525 + 1013904223; return n >> 17, nil }
			above, empty := 0, 0
			const draws = 200
			for draw := range draws {
				// Index 0 of each twelve is always a gear slot.
				item, ok := rt.arenaReward(rt.sookRun(testDivision, c), c, 1+draw%5, 12*draw)
				if !ok {
					empty++
					continue
				}
				if ref, found := items.ItemRefByCodename(item.Codename); found && enterworld.ItemDegree(ref) > 9 {
					above++
				}
			}
			switch env {
			case "off":
				if above == 0 {
					t.Fatal("no level-90 draw reached degree 10; the fixture proves nothing")
				}
			case "on":
				if above != 0 || empty != 0 {
					t.Fatalf("drop stop on: %d rewards above degree 9, %d empty slots of %d", above, empty, draws)
				}
			}
		})
	}
}
