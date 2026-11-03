/*
===========================================================================

combatcopy_test.go - the combat roots read nothing from the bag (#1943)

The basic attack (engage and every continuation tick), the projectile
release, the periodic pulse, the pulse area, the skill-object owner
pass, the attacking hawk's strike tick, the heal-over-time pulse's
retained caster view, the targeted recovery cast's recipient view,
the lowest-party-heal candidate copies, the party-resurrection
member copies and the Scream Mask's member snapshot and attacker
view take the narrow combat copy:
equipment rows and the avatar inventory, no bag rows. The skill press
takes the cast copy: the combat rows plus the companion-carrying bag
rows, because its flow resolves companions through the copy. Each scenario here runs twice on a bag loaded
with everything a root could trip on - spare ammunition, an attack pet's
summon row, a quest item - once on the narrow copy and once forced onto
the full copy, and every frame and the end state must agree. A root that
read a bag row through its copy would diverge.

===========================================================================
*/
package action

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
loadCombatBag

Fills c's bag with rows a bag reader would see: two spare arrow stacks,
an attack pet's summon row and a quest item.
================
*/
func loadCombatBag(c *enterworld.Character) {
	pet := &domain.CharacterCOS{GID: 0x7F00_0001, RefObjID: 3914, Codename: "COS_P_RAVEN", CurrentHP: 100, Summoned: true}
	c.MissionInventory = append(c.MissionInventory,
		enterworld.InventoryRow{Slot: 13, RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", StackCount: 250},
		enterworld.InventoryRow{Slot: 14, RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", StackCount: 250},
		enterworld.InventoryRow{Slot: 15, RefObjID: 3913, Codename: "ITEM_COS_P_RAVEN", StackCount: 1, Summon: pet},
		enterworld.InventoryRow{Slot: 16, RefObjID: 9999, Codename: "ITEM_QSP_TEST_01", StackCount: 1},
	)
}

/*
================
forceFullCombatCopy
================
*/
func forceFullCombatCopy(rt *Runtime) {
	rt.combatCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return rt.characterSnapshot(divisionID, character)
	}
}

/*
================
forceFullCastCopy
================
*/
func forceFullCastCopy(rt *Runtime) {
	rt.castCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return rt.characterSnapshot(divisionID, character)
	}
}

/*
================
combatRowsView

The rows by value: a summon row prints its companion, not its pointer.
================
*/
func combatRowsView(c *enterworld.Character) string {
	view := ""
	for _, row := range c.MissionInventory {
		summon := domain.CharacterCOS{}
		if row.Summon != nil {
			summon = *row.Summon
		}
		view += fmt.Sprintf("{%d %s x%d dur=%d plus=%d summon=%+v}", row.Slot, row.Codename, row.StackCount, row.Durability, row.Plus, summon)
	}
	return view
}

/*
================
basicAttackTrace

An engage, its release and the continuation tick that strikes again,
on a loaded bag: every frame, then the target's HP and the attacker's
rows.
================
*/
func basicAttackTrace(t *testing.T, full bool) string {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	loadCombatBag(character)
	if full {
		forceFullCombatCopy(rt)
	}
	trace := fmt.Sprintf("%+v\n", rt.HandleTargetInteract(testDivision, character, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()))
	for _, at := range []time.Duration{0, testBasicAttackActionDuration, testBasicAttackActionDuration + time.Millisecond} {
		trace += fmt.Sprintf("%+v\n", rt.TickHook()(clock.At(at).UnixMilli()))
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(character))
}

/*
================
projectileTrace

A critical arrow from preparation to release, on a loaded bag.
================
*/
func projectileTrace(t *testing.T, full bool) string {
	rt, c, target, skill, now := arrowFixture(t)
	loadCombatBag(c)
	if full {
		forceFullCombatCopy(rt)
	}
	cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}
	start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now, rt.handlerDoors())
	trace := fmt.Sprintf("%+v %v\n", start, decision)
	for _, at := range []int64{now + 300, now + 971, now + 2000} {
		trace += fmt.Sprintf("%+v\n", rt.advanceProjectileCasts(at))
	}
	after, _ := rt.Monsters.Get(testDivision, target)
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
periodicTrace

A persistent attack's cast and two pulses on a loaded bag. The pulse
root takes the combat copy of the caster; the bag carries an attack
pet's summon row, so a pulse that resolved the caster's companions
through its copy would find none on the narrow root and diverge.
================
*/
func periodicTrace(t *testing.T, full bool) string {
	rt, clock, c, target := periodicFixture(t, 10000)
	loadCombatBag(c)
	if full {
		forceFullCombatCopy(rt)
	}
	trace := fmt.Sprintf("%+v\n", rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: testPeriodicSkillID, HasTarget: true, TargetGid: target.Gid}.Encode()))
	for _, at := range []int64{clock.NowMs() + 2000, clock.NowMs() + 4000} {
		trace += fmt.Sprintf("%+v\n", rt.advancePeriodicEffects(at))
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP >= 10000 {
		t.Fatal("periodic pulses never landed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
skillPressTrace

A persistent attack pressed through the real 0x72CD door and two
pulses, on a loaded bag. The press takes the cast copy; the bag
carries an attack pet's summon row, so a press reader that needed a
plain bag row would diverge, and one that resolved the caster's
companions through its copy still finds the raven on both copies.
================
*/
func skillPressTrace(t *testing.T, full bool) string {
	rt, clock, c, target := periodicFixture(t, 10000)
	loadCombatBag(c)
	if full {
		forceFullCastCopy(rt)
	}
	trace := fmt.Sprintf("%+v\n", rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: testPeriodicSkillID, HasTarget: true, TargetGid: target.Gid}.Encode()))
	for _, at := range []int64{clock.NowMs() + 2000, clock.NowMs() + 4000} {
		trace += fmt.Sprintf("%+v\n", rt.advancePeriodicEffects(at))
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP >= 10000 {
		t.Fatal("periodic pulses never landed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
TestCastPressReadsNoPlainBagRows
================
*/
func TestCastPressReadsNoPlainBagRows(t *testing.T) {
	narrow, full := skillPressTrace(t, false), skillPressTrace(t, true)
	a, b := strings.Split(narrow, "\n"), strings.Split(full, "\n")
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			t.Errorf("skill press: line %d diverged\nnarrow: %.900s\nfull:   %.900s", i, a[i], b[min(i, len(b)-1)])
			break
		}
	}
}

/*
================
crowReleaseCopy

Which copy preparedCrowTrace's release takes: the production cast
copy, the full copy through the cast seam, or the combat copy through
the same seam.
================
*/
const (
	crowReleaseCast = iota
	crowReleaseFull
	crowReleaseCombat
)

/*
================
preparedCrowTrace

A crow owner's prepared release, on a bag whose raven record
rides only in its bag row: the owner charges Guiding Feathers to the
cap, then the pet's record moves out of ActiveCOS into the bag, so a
copy that drops bag rows resolves no crow at all. The cast is accepted
prepared (the monster must be unhurt until the release), the release
re-copies the caster and strikes, and the trace is the accept and
release frames and the end state. Armed with the crow, the release's
impacts carry its empowerment; without it they land plain.
================
*/
func preparedCrowTrace(t *testing.T, copyKind int) string {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	rt.AttackPetExpansion = true
	// The crow lane refuses town: stage in the field, follower at the
	// caster, and the strike target three metres out (roarField's shape
	// at sword reach instead of thirty metres).
	c.World.Spawn.RegionID = testInt64(0x62a6)
	rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
	rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID).follower = simulation.NewPetFollower(c.ActiveCOS.GID, rt.LiveSpawnFor(testDivision, c))
	lease, ok := rt.casterPopulation(testDivision, c)
	if !ok {
		t.Fatal("no caster population")
	}
	at := rt.LiveSpawnFor(testDivision, c)
	at.X += 3
	if rt.Monsters.SpawnGMMonsters(simulation.GMMonsterSpawn{Division: testDivision, Population: lease, RefObjID: 1933, Count: 1, Position: at, NowMs: clock.NowMs()}) != 1 {
		t.Fatal("no strike target")
	}
	rows := rt.Monsters.CombatCandidatesInPopulation(testDivision, lease, at, 1, clock.NowMs(), true)
	if len(rows) != 1 {
		t.Fatal("strike target not found")
	}
	target := rows[0]
	learnPetBook(t, c, 50)
	learnPetBook(t, c, 59)
	step, ok := rt.elementalCompanion(testDivision, c, c.ActiveCOS.GID, "Crow", clock.NowMs())
	if !ok {
		t.Fatal("crow fixture not eligible")
	}
	for range 8 {
		rt.crowAfterAttack(step, []uint32{target.Gid})
	}
	if got := step.state.crow.precision[enterworld.ObjectIDForCharacter(c)].stacks; got != crowChargeCap {
		t.Fatalf("precision stacks = %d, want the cap %d", got, crowChargeCap)
	}
	crow := c.ActiveCOS
	if crow == nil || !crow.Summoned {
		t.Fatal("crow fixture pet is not a summoned active pet")
	}
	c.ActiveCOS = nil
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 15, RefObjID: 3913, Codename: "ITEM_COS_P_RAVEN", StackCount: 1, Summon: crow})

	items := rt.deps.ItemReferences().(cosTestItemSource)
	items.staticItemSource["ITEM_CH_SWORD_01_A"] = &enterworld.ItemRef{
		RefObjID: 71,
		Codename: "ITEM_CH_SWORD_01_A",
		TypeIDs:  [4]int64{3, 1, 6, 2},
		Combat: &enterworld.ItemCombatRef{
			ActionRange:    6,
			PhysicalAttack: enterworld.ItemAttackRange{Minimum: enterworld.ItemStatRange{Min: 15, Max: 16, PerPlus: 2.4000001}, Maximum: enterworld.ItemStatRange{Min: 16, Max: 18, PerPlus: 2.4000001}},
			MagicalAttack:  enterworld.ItemAttackRange{Minimum: enterworld.ItemStatRange{Min: 25, Max: 26, PerPlus: 4.0999999}, Maximum: enterworld.ItemStatRange{Min: 28, Max: 31, PerPlus: 4.0999999}},
			HitRate:        enterworld.ItemStatRange{Min: 24, Max: 30},
			CriticalRate:   enterworld.ItemStatRange{Min: 3, Max: 15},
		},
	}
	// A damaging non-base skill: Guiding Feathers empowers only those
	// (crowSkillEmpowers), so the fixture's base attack cannot witness it.
	// The shipped bow row is the prepared direct skill the projectile
	// lane already pins (arrowFixture's shape, on this runtime's items).
	weapon := items.staticItemSource[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 6
	weapon.Combat.ActionRange = 180
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	arrow := &enterworld.ItemRef{RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", TypeIDs: [4]int64{3, 3, 4, 1}}
	items.staticItemSource[arrow.Codename] = arrow
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 7, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: 10})
	skill := shippedOffense(t, "SKILL_CH_BOW_CRITICAL_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(100)
	rt.CombatRoll = func() (uint32, error) { return 10, nil }

	switch copyKind {
	case crowReleaseFull:
		forceFullCastCopy(rt)
	case crowReleaseCombat:
		rt.castCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return rt.characterSnapshotCombat(divisionID, character)
		}
	}

	before, _ := rt.Monsters.Get(testDivision, target.Gid)
	now := clock.NowMs()
	press, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshotCast(testDivision, c), wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}, now, rt.handlerDoors())
	trace := fmt.Sprintf("%+v %v\n", press, decision)
	prepared, _ := rt.Monsters.Get(testDivision, target.Gid)
	if prepared.CurrentHP != before.CurrentHP {
		t.Fatal("the press struck at once; the trace would pin the press copy, not the release")
	}
	trace += fmt.Sprintf("%+v\n", rt.advanceProjectileCasts(now+2000))
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP >= before.CurrentHP {
		t.Fatal("the prepared release never landed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
TestPreparedReleaseResolvesTheBagRowCrow

The prepared-cast release takes the cast copy: its strike resolves
the caster's crow through the bag-row Summon record (the crow lane
at the strike), so the release lands exactly as on the full copy.
On the combat copy the row is dropped, no crow answers, and the
empowerment is silently lost - the failure #1997 left at this root
when it converted the release and the press lane diverged.
================
*/
func TestPreparedReleaseResolvesTheBagRowCrow(t *testing.T) {
	cast := preparedCrowTrace(t, crowReleaseCast)
	full := preparedCrowTrace(t, crowReleaseFull)
	if cast != full {
		a, b := strings.Split(cast, "\n"), strings.Split(full, "\n")
		for i := range a {
			if i >= len(b) || a[i] != b[i] {
				t.Fatalf("prepared release: line %d diverged\ncast: %.900s\nfull: %.900s", i, a[i], b[min(i, len(b)-1)])
			}
		}
		t.Fatal("prepared release: cast and full traces differ in length")
	}
	if combat := preparedCrowTrace(t, crowReleaseCombat); combat == cast {
		t.Fatal("prepared release on the combat copy matched: the crow never empowered the release strike")
	}
}

/*
================
pulseAreaTrace

A Soul Chaos instance installed directly, then two due pulses, on a
loaded bag. The pulse root takes the combat copy of the caster; the
bag carries an attack pet's summon row, so a pulse that resolved the
caster's companions through its copy would diverge on the narrow root.
================
*/
func pulseAreaTrace(t *testing.T, full bool) string {
	rt, _, c, row, mob := warlockFixture(t, "SKILL_EU_WARLOCK_SOULA_CHAOS_A_01")
	loadCombatBag(c)
	if full {
		forceFullCombatCopy(rt)
	}
	now := rt.Now().UnixMilli()
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: row.ID,
		SkillGroup: row.Group, InstanceToken: 9, State: statuseffect.StateActive,
		StartedAtMs: now - 2000, DurationPresent: true, ExpiresAtMs: now + 14000}) {
		t.Fatal("Soul Chaos did not install")
	}
	rt.trackPulseArea(testDivision, c.Name)
	trace := fmt.Sprintf("%+v\n", rt.advancePulseAreas(now))
	trace += fmt.Sprintf("%+v\n", rt.advancePulseAreas(now+2000))
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if after.CurrentHP >= 100000 {
		t.Fatal("pulse-area pulses never landed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
TestPulseAreaReadsTheCombatCopy

Guard for the #1943 pulse-area slice: the pulse reads the caster's
combat copy, not the whole record. On this rig (bag bulked to 44 plain
rows past the combat fixture's own) one due pulse measures 59,288
allocated bytes through the combat copy and 64,383 through the former
full copy - the bag rows are the difference - so the bound sits
between them while leaving the narrow root headroom.
================
*/
func TestPulseAreaReadsTheCombatCopy(t *testing.T) {
	rt, _, c, row, _ := warlockFixture(t, "SKILL_EU_WARLOCK_SOULA_CHAOS_A_01")
	loadCombatBag(c)
	for slot := int64(20); slot < 60; slot++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 9999, Codename: "ITEM_QSP_TEST_01", StackCount: 1})
	}
	now := rt.Now().UnixMilli()
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: row.ID,
		SkillGroup: row.Group, InstanceToken: 9, State: statuseffect.StateActive,
		StartedAtMs: now - 2000, DurationPresent: true, ExpiresAtMs: now + 1000000}) {
		t.Fatal("Soul Chaos did not install")
	}
	rt.trackPulseArea(testDivision, c.Name)
	at := now
	pulse := func() {
		at += 2000
		if len(rt.advancePulseAreas(at)) == 0 {
			t.Fatal("pulse produced no frames")
		}
	}
	for i := 0; i < 10; i++ {
		pulse()
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 100; i++ {
		pulse()
	}
	runtime.ReadMemStats(&after)
	if got := (after.TotalAlloc - before.TotalAlloc) / 100; got > 62000 {
		t.Fatalf("pulse allocated %d bytes, want <= 62000 (whole-record copy back?)", got)
	}
}

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
targetedResurrectionRun

One Reverse Oblation (REBIRTHA_SPECIAL_A_01) cast on a dead mate
dressed with a +900 HP option and the loaded bag, and the revival
offer the proposal stores. The row heals 100 percent of the
recipient's maximum, so the offer is the derived maximum the stats
funnel computes over the recipient view's equipment band: 1100
dressed, 200 with the band stripped.
================
*/
func targetedResurrectionRun(t *testing.T, copyKind int) (string, int64) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_SPECIAL_A_01")
	affordable(&skill)
	p := newSupportPair(t, skill)
	a := p.m
	*a.CurrentHP = 0
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
	switch copyKind {
	case resuCopyFull:
		forceFullCombatCopy(p.rt)
	case resuCopyPoisoned:
		p.rt.combatCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			view := character.SnapshotCombat()
			view.MissionInventory = nil
			return view
		}
	}
	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(a)})
	trace := fmt.Sprintf("%+v %+v\n", r, batches)
	offer, ok := p.rt.resurrections.take(testDivision, a.Name, p.clock.NowMs())
	if !ok || offer.hp <= 0 {
		t.Fatal("no revival offer stored for the dead mate; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("offer hp=%d mp=%d rows=%s", offer.hp, offer.mp, combatRowsView(a)), offer.hp
}

/*
================
targetedResurrectionTrace

The table's adapter for targetedResurrectionRun: the recipient view
on the narrow copy must propose exactly what the full copy proposes,
frame for frame and vital for vital.
================
*/
func targetedResurrectionTrace(t *testing.T, full bool) string {
	if full {
		trace, _ := targetedResurrectionRun(t, resuCopyFull)
		return trace
	}
	trace, _ := targetedResurrectionRun(t, resuCopyNarrow)
	return trace
}

/*
================
TestTargetedResurrectionOfferReadsTheEquipmentBand

The seeing-eye for the recipient view's resurrection leg, shipped:
stripping the view's equipment band drops the stored offer by
exactly the dressed option (1100 to 200). A root that sized the
offer from anything but the copy's band - or lost the band on the
way - fails here even though the differential above still agrees.
================
*/
func TestTargetedResurrectionOfferReadsTheEquipmentBand(t *testing.T) {
	_, armed := targetedResurrectionRun(t, resuCopyNarrow)
	_, poisoned := targetedResurrectionRun(t, resuCopyPoisoned)
	if armed-poisoned != 900 {
		t.Fatalf("offer hp armed %d, band-stripped %d: the revival vitals do not read the view's equipment band", armed, poisoned)
	}
}

/*
================
screamCopyKind

Which copy the scream-mask runs take: the production combat copy,
the full copy through the seam, or the combat copy with one named
character's equipment band replaced by a row the item source cannot
resolve.
================
*/
const (
	screamCopyNarrow = iota
	screamCopyFull
	screamCopyMemberCorrupt
	screamCopyAttackerCorrupt
)

/*
================
screamMaskInstallLink

Puts the warlock's shipped Scream Mask link on the member, as the
buff install does in production.
================
*/
func screamMaskInstallLink(t *testing.T, rt *Runtime, skill enterworld.SkillRow, warlock, member *enterworld.Character, now int64) {
	t.Helper()
	l := statuseffect.Link{DivisionID: testDivision, SourceName: warlock.Name, TargetName: member.Name,
		SourceGID: enterworld.ObjectIDForCharacter(warlock), TargetGID: enterworld.ObjectIDForCharacter(member),
		SourceToken: 100, TargetToken: 101, SkillID: skill.ID, SkillGroup: skill.Group, MaxOutgoing: 2, MaxDistance: 1500,
		ScreamRange: skill.TimedEffect.Link.ScreamRange, ExpiresAtMs: now + 60000}
	if code := rt.effects.ApplyLink(l); code != 0 {
		t.Fatal(code)
	}
}

/*
================
screamMaskArmCopy

Arms the copy seam for one scream-mask run. The corrupt legs hand
the named character a copy whose only equipment row the item source
cannot resolve, so the stats funnel fails on that copy and the roll
must refuse: the band, not the bag, is what these roots size off.
================
*/
func screamMaskArmCopy(rt *Runtime, copyKind int, memberName, attackerName string) {
	switch copyKind {
	case screamCopyFull:
		forceFullCombatCopy(rt)
	case screamCopyMemberCorrupt:
		screamMaskCorruptCopy(rt, memberName)
	case screamCopyAttackerCorrupt:
		screamMaskCorruptCopy(rt, attackerName)
	}
}

/*
================
screamMaskCorruptCopy
================
*/
func screamMaskCorruptCopy(rt *Runtime, name string) {
	rt.combatCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		view := character.SnapshotCombat()
		if character.Name == name {
			view.MissionInventory = []enterworld.InventoryRow{{Slot: 6, RefObjID: 0x7FFF_FFFF, Codename: "ITEM_MISSING_SCREAM"}}
		}
		return view
	}
}

/*
================
screamMaskMonsterRun

One Scream Mask answer to a monster striking the masked member from
the member's own pose, and its end state: the published frames, the
attacker's stun and HP, the member's rows. The member's copy feeds
the roll's caster side (its level and getv modifiers through the
stats funnel) and the record's source identity.
================
*/
func screamMaskMonsterRun(t *testing.T, copyKind int) (string, bool) {
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skill := shippedOffense(t, "SKILL_EU_WARLOCK_SOULA_STUNLINK_A_07")
	if !skill.TimedEffect.Pinned || !skill.TimedEffect.Link.Scream || skill.TimedEffect.Link.ScreamRange == 0 {
		t.Fatalf("Scream Mask not admitted: %+v", skill.TimedEffect.Link)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	warlock := nearbyCharacter(rt, c, 20, "warlock", 30)
	mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
	if !ok {
		t.Fatal("fixture monster has no mover")
	}
	pose := mover.LivePoseAt(clock.NowMs(), nil)
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
			w.SpawnSet = true
		})
	screamMaskInstallLink(t, rt, skill, warlock, c, clock.NowMs())
	loadCombatBag(c)
	screamMaskArmCopy(rt, copyKind, c.Name, "")
	out := rt.screamMaskMonster(testDivision, c, target, pose, clock.NowMs())
	live, _ := rt.Monsters.Get(testDivision, target.Gid)
	stunned := live.Abnormal != nil && live.Abnormal.Slots[abnormal.Stun].Active
	return fmt.Sprintf("%+v\n", out) + fmt.Sprintf("stunned=%v hp=%d rows=%s", stunned, live.CurrentHP, combatRowsView(c)), stunned
}

/*
================
screamMaskMonsterTrace

The table's adapter for screamMaskMonsterRun: the member snapshot
on the narrow copy must answer a monster's hit exactly as the full
copy answers it, frames and end state alike.
================
*/
func screamMaskMonsterTrace(t *testing.T, full bool) string {
	kind := screamCopyNarrow
	if full {
		kind = screamCopyFull
	}
	trace, stunned := screamMaskMonsterRun(t, kind)
	if !stunned {
		t.Fatal("the attacker was never stunned; the trace would agree vacuously")
	}
	return trace
}

/*
================
screamMaskPlayerRun

One Scream Mask answer to a player striking the masked member, and
its end state: the published frames, the attacker's stun and HP,
both characters' rows. This branch detaches twice per hit - the
member's snapshot (caster side) and the attacker's admitted view
(its spawn lookup, defender stats and the roll's victim side).
================
*/
func screamMaskPlayerRun(t *testing.T, copyKind int) (string, bool) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skill := shippedOffense(t, "SKILL_EU_WARLOCK_SOULA_STUNLINK_A_07")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	warlock := nearbyCharacter(rt, c, 20, "warlock", 30)
	attacker := nearbyCharacter(rt, c, 21, "attacker", 10)
	hp := *c.CurrentHP
	attacker.CurrentHP = &hp
	screamMaskInstallLink(t, rt, skill, warlock, c, clock.NowMs())
	loadCombatBag(c)
	loadCombatBag(attacker)
	screamMaskArmCopy(rt, copyKind, c.Name, attacker.Name)
	out := rt.screamMaskPlayer(testDivision, c, attacker, clock.NowMs())
	block := rt.playerAbnormal(testDivision, attacker.Name)
	stunned := block != nil && block.Slots[abnormal.Stun].Active
	return fmt.Sprintf("%+v\n", out) + fmt.Sprintf("stunned=%v ahp=%d mrows=%s arows=%s", stunned, *attacker.CurrentHP, combatRowsView(c), combatRowsView(attacker)), stunned
}

/*
================
screamMaskPlayerTrace

The table's adapter for screamMaskPlayerRun: the member snapshot
and the attacker view on the narrow copy must answer a player's
hit exactly as the full copies answer it.
================
*/
func screamMaskPlayerTrace(t *testing.T, full bool) string {
	kind := screamCopyNarrow
	if full {
		kind = screamCopyFull
	}
	trace, stunned := screamMaskPlayerRun(t, kind)
	if !stunned {
		t.Fatal("the attacker was never stunned; the trace would agree vacuously")
	}
	return trace
}

/*
================
TestScreamMaskCopiesReadTheirEquipmentBand

The seeing-eye for the scream-mask roots, shipped: with the armed
copies the attacker is stunned; hand any one of the three copies -
the member's snapshot in either branch, the attacker's admitted
view - an equipment band the item source cannot resolve and the
stats funnel fails on that copy, so no roll runs and nobody is
stunned. With the injected roll pinned to always proc, the chance
arithmetic cannot move; the funnel over the copy's band is the one
channel that can refuse. A root that took its stats from anything
but its copy - or lost the band on the way - fails here even
though the differential above still agrees.
================
*/
func TestScreamMaskCopiesReadTheirEquipmentBand(t *testing.T) {
	if _, stunned := screamMaskMonsterRun(t, screamCopyNarrow); !stunned {
		t.Fatal("monster branch: the attacker was not stunned off the armed member copy")
	}
	if _, stunned := screamMaskMonsterRun(t, screamCopyMemberCorrupt); stunned {
		t.Fatal("monster branch: the roll ran off a member copy with an unreadable equipment band")
	}
	if _, stunned := screamMaskPlayerRun(t, screamCopyNarrow); !stunned {
		t.Fatal("player branch: the attacker was not stunned off the armed copies")
	}
	if _, stunned := screamMaskPlayerRun(t, screamCopyAttackerCorrupt); stunned {
		t.Fatal("player branch: the roll ran off an attacker view with an unreadable equipment band")
	}
	if _, stunned := screamMaskPlayerRun(t, screamCopyMemberCorrupt); stunned {
		t.Fatal("player branch: the roll ran off a member copy with an unreadable equipment band")
	}
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
	}{{"basic attack", basicAttackTrace}, {"projectile release", projectileTrace}, {"periodic pulse", periodicTrace}, {"pulse area", pulseAreaTrace}, {"skill object explosion", skillObjectTrace}, {"hawk strike", hawkTrace}, {"heal-over-time pulse", healOverTimeTrace}, {"targeted heal", targetedHealTrace}, {"lowest-party heal", lowestHealTrace}, {"party resurrection", partyResurrectionTrace}, {"targeted resurrection", targetedResurrectionTrace}, {"scream mask (monster attacker)", screamMaskMonsterTrace}, {"scream mask (player attacker)", screamMaskPlayerTrace}} {
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

/*
================
TestSnapshotCombatKeepsEquipmentAndAvatarOnly

The narrow copy's shape: the equipment rows and the whole avatar
inventory, no bag row.
================
*/
func TestSnapshotCombatKeepsEquipmentAndAvatarOnly(t *testing.T) {
	_, _, c, _ := newCombatTestRuntime(t, 100)
	loadCombatBag(c)
	c.AvatarInventory = &domain.AvatarInventory{Rows: []domain.InventoryRow{{Slot: 0, RefObjID: 7, Codename: "ITEM_AVATAR_TEST"}}}
	copy := c.SnapshotCombat()
	for _, row := range copy.MissionInventory {
		if row.Slot >= 13 {
			t.Fatalf("bag row %d in the combat copy", row.Slot)
		}
	}
	if copy.AvatarInventory == nil || len(copy.AvatarInventory.Rows) != 1 {
		t.Fatalf("avatar inventory = %+v, want the one row", copy.AvatarInventory)
	}
	equipped := 0
	for _, row := range c.MissionInventory {
		if row.Slot < 13 {
			equipped++
		}
	}
	if len(copy.MissionInventory) != equipped {
		t.Fatalf("combat copy has %d rows, the character wears %d", len(copy.MissionInventory), equipped)
	}
}
