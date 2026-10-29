/*
===========================================================================

jobbandit_test.go - tests for the job bandit attack refusal

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
dressJobSuit

Register a job suit reference and wear it in slot eight, the dressed
job playerRelationEquipment resolves.
================
*/
func dressJobSuit(t *testing.T, rt *Runtime, c *enterworld.Character, job uint8) {
	t.Helper()
	suits := map[uint8]*enterworld.ItemRef{
		domain.JobTrader: {RefObjID: 9201, Codename: "TEST_TRADER_SUIT", TypeIDs: [4]int64{3, 1, 7, 1}},
		domain.JobThief:  {RefObjID: 9202, Codename: "TEST_THIEF_SUIT", TypeIDs: [4]int64{3, 1, 7, 2}},
		domain.JobHunter: {RefObjID: 9203, Codename: "TEST_HUNTER_SUIT", TypeIDs: [4]int64{3, 1, 7, 3}},
	}
	suit := suits[job]
	rt.deps.ItemReferences().(staticItemSource)[suit.Codename] = suit
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(enterworld.JobSuitSlot),
		RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), StackCount: 1})
}

/*
================
TestJobBanditAttackRefusal

The native attack validation's monster branch at both doors' shared
refusal: a thief bandit refuses a thief and an unsuited attacker, a
hunter bandit everyone but a thief, an ordinary monster nobody; the
refusal is 0x3006.
================
*/
func TestJobBanditAttackRefusal(t *testing.T) {
	bandit := func(typeID4 uint8) monster.Instance {
		var i monster.Instance
		i.Ref.TidWord, i.Ref.TypeID4 = 0x00c6, typeID4
		return i
	}
	thiefBandit, hunterBandit, ordinary := bandit(2), bandit(3), bandit(1)
	cases := []struct {
		name   string
		job    uint8 // 0 wears no suit
		target monster.Instance
		want   uint16
	}{
		{"trader strikes thief bandit", domain.JobTrader, thiefBandit, 0},
		{"trader strikes hunter bandit", domain.JobTrader, hunterBandit, 0x3006},
		{"thief strikes thief bandit", domain.JobThief, thiefBandit, 0x3006},
		{"thief strikes hunter bandit", domain.JobThief, hunterBandit, 0},
		{"hunter strikes thief bandit", domain.JobHunter, thiefBandit, 0},
		{"hunter strikes hunter bandit", domain.JobHunter, hunterBandit, 0x3006},
		{"unsuited strikes thief bandit", 0, thiefBandit, 0x3006},
		{"unsuited strikes hunter bandit", 0, hunterBandit, 0x3006},
		{"thief strikes ordinary monster", domain.JobThief, ordinary, 0},
		{"unsuited strikes ordinary monster", 0, ordinary, 0},
	}
	for _, tc := range cases {
		rt, _, c, _ := newCombatTestRuntime(t, 100)
		if tc.job != 0 {
			dressJobSuit(t, rt, c, tc.job)
		}
		if got := rt.jobMonsterTargetRefusal(c, tc.target); got != tc.want {
			t.Errorf("%s: refusal %#x, want %#x", tc.name, got, tc.want)
		}
	}
}

/*
================
TestBasicAttackOnOrdinaryMonsterIgnoresJobSuits

The bandit refusal must not leak onto ordinary monsters: a suited
thief's basic attack still lands.
================
*/
func TestBasicAttackOnOrdinaryMonsterIgnoresJobSuits(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 10000)
	dressJobSuit(t, rt, c, domain.JobThief)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP >= target.CurrentHP {
		t.Fatal("the suited attack on an ordinary monster never landed")
	}
}

/*
================
TestBasicAttackRefusesOwnSideBanditAtTheCommand

The basic-attack door: a suited thief engaging a thief bandit from
out of reach is refused 0x3006 at the command itself - the refusal
answers at once, no approach intent is left walking, and the bandit
is untouched. (In reach, the skill-stage refusal would answer the
swing instead, which is the behaviour this door exists to pre-empt.)
The same thief engaging a hunter bandit in reach is admitted.
================
*/
func TestBasicAttackRefusesOwnSideBanditAtTheCommand(t *testing.T) {
	rt, _, c, bandit := newCombatTestRuntimeBandit(t, 10000, 2)
	if !bandit.ThiefMonster() {
		t.Fatal("the fixture target is not a thief bandit")
	}
	dressJobSuit(t, rt, c, domain.JobThief)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	*c.World.Spawn.X = 900 // the bandit stands at 963: far out of reach
	out := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: bandit.Gid}.Encode())
	refusal, ok := findFrame(out.Frames, wire.OpSkillCastResult)
	if !ok || len(refusal.Payload) != 2 || refusal.Payload[0] != 2 || refusal.Payload[1] != 0x06 {
		t.Fatalf("own-side engage answer: %+v", out.Frames)
	}
	if _, intent := rt.combatIntentFor(testDivision, c.Name); intent {
		t.Fatal("the refused engage left an approach intent behind")
	}
	after, _ := rt.Monsters.Get(testDivision, bandit.Gid)
	if after.CurrentHP != bandit.CurrentHP {
		t.Fatal("the refused engage damaged the bandit")
	}

	rt2, _, c2, hunter := newCombatTestRuntimeBandit(t, 10000, 3)
	dressJobSuit(t, rt2, c2, domain.JobThief)
	rt2.CombatRoll = func() (uint32, error) { return 0, nil }
	rt2.HandleTargetInteract(testDivision, c2, wire.BasicAttackEngage{TargetGid: hunter.Gid}.Encode())
	after2, _ := rt2.Monsters.Get(testDivision, hunter.Gid)
	if after2.CurrentHP >= hunter.CurrentHP {
		t.Fatal("the cross-side engage never landed")
	}
}

/*
================
TestTargetedSkillRefusesOwnSideBandit

The targeted-skill door: a suited thief's targeted skill at a thief
bandit is refused 0x3006 and lands nothing; at a hunter bandit it
casts and lands.
================
*/
func TestTargetedSkillRefusesOwnSideBandit(t *testing.T) {
	rt, _, c, bandit := newCombatTestRuntimeBandit(t, 10000, 2)
	dressJobSuit(t, rt, c, domain.JobThief)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: bandit.Gid}.Encode())
	refusal, ok := findFrame(out.Frames, wire.OpSkillCastResult)
	if !ok || len(refusal.Payload) != 2 || refusal.Payload[0] != 2 || refusal.Payload[1] != 0x06 {
		t.Fatalf("own-side skill answer: %+v", out.Frames)
	}
	after, _ := rt.Monsters.Get(testDivision, bandit.Gid)
	if after.CurrentHP != bandit.CurrentHP {
		t.Fatal("the refused skill damaged the bandit")
	}

	rt2, _, c2, hunter := newCombatTestRuntimeBandit(t, 10000, 3)
	dressJobSuit(t, rt2, c2, domain.JobThief)
	rt2.CombatRoll = func() (uint32, error) { return 0, nil }
	rt2.HandleTargetInteract(testDivision, c2, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: hunter.Gid}.Encode())
	after2, _ := rt2.Monsters.Get(testDivision, hunter.Gid)
	if after2.CurrentHP >= hunter.CurrentHP {
		t.Fatal("the cross-side skill never landed")
	}
}
