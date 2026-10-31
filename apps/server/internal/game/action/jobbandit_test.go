/*
===========================================================================

jobbandit_test.go - tests for the job bandit attack refusal

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
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
	// The pet fixtures wrap the item source to add character references.
	switch items := rt.deps.ItemReferences().(type) {
	case staticItemSource:
		items[suit.Codename] = suit
	case cosTestItemSource:
		items.staticItemSource[suit.Codename] = suit
	default:
		t.Fatalf("unknown item source %T", items)
	}
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

/*
================
TestAreaSelectionSparesOwnSideBandit

The area door (#2080): a suited thief's area selection never takes a
thief bandit, takes the opposing hunter bandit, and still takes an
ordinary monster - the selector's candidates pass the same monster
branch as a direct attack.
================
*/
func TestAreaSelectionSparesOwnSideBandit(t *testing.T) {
	candidates := func(rt *Runtime, c *enterworld.Character, m monster.Instance) []areaCandidate {
		lease, ok := rt.areaPopulation(testDivision, c, combatTarget{gid: m.Gid, monster: &m})
		if !ok {
			t.Fatal("no area population")
		}
		center := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, 1_000_000)
		return rt.areaCandidates(areaQuery{selects: areaSelectObjects, division: testDivision,
			caster: c, lease: lease, center: center, reach: 50, now: 1_000_000})
	}
	rt, _, c, bandit := newCombatTestRuntimeBandit(t, 10000, 2)
	dressJobSuit(t, rt, c, domain.JobThief)
	if got := candidates(rt, c, bandit); len(got) != 0 {
		t.Fatalf("own-side bandit in area selection: %+v", got)
	}
	rt2, _, c2, hunter := newCombatTestRuntimeBandit(t, 10000, 3)
	dressJobSuit(t, rt2, c2, domain.JobThief)
	if got := candidates(rt2, c2, hunter); len(got) != 1 || got[0].target.gid != hunter.Gid {
		t.Fatalf("opposing bandit missing from area selection: %+v", got)
	}
	rt3, _, c3, ordinary := newCombatTestRuntime(t, 10000)
	dressJobSuit(t, rt3, c3, domain.JobThief)
	if got := candidates(rt3, c3, ordinary); len(got) != 1 || got[0].target.gid != ordinary.Gid {
		t.Fatalf("ordinary monster missing from area selection: %+v", got)
	}
}

/*
================
newPetBanditRuntime

The attack-pet fixture over a job bandit (typeID4 2 thief, 3 hunter).
================
*/
func newPetBanditRuntime(t *testing.T, monsterHP uint32, typeID4 uint8) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	rt, clock, c, m := newCombatTestRuntimeBandit(t, monsterHP, typeID4)
	ref := equipCombatTestPet(t, rt, c, attackPetBand)
	ref.Parameters.DefaultSkillIDs[0] = 2
	ref.Parameters.HitRate = 10_000
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 40, 40, 100
	skills[2] = skill
	rt.Now = clock.Now
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	rt.CompanionSurfaceHeight = func(_ uint16, _ float64, y float64, _ float64) (float64, bool) { return y, true }
	rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	rt.BindPetSession(testDivision, c, 1)
	rt.TickHook()(clock.NowMs())
	return rt, clock, c, m
}

/*
================
TestAttackPetRefusesOwnersOwnSideBandit

The pet door (#2080): a thief's attack pet ordered onto a thief bandit
is refused at the order and never strikes it; ordered onto a hunter
bandit it strikes - the owner's job state decides, as for the owner.
================
*/
func TestAttackPetRefusesOwnersOwnSideBandit(t *testing.T) {
	rt, clock, c, bandit := newPetBanditRuntime(t, 10000, 2)
	dressJobSuit(t, rt, c, domain.JobThief)
	result := rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, bandit.Gid))
	if result.DiagnosticRefusal == "" {
		t.Fatalf("order onto own-side bandit admitted: %+v", result)
	}
	for range 50 {
		clock.now = clock.now.Add(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	after, _ := rt.Monsters.Get(testDivision, bandit.Gid)
	if after.CurrentHP != bandit.CurrentHP {
		t.Fatal("the refused pet order damaged the bandit")
	}

	rt2, clock2, c2, hunter := newPetBanditRuntime(t, 10000, 3)
	dressJobSuit(t, rt2, c2, domain.JobThief)
	if result := rt2.HandleCosCommand(testDivision, c2, petAttackOrder(c2.ActiveCOS.GID, hunter.Gid)); result.DiagnosticRefusal != "" {
		t.Fatalf("order onto opposing bandit refused: %+v", result)
	}
	tickPetCombat(t, rt2, clock2, 100, func() bool {
		live, ok := rt2.Monsters.Get(testDivision, hunter.Gid)
		return !ok || live.CurrentHP < hunter.CurrentHP
	})
}
