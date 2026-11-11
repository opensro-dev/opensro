/*
===========================================================================

combatcopy_roots_test.go - the trace battery: nineteen roots, two copies

One test runs nineteen combat roots on a bag loaded with everything a
root could trip on - spare ammunition, an attack pet's summon row, a
quest item - once on the narrow combat copy and once forced onto the
whole record, and every line of the two traces must agree; a root that
read a bag row through its copy would diverge. The scenario drivers
for the roots without bespoke assertions live here with the test that
runs them; the shared fixtures (the loaded bag, the copy seams, the
rows view), the basic-attack, projectile, periodic and pulse-area
drivers, and the roots with their own tests live in
combatcopy_test.go.

===========================================================================
*/

package action

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
skillObjectTrace

A planted fire trap scanned until it explodes on the monster beside
its planter, on a loaded bag. The skill-object pass takes the combat
copy of the owner once per object per tick; the bag carries an attack
pet's summon row, so an object tick that resolved the owner's
companions through its copy would diverge on the narrow root.
================
*/
func skillObjectTrace(t *testing.T, full bool) string {
	rt, clock, c, skill, gid := combatTrapFixture(t)
	loadCombatBag(c)
	if full {
		forceFullCombatCopy(rt)
	}
	before, _ := rt.Monsters.Get(testDivision, gid)
	release := plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	trace := fmt.Sprintf("%+v\n", rt.drainSkillFinalizes(release+int64(skill.ActionDurationMs)))
	trace += fmt.Sprintf("%+v\n", rt.AdvanceSkillObjects(release+int64(enterworld.CombatTrapScanMs), nil))
	after, _ := rt.Monsters.Get(testDivision, gid)
	if after.CurrentHP >= before.CurrentHP || len(rt.SkillObjects.Snapshot()) != 0 {
		t.Fatal("trap never exploded; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
hawkTrace

One Black Hawk strike at the interval on a loaded bag, and its end
state: the target's HP, the caster's rows and the hawk record. The
strike root takes the combat copy of the caster; the bag carries an
attack pet's summon row, so a strike that resolved the caster's
companions through its copy would find none on the narrow root and
diverge.
================
*/
func hawkTrace(t *testing.T, full bool) string {
	rt, c, target, h, now, interval := aimedHawk(t)
	loadCombatBag(c)
	if full {
		forceFullCombatCopy(rt)
	}
	before, _ := rt.Monsters.Get(testDivision, target)
	at := now + interval
	trace := fmt.Sprintf("%+v\n", rt.advanceHawk(h, at))
	after, _ := rt.Monsters.Get(testDivision, target)
	if after.CurrentHP >= before.CurrentHP {
		t.Fatal("the hawk strike never landed; the trace would agree vacuously")
	}
	record := rt.hawks[hawkKey(testDivision, c.Name)]
	return trace + fmt.Sprintf("hp=%d rows=%s record=%+v", after.CurrentHP, combatRowsView(c), *record)
}

/*
================
healOverTimeTrace

One Healing Orbit cast on a loaded bag and its first pulse. The pulse
root retains the caster's copy at release and reads the weapon term
(the slot-6 staff's magical pair, scaled by the caster's intellect
through the stats funnel) and HLRU from that copy on every pulse; the
bag carries an attack pet's summon row, so a pulse that resolved the
caster's companions or bag rows through its copy would diverge. The
recipients' strength is raised so the pulse lands below their gauge
maximum - at the fixture maximum the clamp would hide the weapon
term the pulse reads through the copy.
================
*/
func healOverTimeTrace(t *testing.T, full bool) string {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_CYCLE_B_01")
	affordable(&skill)
	p := newSupportParty(t, skill)
	p.c.Strength, p.m.Strength = testInt64(3000), testInt64(3000)
	loadCombatBag(p.c)
	if full {
		forceFullCombatCopy(p.rt)
	}
	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})
	trace := fmt.Sprintf("%+v %+v\n", r, batches)
	p.clock.Advance(time.Duration(int64(skill.ActionCastingTimeMs)+1+int64(skill.Recovery.PulseMs)) * time.Millisecond)
	trace += fmt.Sprintf("%+v\n", p.rt.advanceHealsOverTime(p.clock.NowMs()))
	if len(p.rt.healsOverTime) == 0 {
		t.Fatal("no heal over time installed; the trace would agree vacuously")
	}
	for _, heal := range p.rt.healsOverTime {
		staff := false
		for _, row := range heal.caster.MissionInventory {
			staff = staff || row.Slot == 6
		}
		if !staff {
			t.Fatal("the retained caster copy lost its slot-6 staff; the weapon term reads nothing")
		}
	}
	if *p.m.CurrentHP <= 1 && *p.c.CurrentHP <= 40 {
		t.Fatal("the heal-over-time pulse never healed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("casterhp=%d matehp=%d rows=%s", *p.c.CurrentHP, *p.m.CurrentHP, combatRowsView(p.c))
}

/*
================
targetedHealTrace

One targeted Healing cast on a bag-loaded party mate and the heal it
lands. The admission root detaches the recipient's view for the cast
and reads its facts (the spacing seat, the admission checks, the
chain pick's identity) from that view; the heal amounts themselves
run on the live mate in its door. The loaded bag rides the RECIPIENT
here, not the caster as in the other rows: a root that read the
mate's bag rows or resolved the mate's companions through its view
would diverge.
================
*/
func targetedHealTrace(t *testing.T, full bool) string {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_TARGET_A_01")
	affordable(&skill)
	p := newSupportPair(t, skill)
	loadCombatBag(p.m)
	if full {
		forceFullCombatCopy(p.rt)
	}
	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(p.m)})
	trace := fmt.Sprintf("%+v %+v\n", r, batches)
	if *p.m.CurrentHP <= 1 {
		t.Fatal("the targeted heal never landed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("matehp=%d matemp=%d rows=%s", *p.m.CurrentHP, *p.m.CurrentMP, combatRowsView(p.m))
}

/*
================
recoveryEffectTrace

One targeted Healing cast on a bag-loaded party mate, through the
recovery effect's recipient pick. The pick detaches one copy per
vector entry and reads only its aliveness; the token-0 effect add
for the mate is the pick's visible output, so the trace holds the
result, the batches and that add's presence. A pick that read the
mate's bag rows or resolved the mate's companions through its copy
would diverge or drop the add.
================
*/
func recoveryEffectTrace(t *testing.T, full bool) string {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_TARGET_A_01")
	affordable(&skill)
	p := newSupportPair(t, skill)
	loadCombatBag(p.m)
	if full {
		forceFullCombatCopy(p.rt)
	}
	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(p.m)})
	trace := fmt.Sprintf("%+v %+v\n", r, batches)
	mateGID := enterworld.ObjectIDForCharacter(p.m)
	adds := effectAdds(t, skill, r, batches)
	if len(adds) != 1 || adds[0] != mateGID {
		t.Fatalf("recovery effect adds = %v, want the mate %d once; the trace would agree vacuously", adds, mateGID)
	}
	if *p.m.CurrentHP <= 1 {
		t.Fatal("the targeted heal never landed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("matehp=%d adds=%v rows=%s", *p.m.CurrentHP, adds, combatRowsView(p.m))
}

/*
================
lowestHealTrace

One Healing Division cast over a party whose lowest-ratio member is
decided by its equipment: the candidate copies feed the pick's HP
ratio through the stats funnel, so matea - dressed with a +900 HP
option that lifts its maximum to 1100 - is the lowest at 100 HP,
where undressed it would tie the plain mate and lose to mateb. The
winner takes the whole heal, the rest the reduction's share. Both
candidates carry the loaded bag: a pick that read a candidate's bag
rows or resolved its companions through the copy would diverge.
The caster stands at 190 HP so it is never the pick itself.
================
*/
func lowestHealTrace(t *testing.T, full bool) string {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_DIVIDE_A_01")
	affordable(&skill)
	p := newSupportParty(t, skill, 50, 80)
	p.c.CurrentHP = testInt64(190)
	m, a, b := p.mates[0], p.mates[1], p.mates[2]
	m.CurrentHP, a.CurrentHP, b.CurrentHP = testInt64(100), testInt64(100), testInt64(90)

	armor := &enterworld.ItemRef{RefObjID: 9, Codename: "TEST_HP_ARMOR", TypeIDs: [4]int64{3, 1, 1, 1}, Combat: &enterworld.ItemCombatRef{}}
	items := p.rt.deps.ItemReferences().(staticItemSource)
	items[armor.Codename] = armor
	p.rt.deps.(*enterworld.Deps).Items = items
	p.rt.deps.(*enterworld.Deps).MagicOptions = gaugeOptions{140: {ParamID: 140, OptionName: "MATTR_HP", Tag: hpTag()}}
	a.MissionInventory = append(a.MissionInventory, enterworld.InventoryRow{
		Slot: 1, RefObjID: armor.RefObjID, Codename: armor.Codename, TypeFlags: armor.TypeFlags(),
		VarianceBits: "0", Durability: 1, StackCount: 1, MagicOptions: []uint64{uint64(900)<<32 | 140},
	})
	loadCombatBag(a)
	loadCombatBag(b)
	if full {
		forceFullCombatCopy(p.rt)
	}
	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})
	trace := fmt.Sprintf("%+v %+v\n", r, batches)
	if *a.CurrentHP <= 100 || *a.CurrentHP < *b.CurrentHP {
		t.Fatalf("the dressed member did not take the whole heal (a %d, b %d); the trace would agree vacuously", *a.CurrentHP, *b.CurrentHP)
	}
	return trace + fmt.Sprintf("c=%d m=%d a=%d b=%d rows=%s|%s",
		*p.c.CurrentHP, *m.CurrentHP, *a.CurrentHP, *b.CurrentHP, combatRowsView(a), combatRowsView(b))
}

/*
================
partyResurrectionTrace

One Group Reverse cast over a party with one dead, bag-loaded member.
The proposal arm detaches a copy per member and reads its death and
pending facts, its level against the resu word and its vitals through
the stats funnel; the shipped party rows are flat-only, so the
stored offer is the row's flat 763 here and what the trace holds the
copies to is the proposal itself. A member the copies misread - one
whose bag rows or companions the arm resolved through its copy -
would diverge.
================
*/
func partyResurrectionTrace(t *testing.T, full bool) string {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_GROUP_A_01")
	affordable(&skill)
	p := newSupportParty(t, skill, 50)
	a := p.mates[1]
	*a.CurrentHP = 0
	loadCombatBag(a)
	if full {
		forceFullCombatCopy(p.rt)
	}
	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})
	trace := fmt.Sprintf("%+v %+v\n", r, batches)
	offer, ok := p.rt.resurrections.take(testDivision, a.Name, p.clock.NowMs())
	if !ok || offer.hp <= 0 {
		t.Fatal("no revival offer stored for the dead member; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("offer hp=%d mp=%d rows=%s", offer.hp, offer.mp, combatRowsView(a))
}

/*
================
resuCopyKind

Which copy targetedResurrectionRun's proposal takes: the production
combat copy, the full copy through the seam, or the combat copy with
its equipment band stripped.
================
*/
const (
	resuCopyNarrow = iota
	resuCopyFull
	resuCopyPoisoned
)

/*
================
timedTargetTrace

A Force Blessing linked onto an ally whose bag is loaded, and its end
state: the recipient half on the ally, the ally's strength and its
rows. The timed-target root takes the combat copy of the ALLY; the
load-bearing read is the ally's current Strength through that copy:
the caster's charity (HLFS 40) makes the raw write 4 + 40, so the
recipient half's bonus is set by the 50 % cap on the ally's Strength,
and the ally carries that Strength on an equipment row's magic
option. A copy that lost the equipment band would cap the bonus
lower and the end state would diverge; the bag's summon row would
diverge it too if the root resolved the ally's companions through
its copy.
================
*/
func timedTargetTrace(t *testing.T, full bool) string {
	rt, _, c := concealmentFixture(t, forceBlessingA1)
	rt.deps.(*enterworld.Deps).MagicOptions = gaugeOptions{11: {ParamID: 11, OptionName: "MATTR_STR", Tag: 0x737472}}
	var charity enterworld.SkillRow
	charity.ID, charity.Group, charity.Level = 90003, 90003, 1
	charity.PassiveParameters.Pinned = true
	charity.PassiveParameters.Mask = enterworld.SkillParameterMask(1) << enterworld.ParameterBlessStrength
	charity.PassiveParameters.Values[enterworld.ParameterBlessStrength] = 40
	rt.deps.SkillData().(staticSkillSource)[charity.ID] = charity
	c.Skills = append(c.Skills, charity.ID)
	ally := nearbyCharacter(rt, c, 21, "ally", 1)
	bare, _, err := rt.playerCombatStats(testDivision, ally)
	if err != nil {
		t.Fatal(err)
	}
	for i := range ally.MissionInventory {
		if ally.MissionInventory[i].Slot <= 12 {
			ally.MissionInventory[i].MagicOptions = append(ally.MissionInventory[i].MagicOptions, uint64(20)<<32|11)
		}
	}
	dressed, _, err := rt.playerCombatStats(testDivision, ally)
	if err != nil {
		t.Fatal(err)
	}
	if dressed.Strength <= bare.Strength {
		t.Fatalf("the strength option never landed: bare %v dressed %v; the cap would bind on nothing", bare.Strength, dressed.Strength)
	}
	loadCombatBag(ally)
	if full {
		forceFullCombatCopy(rt)
	}
	cast := wire.SkillAction{ActionId: forceBlessingA1, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(ally)}.Encode()
	r := rt.HandleTargetInteract(testDivision, c, cast)
	if r.DiagnosticRefusal != "" {
		t.Fatalf("timed target refused: %+v", r)
	}
	halves := linkedHalves(rt, ally.Name, forceBlessingA1)
	if len(halves) != 1 || halves[0] != 2 {
		t.Fatalf("the blessing never landed on the ally; the trace would agree vacuously: %v", halves)
	}
	blessed, _, err := rt.playerCombatStats(testDivision, ally)
	if err != nil {
		t.Fatal(err)
	}
	if want := float32(int32(dressed.Strength)) * 0.5; float32(blessed.Strength-dressed.Strength) != want {
		t.Fatalf("recipient bonus %v, want the 50%% cap %v of the ally's dressed strength %v; the cap never bound", blessed.Strength-dressed.Strength, want, dressed.Strength)
	}
	return fmt.Sprintf("%+v\n", r) + fmt.Sprintf("halves=%v strength=%v rows=%s", halves, blessed.Strength, combatRowsView(ally))
}

/*
================
TestCombatRootsReadNoBagRowsFromTheirCopy
================
*/
func TestCombatRootsReadNoBagRowsFromTheirCopy(t *testing.T) {
	for _, c := range []struct {
		name  string
		trace func(*testing.T, bool) string
	}{{"basic attack", basicAttackTrace}, {"projectile release", projectileTrace}, {"periodic pulse", periodicTrace}, {"pulse area", pulseAreaTrace}, {"skill object explosion", skillObjectTrace}, {"hawk strike", hawkTrace}, {"heal-over-time pulse", healOverTimeTrace}, {"targeted heal", targetedHealTrace}, {"lowest-party heal", lowestHealTrace}, {"party resurrection", partyResurrectionTrace}, {"targeted resurrection", targetedResurrectionTrace}, {"recovery effect", recoveryEffectTrace}, {"scream mask (monster attacker)", screamMaskMonsterTrace}, {"scream mask (player attacker)", screamMaskPlayerTrace}, {"support-cast walker (heal)", supportHealWalkTrace}, {"support-cast walker (guard)", supportGuardWalkTrace}, {"support-cast walker (duplicate)", supportDuplicateWalkTrace}, {"support-cast walker (discord)", supportDiscordWalkTrace}, {"timed target", timedTargetTrace}} {
		narrow, full := c.trace(t, false), c.trace(t, true)
		a, b := strings.Split(narrow, "\n"), strings.Split(full, "\n")
		for i := range a {
			if i >= len(b) || a[i] != b[i] {
				t.Errorf("%s: line %d diverged\nnarrow: %.900s\nfull:   %.900s", c.name, i, a[i], b[min(i, len(b)-1)])
				break
			}
		}
	}
}
