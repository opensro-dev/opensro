/*
===========================================================================

linkedmana_test.go - the Bard's Mana Switch (BATTLAA_MPSTEAL_A)

Mana Switch is the lnks pair with lkdh: cast on a party member it links
the Bard (source) to the member (recipient), and while the link holds the
member's hits on a monster hand the Bard 50 % of each hit's damage as MP,
at most the tier's ceiling per hit (Bard specification, rule 11).

===========================================================================
*/

package action

import (
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// manaSwitchCode is the last tier: lnks(15,700,0,0) lks2 dura(20000)
	// lkdh(0,50,1596) getv(BDMD) reqi(6,14).
	manaSwitchCode     = "SKILL_EU_BARD_BATTLAA_MPSTEAL_A_13"
	manaSwitchPercent  = 50
	manaSwitchCeiling  = 1596
	manaSwitchDuration = 20000 * time.Millisecond
	manaSwitchRange    = 700
	// manaSwitchHarpCode is the fixture's harp: the sword reference with
	// the harp's TypeID3.
	manaSwitchHarpCode = "ITEM_EU_HARP_TEST"
	// memberBaseAttack is the fixture's sword base attack (row 2).
	memberBaseAttack = 2
	// bigHit and smallHit straddle the ceiling: 50 % of bigHit exceeds it.
	bigHit   = 10000
	smallHit = 1001
)

/*
================
manaSwitchParty

The combat fixture as a party of a European Bard with Mana Switch learned
(its harp reqi kept) and a sword-wielding member one unit away, the Bard
at full MP and pushed frames recorded per character.
================
*/
type manaSwitchParty struct {
	rt            *Runtime
	clock         *fakeClock
	bard, member  *enterworld.Character
	mob           monster.Instance
	skill         enterworld.SkillRow
	pushed        map[string][]wire.Frame
	bardGID, mate uint32
}

/*
================
newManaSwitchParty

harp false leaves the Bard on the fixture's sword.
================
*/
func newManaSwitchParty(t *testing.T, harp bool) manaSwitchParty {
	t.Helper()
	rt, clock, c, mob := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0
	member := nearbyCharacter(rt, c, 21, "member", 1)
	member.Skills = []uint32{memberBaseAttack}
	member.MissionInventory = slices.Clone(c.MissionInventory)
	member.CurrentMP = testInt64(0)

	row, ok := shippedSkills(t).SkillByCodename(manaSwitchCode)
	if !ok || !row.TimedEffect.Pinned || !row.TimedEffect.Link.Mana {
		t.Fatalf("%s not admitted as a damage link: %+v", manaSwitchCode, row.TimedEffect)
	}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{row.ID}
	c.Intellect = testInt64(tuningIntellect)
	if harp {
		items := rt.deps.ItemReferences().(staticItemSource)
		ref := *items[c.MissionInventory[0].Codename]
		ref.Codename = manaSwitchHarpCode
		ref.TypeIDs[3] = bardHarpKind
		items[ref.Codename] = &ref
		weapon := c.MissionInventory[0]
		weapon.Codename, weapon.TypeFlags = ref.Codename, ref.TypeFlags()
		c.MissionInventory = []enterworld.InventoryRow{weapon}
	}
	_, maxMP, _, _ := rt.playerKeeperVitals(testDivision, c)
	c.CurrentMP = testInt64(maxMP)

	p := manaSwitchParty{rt: rt, clock: clock, bard: c, member: member, mob: mob, skill: row,
		pushed: make(map[string][]wire.Frame), bardGID: enterworld.ObjectIDForCharacter(c), mate: enterworld.ObjectIDForCharacter(member)}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: []uint32{p.bardGID, p.mate}}} }
	rt.PushCharacterFrames = func(_ string, name string, frames []wire.Frame) {
		p.pushed[name] = append(p.pushed[name], frames...)
	}
	return p
}

/*
================
cast

Press Mana Switch on target.
================
*/
func (p manaSwitchParty) cast(target uint32) OpResult {
	return p.rt.HandleTargetInteract(testDivision, p.bard, wire.SkillAction{ActionId: p.skill.ID, HasTarget: true, TargetGid: target}.Encode())
}

/*
================
link

Cast on the member, require both halves, then empty the Bard's MP so
every later gain is measured from zero.
================
*/
func (p manaSwitchParty) link(t *testing.T) {
	t.Helper()
	r := p.cast(p.mate)
	if r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("Mana Switch refused: %+v", r)
	}
	if got := linkedHalves(p.rt, p.bard.Name, p.skill.ID); len(got) != 1 || got[0] != 1 {
		t.Fatalf("bard halves %v, want the source", got)
	}
	if got := linkedHalves(p.rt, p.member.Name, p.skill.ID); len(got) != 1 || got[0] != 2 {
		t.Fatalf("member halves %v, want the recipient", got)
	}
	p.bard.CurrentMP = testInt64(0)
}

/*
================
bardMP
================
*/
func (p manaSwitchParty) bardMP() int64 {
	_, _, _, mp := p.rt.playerKeeperVitals(testDivision, p.bard)
	return mp
}

/*
================
memberHits

The member's committed damage on the monster, at the current clock,
through the damage commit path every player hit takes.
================
*/
func (p manaSwitchParty) memberHits(applied ...uint32) {
	var impacts []simulation.MonsterDamageResult
	for _, a := range applied {
		impacts = append(impacts, simulation.MonsterDamageResult{Applied: a})
	}
	p.rt.commitSkillHostility(testDivision, p.mate, p.mob.Gid, enterworld.SkillRow{}, impacts, p.rt.Now().UnixMilli())
}

/*
==================
TestManaSwitchTurnsTheMembersDamageIntoTheBardsMP

A cast on a party member installs the Bard's source half and the
member's recipient half. The member's real basic attack then hands the
Bard half of the HP it took, pushed in a 0x33A6; a hit whose half
exceeds the ceiling gives exactly the ceiling, each impact capped alone.
==================
*/
func TestManaSwitchTurnsTheMembersDamageIntoTheBardsMP(t *testing.T) {
	p := newManaSwitchParty(t, true)
	// One impact per swing, so the HP the swing took is one hit.
	base := p.rt.deps.SkillData().(staticSkillSource)[memberBaseAttack]
	base.Attack.ImpactCount = 1
	p.rt.deps.SkillData().(staticSkillSource)[memberBaseAttack] = base
	p.link(t)

	r := p.rt.HandleTargetInteract(testDivision, p.member, wire.SkillAction{ActionId: memberBaseAttack, HasTarget: true, TargetGid: p.mob.Gid}.Encode())
	after, _ := p.rt.Monsters.Get(testDivision, p.mob.Gid)
	lost := p.mob.CurrentHP - after.CurrentHP
	if lost == 0 {
		t.Fatalf("the member's attack did not land: %+v", r)
	}
	if got, want := p.bardMP(), int64(lost*manaSwitchPercent/100); got != want {
		t.Fatalf("bard MP %d after a %d hit, want %d", got, lost, want)
	}
	if _, ok := findFrame(p.pushed[p.bard.Name], simulation.OpVitalsUpdate); !ok {
		t.Fatal("no 0x33A6 pushed to the Bard")
	}

	before := p.bardMP()
	p.memberHits(bigHit, smallHit)
	if got, want := p.bardMP()-before, int64(manaSwitchCeiling+smallHit*manaSwitchPercent/100); got != want {
		t.Fatalf("capped hits gave %d MP, want %d", got, want)
	}
}

/*
==================
TestManaSwitchRefusesSelfAndAHarplessBard

The row does not name Self: a cast on the Bard is refused 0x3006. A Bard
without a harp is refused 0x300D by the reqi pair. Neither installs a half
or charges MP.
==================
*/
func TestManaSwitchRefusesSelfAndAHarplessBard(t *testing.T) {
	for _, tc := range []struct {
		name   string
		harp   bool
		self   bool
		refuse byte
	}{
		{"self", true, true, 0x06},
		{"no harp", false, false, 0x0d},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newManaSwitchParty(t, tc.harp)
			target, before := p.mate, p.bardMP()
			if tc.self {
				target = p.bardGID
			}
			r := p.cast(target)
			if len(r.Frames) != 1 || r.Frames[0].Opcode != wire.OpSkillCastResult ||
				r.Frames[0].Payload[0] != 2 || r.Frames[0].Payload[1] != tc.refuse {
				t.Fatalf("refusal %+v, want 0x30%02x", r, tc.refuse)
			}
			if len(linkedHalves(p.rt, p.bard.Name, p.skill.ID))+len(linkedHalves(p.rt, p.member.Name, p.skill.ID)) != 0 || p.bardMP() != before {
				t.Fatal("a refused cast linked or charged")
			}
		})
	}
}

/*
==================
TestManaSwitchFeedsNothingAfterTheLinkEnds

Past its 20 s the link gives nothing; nor does it once the member walks
beyond the 700 link range, which retires both halves.
==================
*/
func TestManaSwitchFeedsNothingAfterTheLinkEnds(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		p := newManaSwitchParty(t, true)
		p.link(t)
		p.clock.Advance(manaSwitchDuration + time.Millisecond)
		p.rt.TickHook()(p.clock.NowMs())
		p.memberHits(smallHit)
		if got := p.bardMP(); got != 0 {
			t.Fatalf("an expired link gave %d MP", got)
		}
	})
	t.Run("out of range", func(t *testing.T) {
		p := newManaSwitchParty(t, true)
		p.link(t)
		key := simulation.WorldKey(testDivision, p.member.Name)
		p.rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(p.member) },
			func(w *simulation.WorldState) { w.Spawn.X += manaSwitchRange + 100 })
		p.rt.advanceLinkedEffects(p.clock.NowMs())
		p.rt.drainStoppedCharacterEffects()
		if len(linkedHalves(p.rt, p.bard.Name, p.skill.ID))+len(linkedHalves(p.rt, p.member.Name, p.skill.ID)) != 0 {
			t.Fatal("the link outlived its range")
		}
		p.memberHits(smallHit)
		if got := p.bardMP(); got != 0 {
			t.Fatalf("a retired link gave %d MP", got)
		}
	})
}
