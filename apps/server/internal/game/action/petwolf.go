/*
===========================================================================
petwolf.go - port-only wolf/owner Execute and independent seventh-attack sweeps.

Only normal committed attack actions enter here. Executions use the existing
credited monster HP/reward door and publish death, not another cast or animation.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"slices"
	"strings"
)

/*
================
wolfSource

The division lock owns transient counters. The character read door supplies
one canonical snapshot; another player's wolf never grants owner bonuses.
================
*/
func (rt *Runtime) wolfSource(division string, owner *enterworld.Character, gid uint32, now int64) (petCombatStep, companion.SkillDef, uint8, bool) {
	step, ok := rt.wolfCompanion(division, owner, gid, now)
	if !ok {
		return step, companion.SkillDef{}, 0, false
	}
	skill, level, ok := companion.Passive(step.pet, "Execute", companion.ModifiersFor(step.pet))
	return step, skill, level, ok
}

/*
================
wolfCompanion

Common eligibility for every wolf bonus; no learned Execute prerequisite.
The copy stays the full one: eligibility itself resolves the wolf
through the owner's inventory rows, and companion resolution walks
bag rows - the narrow equipment copy drops the pet it just admitted
(#1943; the domain SnapshotEquipment pin).
================
*/
func (rt *Runtime) wolfCompanion(division string, owner *enterworld.Character, gid uint32, now int64) (petCombatStep, bool) {
	return rt.elementalCompanionMatching(division, owner, elementalQuery{gid: gid, kind: "Wolf"}, now)
}

/*
================
elementalCompanion

Shared port-only PvE eligibility for the wolf and frost caster.
================
*/
func (rt *Runtime) elementalCompanion(division string, owner *enterworld.Character, gid uint32, kind string, now int64) (petCombatStep, bool) {
	return rt.elementalCompanionEligibility(division, owner, gid, kind, now, false)
}

/*
================
elementalQuery

Which pet an eligibility walk looks for. passive, when set, admits only
a pet with that passive slotted: the per-tick passive lanes (Tailwind,
Guiding Feathers) refuse every other crow before copying its owner.
================
*/
type elementalQuery struct {
	gid       uint32
	kind      string
	allowTown bool
	passive   string
}

/*
================
hasElementalCandidate

The record half of elementalCompanionMatching's pet filter: plain field
reads and catalogue lookups, safe inside the read door. Every pet it
rejects the walk (or, for passive, the caller's own Passive check on
the selected pet) rejects too, so admission never changes the outcome.
================
*/
func hasElementalCandidate(c *enterworld.Character, q elementalQuery) bool {
	found := false
	c.VisitCompanions(func(pet *domain.CharacterCOS) bool {
		found = pet.BroodStage == 0 && (q.gid == 0 || pet.GID == q.gid) && pet.Summoned && pet.CurrentHP != 0 &&
			companion.PetKind(pet.Codename) == q.kind && (q.passive == "" || hasPassive(pet, q.passive))
		return !found
	})
	return found
}

func hasPassive(pet *domain.CharacterCOS, name string) bool {
	_, _, ok := companion.Passive(pet, name, companion.Modifiers{})
	return ok
}

/*
================
elementalPetFacts

One companion's record-filter row in an owner's armed facts (#2030):
the fields hasElementalCandidate judges per pet - gid, kind, and the
two passives any elemental query names.
================
*/
type elementalPetFacts struct {
	gid      uint32
	kind     string
	tailwind bool
	feathers bool
}

/*
================
elementalWalkFacts

What one owner's elemental walk judges on, captured once per pet tick
by armElementalFacts (#2030): the owner's record viability, the
expansion owner half, the town verdict, one record-filter row per
companion, and the one pet-owner snapshot the admitted lanes share
(installed by the first lane that reaches the copy). The lanes used
to repeat elementalWalkRefused's door read - and, past it, the owner
copy - once per lane, three to four times per owner per tick.
================
*/
type elementalWalkFacts struct {
	owner       *enterworld.Character
	nowMs       int64
	ownerViable bool
	expansionOK bool
	townJudged  bool
	inTown      bool
	pets        []elementalPetFacts
	snapshot    *enterworld.Character
}

/*
================
elementalWalkFacts.walkRefused

The facts' own answer to elementalWalkRefused for one query, judged
without a store read. answered is false when the facts cannot decide -
the owner has no world entry to judge the town on (the copy path seeds
and judges it exactly as before), or the query names a passive the
capture never folded - and the caller then walks as before.
================
*/
func (f *elementalWalkFacts) walkRefused(q elementalQuery) (refused, answered bool) {
	switch q.passive {
	case "", "Tailwind", "Guiding Feathers":
	default:
		return false, false
	}
	if !f.ownerViable || !f.expansionOK {
		return true, true
	}
	candidate := false
	for _, pet := range f.pets {
		if (q.gid == 0 || pet.gid == q.gid) && pet.kind == q.kind &&
			(q.passive == "" || q.passive == "Tailwind" && pet.tailwind || q.passive == "Guiding Feathers" && pet.feathers) {
			candidate = true
			break
		}
	}
	if !candidate {
		return true, true
	}
	if !f.townJudged {
		return false, false
	}
	return !q.allowTown && f.inTown, true
}

/*
================
armElementalFacts

Capture one pet owner's walk facts under its division lane and arm
them for the tailwind, crow-kit, plume and wolf lanes of this tick.
The capture reads the record exactly where the lanes' own first read
read it - after the brood, penguin and gear lanes have had their
writes - and folds the same record filter hasElementalCandidate folds
per query, for every companion at once. The returned key clears the
arm; the zero key means nothing armed (the expansion off, the seam
up, or no session yet), and every lane then walks as before. The
capture itself is recycled across ticks (takeElementalFacts):
arming and clearing stay the only doors, so a capture never outlives
the tick that armed it. The capture reads on the stack read guard
instead of the callback door: the callback boxed its closure and
captures on every pet tick, and the guard holds the same store read
lock the callback runs between (#2033).
================
*/
func (rt *Runtime) armElementalFacts(key petOwnerKey, now int64) petOwnerKey {
	var none petOwnerKey
	if !rt.AttackPetExpansion || rt.Monsters == nil || rt.companionCopy != nil || rt.Worlds == nil {
		return none
	}
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil || state.character == nil {
		return none
	}
	owner := state.character
	facts := rt.takeElementalFacts(owner, now)
	guard := rt.deps.AcquireRead(key.division)
	facts.ownerViable = !owner.DeletePending && enterworld.CharacterAlive(owner)
	if facts.ownerViable {
		owner.VisitCompanions(func(pet *domain.CharacterCOS) bool {
			if pet.BroodStage == 0 && pet.Summoned && pet.CurrentHP != 0 {
				facts.pets = append(facts.pets, elementalPetFacts{
					gid:      pet.GID,
					kind:     companion.PetKind(pet.Codename),
					tailwind: hasPassive(pet, "Tailwind"),
					feathers: hasPassive(pet, "Guiding Feathers"),
				})
			}
			return true
		})
		facts.expansionOK = len(facts.pets) > 0 && !rt.inFreeBattle(owner) &&
			!wearsJobSuit(owner, rt.deps.ItemReferences()) &&
			domain.CharacterWorldInstance(owner) == domain.DefaultWorldInstance
	}
	guard.Release()
	if facts.ownerViable {
		if ownerWorld, ok := rt.Worlds.SnapshotExisting(simulation.WorldKey(key.division, owner.Name)); ok {
			facts.townJudged = true
			facts.inTown = townRegion(ownerWorld.LiveSpawnAt(now).RegionID)
		}
	}
	ownerKey := petOwnerKey{division: key.division, name: strings.ToLower(owner.Name)}
	rt.elementalFactsMu.Lock()
	if rt.elementalFacts == nil {
		rt.elementalFacts = make(map[petOwnerKey]*elementalWalkFacts)
	}
	// An entry still armed for this owner was never cleared (a cut
	// tick's arm): it answers nothing any more, so it recycles too.
	if stale := rt.elementalFacts[ownerKey]; stale != nil && stale != facts {
		rt.elementalFactsFree = append(rt.elementalFactsFree, stale)
	}
	rt.elementalFacts[ownerKey] = facts
	rt.elementalFactsMu.Unlock()
	return ownerKey
}

/*
================
takeElementalFacts

One owner's walk-facts capture for this tick: the last recycled
capture when one waits, a fresh one otherwise. A recycled capture
comes back reset - last tick's pet rows, town verdict and shared
snapshot gone, only the pet rows' backing kept - because a capture
that outlives its tick would answer for a record that has moved on.
armedElementalFacts' owner and clock guards still refuse a stale
arm; recycling only decides where the struct comes from (#2030: a
fresh capture per owner per tick was the elemental lanes' largest
remaining flat allocation).
================
*/
func (rt *Runtime) takeElementalFacts(owner *enterworld.Character, now int64) *elementalWalkFacts {
	rt.elementalFactsMu.Lock()
	var facts *elementalWalkFacts
	if free := rt.elementalFactsFree; len(free) > 0 {
		facts = free[len(free)-1]
		rt.elementalFactsFree = free[:len(free)-1]
	}
	rt.elementalFactsMu.Unlock()
	if facts == nil {
		return &elementalWalkFacts{owner: owner, nowMs: now, pets: make([]elementalPetFacts, 0, 4)}
	}
	pets := facts.pets
	*facts = elementalWalkFacts{owner: owner, nowMs: now, pets: pets[:0]}
	return facts
}

/*
================
armedElementalFacts

The facts armed for this owner and tick, if any. The companionCopy
seam abstains exactly as elementalWalkRefused abstains: behind it the
walk must judge on the full copy, so no entry is served. Owner
identity and the tick's clock both guard the entry, so a stale arm
can never answer for a re-seated character or a later tick.
================
*/
func (rt *Runtime) armedElementalFacts(division string, owner *enterworld.Character, now int64) (*elementalWalkFacts, bool) {
	if rt.companionCopy != nil || owner == nil {
		return nil, false
	}
	rt.elementalFactsMu.Lock()
	facts := rt.elementalFacts[petOwnerKey{division: division, name: strings.ToLower(owner.Name)}]
	rt.elementalFactsMu.Unlock()
	if facts == nil || facts.owner != owner || facts.nowMs != now {
		return nil, false
	}
	return facts, true
}

/*
================
clearElementalFacts

Drop one owner's armed facts into the recycle list
(takeElementalFacts resets it for its next arm). advancePetInDivision
clears after the wolf lane: the spirit, bond and hunt lanes that
follow must judge fresh records, as they always have.
================
*/
func (rt *Runtime) clearElementalFacts(ownerKey petOwnerKey) {
	if ownerKey.division == "" {
		return
	}
	rt.elementalFactsMu.Lock()
	if facts := rt.elementalFacts[ownerKey]; facts != nil {
		delete(rt.elementalFacts, ownerKey)
		rt.elementalFactsFree = append(rt.elementalFactsFree, facts)
	}
	rt.elementalFactsMu.Unlock()
}

/*
================
elementalCompanionEligibility

Only travel support may opt into safe zones; combat callers keep the town gate.
================
*/
func (rt *Runtime) elementalCompanionEligibility(division string, owner *enterworld.Character, gid uint32, kind string, now int64, allowTown bool) (petCombatStep, bool) {
	return rt.elementalCompanionMatching(division, owner, elementalQuery{gid: gid, kind: kind, allowTown: allowTown}, now)
}

/*
================
elementalWalkRefused

The eligibility walk's refusal judged on door-read values (#2030), so
a pet whose owner record already refuses - dead, deletion-pending,
no companion passing the record filters, or the expansion facts -
never pays for the pet-owner copy the walk used to take first. Every
input weighs the same facts the copy's admission and loop gates weigh:
hasElementalCandidate is the loop's own record filter, the expansion
facts are petExpansionAllowed's owner half (its pet half is the kind
match the filter already enforced), and the town verdict reads the
world the judged pet tick reads (SnapshotExisting). Judged is false
when the facts cannot decide - the owner has no world entry yet, so
the copy path seeds and judges it exactly as before. A walk the facts
admit still takes the copy: the selected lanes read step.snapshot
root by root, and only a refusal is provable from values. The
companionCopy seam abstains the same way petjudged's does.
================
*/
func (rt *Runtime) elementalWalkRefused(division string, owner *enterworld.Character, q elementalQuery, now int64) (refused, judged bool) {
	if rt.companionCopy != nil {
		return false, false
	}
	admitted := false
	expansionOK := false
	rt.deps.Read(division, func() {
		admitted = !owner.DeletePending && enterworld.CharacterAlive(owner) && hasElementalCandidate(owner, q)
		if admitted {
			expansionOK = !rt.inFreeBattle(owner) && !wearsJobSuit(owner, rt.deps.ItemReferences()) &&
				domain.CharacterWorldInstance(owner) == domain.DefaultWorldInstance
		}
	})
	if !admitted || !expansionOK {
		return true, true
	}
	ownerWorld, ok := rt.Worlds.SnapshotExisting(simulation.WorldKey(division, owner.Name))
	if !ok {
		return false, false
	}
	return !q.allowTown && townRegion(ownerWorld.LiveSpawnAt(now).RegionID), true
}

/*
================
elementalCompanionMatching

The one eligibility walk. The owner copy is the full admitted one,
because the walk itself resolves the pet through the owner's
inventory rows (#1943).
================
*/
func (rt *Runtime) elementalCompanionMatching(division string, owner *enterworld.Character, q elementalQuery, now int64) (petCombatStep, bool) {
	var step petCombatStep
	if !rt.AttackPetExpansion || rt.Monsters == nil {
		return step, false
	}
	// Facts armed for this owner's tick (#2030) judge the refusal and
	// lend the one owner snapshot; a walk the facts cannot decide,
	// and every walk with nothing armed, runs exactly as before.
	if facts, ok := rt.armedElementalFacts(division, owner, now); ok {
		if refused, answered := facts.walkRefused(q); answered {
			if refused {
				return step, false
			}
			return rt.elementalMatchingOnFacts(division, owner, q, now, facts)
		}
	}
	allowTown := q.allowTown
	// A refusal the door-read facts already decide never reaches the
	// copy below (#2030); an admitted walk judges as before.
	if refused, judged := rt.elementalWalkRefused(division, owner, q, now); judged && refused {
		return step, false
	}
	// Runs per pet per tick: copy the owner only when a pet could pass the
	// record filters below (#1943), and then only the pet-owner view
	// (#2030): the walk and every elemental lane reading step.snapshot
	// read companions, life, town, world, party and the relation facts.
	snapshot := rt.characterSnapshotCompanionAdmitted(division, owner, func(c *enterworld.Character) bool {
		return !c.DeletePending && enterworld.CharacterAlive(c) && hasElementalCandidate(c, q)
	})
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) || !allowTown && rt.inTown(division, snapshot) {
		return step, false
	}
	return rt.elementalSelect(division, snapshot, q, now)
}

/*
================
elementalMatchingOnFacts

The admitted half of an armed walk: the lanes share the facts' one
owner snapshot instead of copying per lane. The first lane to reach
the copy takes it under the walk's own admission and installs it;
the admission judge runs on the copy it installed exactly as the
unarmed walk judges. A snapshot a previous lane installed already
passed that judge on these same captured values.
================
*/
func (rt *Runtime) elementalMatchingOnFacts(division string, owner *enterworld.Character, q elementalQuery, now int64, facts *elementalWalkFacts) (petCombatStep, bool) {
	var step petCombatStep
	snapshot := facts.snapshot
	if snapshot == nil {
		snapshot = rt.characterSnapshotCompanionAdmitted(division, owner, func(c *enterworld.Character) bool {
			return !c.DeletePending && enterworld.CharacterAlive(c) && hasElementalCandidate(c, q)
		})
		if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) || !q.allowTown && rt.inTown(division, snapshot) {
			return step, false
		}
		facts.snapshot = snapshot
	}
	return rt.elementalSelect(division, snapshot, q, now)
}

/*
================
elementalSelect

The pet loop of the eligibility walk over one admitted owner copy:
the first companion passing every gate wins the step.
================
*/
func (rt *Runtime) elementalSelect(division string, snapshot *enterworld.Character, q elementalQuery, now int64) (petCombatStep, bool) {
	var step petCombatStep
	gid, kind, allowTown := q.gid, q.kind, q.allowTown
	for _, pet := range snapshot.Companions() {
		if pet.BroodStage != 0 {
			continue
		}
		if gid != 0 && pet.GID != gid || companion.PetKind(pet.Codename) != kind || !pet.Summoned || pet.CurrentHP == 0 || !rt.petExpansionAllowed(snapshot, pet) {
			continue
		}
		state := rt.petSessionFor(division, snapshot.Name, pet.GID)
		if state == nil || !state.ready || state.follower == nil {
			continue
		}
		from := state.follower.Position(now)
		at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
		if !samePlaneAdjacent(from, at) || areaDistance(from, at) > 20*petAreaPerMeter || !allowTown && townRegion(from.RegionID) {
			continue
		}
		ref, found := rt.cosReference(pet)
		if !found {
			continue
		}
		step = petCombatStep{key: petOwnerKey{division: division, name: strings.ToLower(snapshot.Name), gid: pet.GID}, state: state, snapshot: snapshot, pet: pet, ref: ref, nowMs: now}
		return step, true
	}
	return step, false
}

/*
================
resetWolfExecution

Book changes, separation, death, unsummon and eligibility loss clear progress.
================
*/
func (rt *Runtime) resetWolfExecution(key petOwnerKey, now int64) {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil {
		return
	}
	step, skill, _, ok := rt.wolfSource(key.division, state.character, key.gid, now)
	if !ok || skill.Option != 3 || state.wolfBook != skill.ID {
		state.wolfHits = [2]uint8{}
		state.wolfBook = 0
		if ok && skill.Option == 3 {
			step.state.wolfBook = skill.ID
		}
	}
}

/*
================
petStrikeDamaged
================
*/
func petStrikeDamaged(hit monsterStrike) bool {
	for _, impact := range hit.impacts {
		if !impact.Blocked && impact.Damage > 0 {
			return true
		}
	}
	return false
}

/*
================
wolfExecuteAttack

actor0 is the wolf,1 the owner. Nonempty targets are committed damaging monster
hits, collected once per action after all primary/area victims have settled.
Player victims (an owner's PK hit) are dropped: the kit is PvE only.
================
*/
func (rt *Runtime) wolfExecuteAttack(division string, owner *enterworld.Character, gid uint32, actor int, targets []uint32, now int64) {
	if rt.Monsters == nil {
		return
	}
	targets = slices.DeleteFunc(slices.Clone(targets), func(target uint32) bool {
		_, monster := rt.Monsters.Get(division, target)
		return !monster
	})
	if len(targets) == 0 {
		return
	}
	if step, ok := rt.wolfCompanion(division, owner, gid, now); ok {
		rt.wolfKitAttack(step, actor, targets)
	}
	step, skill, level, ok := rt.wolfSource(division, owner, gid, now)
	if !ok || actor == 1 && skill.Option < 2 {
		return
	}
	if actor == 1 {
		for _, target := range targets {
			rt.wolfExecuteVictim(step, skill, level, target)
		}
	}
	if skill.Option != 3 {
		return
	}
	if step.state.wolfBook != skill.ID {
		step.state.wolfBook, step.state.wolfHits = skill.ID, [2]uint8{}
	}
	step.state.wolfHits[actor] = (step.state.wolfHits[actor] + 1) % 7
	if step.state.wolfHits[actor] != 0 {
		return
	}
	from := step.state.follower.Position(now)
	if actor == 1 {
		from = rt.liveSpawn(simulation.WorldKey(division, step.snapshot.Name), step.snapshot, now)
	}
	lease, found := rt.casterPopulation(division, step.snapshot)
	if !found {
		return
	}
	if actor == 1 {
		rt.wolfEmote(division, enterworld.ObjectIDForCharacter(step.snapshot), wolfEmoteSweep)
	} else {
		rt.wolfEmote(division, step.pet.GID, wolfEmoteSweep)
	}
	_, nav := rt.liveNav(simulation.WorldKey(division, step.snapshot.Name), step.snapshot, now)
	if actor == 0 {
		world, _ := step.state.follower.Presentation()
		nav = world.LiveOwnerAt(now)
	}
	for _, mob := range rt.Monsters.CombatCandidatesInPopulation(division, lease, from, 10*petAreaPerMeter, now, false) {
		target, found := rt.resolvePetCombatTarget(step, mob.Gid)
		if !found || target.monster == nil || !samePlaneAdjacent(from, target.at) || areaDistance(from, target.at) > 10*petAreaPerMeter {
			continue
		}
		if rt.LineOfSight != nil && !rt.LineOfSight(from, nav, target.at) {
			continue
		}
		rt.wolfExecuteVictim(step, skill, level, mob.Gid)
	}
}

/*
================
wolfExecuteVictim

Fresh HP and expected-HP commit prevent double kills/rewards. No call back into
wolfExecuteAttack: sweep victims never generate attacks or recursive sweeps.
================
*/
func (rt *Runtime) wolfExecuteVictim(step petCombatStep, skill companion.SkillDef, level uint8, gid uint32) {
	target, found := rt.resolvePetCombatTarget(step, gid)
	if !found || target.monster == nil {
		return
	}
	mob := *target.monster
	unique := mob.Rarity()&0x0f == 3 || mob.Rarity()&0x0f == 8
	threshold := companion.ExecutionThreshold(skill, level, petTargetBleeds(mob))
	if unique || mob.CurrentHP == 0 || float64(mob.CurrentHP)*100 >= float64(mob.Ref.MaxHP)*threshold {
		return
	}
	rt.wolfBonusHit(step, mob, mob.CurrentHP, "wolf-execute")
}
