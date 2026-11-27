/*
===========================================================================

killshare_test.go - party quest kill share tests (#4490 U17-04)

The DivideType fan-out: one member at a time in turn (0), every near
holder (1/4/5), the killer only (2/3), and the decline to the solo path
when the party does not share both experience and items.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// killShareBook records every credit the share lane and the solo lane
// hand out.
type killShareBook struct {
	credits   map[string]map[uint32]int
	drops     map[string]int
	soloKill  int
	soloDrops int
	chains    int
	pushed    map[string]int
}

/*
================
seedKillShareParty

Killer at the origin, five members: near (5/10/15), far in the same
world (5000), and one in another world instance. The killer's quests:
101 in turn with Near1, 106 killer-only with Near3. Near2 and the two
far members hold 102 (every holder), Near2 and Near3 hold the gather
quest 104, Near3 alone holds the killer-only 105 nobody else can take.
================
*/
func seedKillShareParty(t *testing.T, rt *Runtime, killer *enterworld.Character, options uint8) *killShareBook {
	t.Helper()
	now := int64(40000)
	origin := rt.liveSpawn(simulation.WorldKey(testDivision, killer.Name), killer, now)
	seed := func(m *enterworld.Character, x float64) {
		rt.Worlds.Update(simulation.WorldKey(testDivision, m.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(m) },
			func(w *simulation.WorldState) {
				w.Spawn = simulation.Spawn{RegionID: origin.RegionID, X: x, Y: origin.Y, Z: origin.Z}
				w.SpawnSet = true
			})
	}
	near1 := nearbyCharacter(rt, killer, 11, "Near1", 5)
	near2 := nearbyCharacter(rt, killer, 12, "Near2", 10)
	near3 := nearbyCharacter(rt, killer, 13, "Near3", 15)
	far := nearbyCharacter(rt, killer, 14, "Far", 5000)
	other := nearbyCharacter(rt, killer, 15, "OtherWorld", 10)
	otherInstance := uint32(424242)
	other.World.PackedInstance = &otherInstance
	for i, m := range []*enterworld.Character{near1, near2, near3, far, other} {
		m.CurrentHP = testInt64(100)
		m.World.Spawn.X = testFloat64(origin.X + []float64{5, 10, 15, 5000, 10}[i])
	}
	seed(near1, origin.X+5)
	seed(near2, origin.X+10)
	seed(near3, origin.X+15)
	seed(far, origin.X+5000)
	seed(other, origin.X+10)

	gids := make([]uint32, 0, 6)
	for _, m := range []*enterworld.Character{killer, near1, near2, near3, far, other} {
		gids = append(gids, enterworld.ObjectIDForCharacter(m))
	}
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Order: 77, Options: options, LeaderID: gids[0], Members: gids}}
	}
	held := map[string][]QuestKillTaker{
		killer.Name: {{RefID: 101}, {RefID: 106, DivideType: 2}},
		near1.Name:  {{RefID: 101}},
		near2.Name:  {{RefID: 102, DivideType: 5}, {RefID: 104, DivideType: 1, Drop: true}},
		near3.Name:  {{RefID: 104, DivideType: 1, Drop: true}, {RefID: 105, DivideType: 2}, {RefID: 106, DivideType: 2}},
		far.Name:    {{RefID: 102, DivideType: 5}},
		other.Name:  {{RefID: 102, DivideType: 5}, {RefID: 104, DivideType: 1, Drop: true}},
	}
	book := &killShareBook{credits: map[string]map[uint32]int{}, drops: map[string]int{}, pushed: map[string]int{}}
	rt.QuestKillTakers = func(c *enterworld.Character, _ string, _ uint8) []QuestKillTaker { return held[c.Name] }
	rt.UpdateQuestKillFor = func(c *enterworld.Character, _ string, _ uint8, only map[uint32]bool) []wire.Frame {
		if book.credits[c.Name] == nil {
			book.credits[c.Name] = map[uint32]int{}
		}
		for refID := range only {
			book.credits[c.Name][refID]++
		}
		return []wire.Frame{{Opcode: 0xBEEF}}
	}
	rt.NoteQuestKillChains = func(*enterworld.Character, string) []wire.Frame {
		book.chains++
		return []wire.Frame{{Opcode: 0xFEED}}
	}
	rt.UpdateQuestKill = func(c *enterworld.Character, _ string, _ uint8) ([]wire.Frame, bool) {
		book.soloKill++
		return nil, true
	}
	ref := &enterworld.ItemRef{RefObjID: 2201, Codename: "ITEM_QNO_CH_CHEF_1", TypeIDs: [4]int64{3, 3, 8, 0}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}
	rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = ref
	rt.DropRoll = func() (uint32, error) { return 32767, nil }
	rt.QuestMonsterDropsFor = func(c *enterworld.Character, _ string, _ uint8, _ func() (uint32, error), _ map[uint32]bool) []inventory.ItemAmount {
		book.drops[c.Name]++
		return []inventory.ItemAmount{{Codename: ref.Codename, Count: 1}}
	}
	rt.QuestMonsterDrops = func(c *enterworld.Character, _ string, _ uint8, _ func() (uint32, error)) []inventory.ItemAmount {
		book.soloDrops++
		return nil
	}
	rt.PushCharacterFrames = func(_ string, name string, _ []wire.Frame) { book.pushed[name]++ }
	return book
}

/*
================
wantCredits

The named member's count of one quest.
================
*/
func (book *killShareBook) wantCredits(t *testing.T, name string, refID uint32, want int) {
	t.Helper()
	if got := book.credits[name][refID]; got != want {
		t.Fatalf("%s quest %d credited %d times, want %d (all: %+v)", name, refID, got, want, book.credits)
	}
}

/*
================
TestPartyKillSharesByTheQuestsRule

One real kill in a party sharing experience and items: the turn quest
goes to the killer (first in turn), the every-holder quest to the only
near holder, the gather quest to both near holders' drop rolls, and
the killer-only quests to the killer alone. Far and other-world
holders take nothing; the killer's chains ran exactly once.
================
*/
func TestPartyKillSharesByTheQuestsRule(t *testing.T) {
	rt, _, killer, target := newCombatTestRuntime(t, 1)
	book := seedKillShareParty(t, rt, killer, 0x3)
	out := rt.HandleTargetInteract(testDivision, killer, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	assertAndSeparateActionSession(t, out)
	book.wantCredits(t, killer.Name, 101, 1)
	book.wantCredits(t, "Near1", 101, 0)
	book.wantCredits(t, "Near2", 102, 1)
	book.wantCredits(t, "Far", 102, 0)
	book.wantCredits(t, "OtherWorld", 102, 0)
	book.wantCredits(t, killer.Name, 106, 1)
	book.wantCredits(t, "Near3", 106, 0)
	for name := range book.credits {
		for refID := range book.credits[name] {
			if refID == 105 {
				t.Fatalf("quest 105 credited to %s without the killer holding it", name)
			}
		}
	}
	if book.drops["Near2"] != 1 || book.drops["Near3"] != 1 || book.drops["OtherWorld"] != 0 {
		t.Fatalf("gather drop rolls: %+v", book.drops)
	}
	if book.soloKill != 0 || book.soloDrops != 0 {
		t.Fatalf("the solo lane ran in a sharing party: %d/%d", book.soloKill, book.soloDrops)
	}
	if book.chains != 1 {
		t.Fatalf("the killer's chains ran %d times", book.chains)
	}
	if book.pushed["Near2"] == 0 || book.pushed["Far"] != 0 {
		t.Fatalf("progress went to the wrong sessions: %+v", book.pushed)
	}
}

/*
================
TestPartyKillTurnRotates

Three kills of the same monster: the turn quest credits killer,
Near1, killer, while the every-holder and gather quests credit their
near holders every time.
================
*/
func TestPartyKillTurnRotates(t *testing.T) {
	rt, _, killer, target := newCombatTestRuntime(t, 100)
	book := seedKillShareParty(t, rt, killer, 0x3)
	now := int64(40000)
	origin := rt.liveSpawn(simulation.WorldKey(testDivision, killer.Name), killer, now)
	pose := monster.Pose{RegionID: origin.RegionID, X: origin.X, Y: origin.Y, Z: origin.Z}
	for kill := 0; kill < 3; kill++ {
		frames, _, shared := rt.shareQuestKill(testDivision, killer, target, pose, now)
		if !shared || len(frames) == 0 || frames[0].Opcode != 0xFEED {
			t.Fatalf("kill %d: shared=%v frames=%+v", kill, shared, frames)
		}
	}
	book.wantCredits(t, killer.Name, 101, 2)
	book.wantCredits(t, "Near1", 101, 1)
	book.wantCredits(t, "Near2", 102, 3)
	book.wantCredits(t, "Far", 102, 0)
	book.wantCredits(t, killer.Name, 106, 3)
	book.wantCredits(t, "Near3", 106, 0)
	if book.drops["Near2"] != 3 || book.drops["Near3"] != 3 {
		t.Fatalf("gather drop rolls: %+v", book.drops)
	}
	if book.chains != 3 {
		t.Fatalf("the killer's chains ran %d times", book.chains)
	}
}

/*
================
TestPartyKillWithoutBothShareBitsStaysSolo

A party sharing experience but not items: the share declines and the
kill counts for the killer alone, through the solo lane.
================
*/
func TestPartyKillWithoutBothShareBitsStaysSolo(t *testing.T) {
	rt, _, killer, target := newCombatTestRuntime(t, 1)
	book := seedKillShareParty(t, rt, killer, 0x1)
	out := rt.HandleTargetInteract(testDivision, killer, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	assertAndSeparateActionSession(t, out)
	if book.soloKill != 1 || len(book.credits) != 0 || book.soloDrops != 1 {
		t.Fatalf("solo path: kills=%d credits=%+v drops=%d", book.soloKill, book.credits, book.soloDrops)
	}
}
