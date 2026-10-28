/*
===========================================================================
petshield.go - port-only Iron Hide absorption, five-second lifetime and burst

Shields are transient combat state, never HP or saved character data. The pet
tick expires/publishes them; normal hit doors consume them before HP debit.
===========================================================================
*/
package action

import (
	"encoding/binary"
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const petShieldOpcode uint16 = 0x3FB2
const petShieldDuration int64 = 5000
const petShieldPartyRange = 200.0 // 20m, matching the expansion's game-unit scale.
const petShieldBurstRange = 80.0  // 8m around each protected player and bear.

/*
================
petShieldKey / petShield / petShieldStore
================
*/
type petShieldKey struct {
	division string
	gid      uint32
}
type petShield struct {
	key              petShieldKey
	source           petOwnerKey
	name             string
	amount, maxHP    uint32
	until, publishAt int64
	burst            bool
}
type petShieldStore struct {
	sync.Mutex
	rows map[petShieldKey]petShield
}

/*
================
petShieldFrame

Fixed 21-byte port frame: recipient, absorption HP, reference max HP, remaining
milliseconds, caster and flags (bit0 = expiry explosion). No native opcode changes.
================
*/
func petShieldFrame(s petShield, now int64, explode bool) wire.Frame {
	p := binary.LittleEndian.AppendUint32(nil, s.key.gid)
	p = binary.LittleEndian.AppendUint32(p, s.amount)
	p = binary.LittleEndian.AppendUint32(p, s.maxHP)
	p = binary.LittleEndian.AppendUint32(p, uint32(max(int64(0), s.until-now)))
	p = binary.LittleEndian.AppendUint32(p, s.source.gid)
	flags := byte(0)
	if explode {
		flags = 1
	}
	p = append(p, flags)
	return wire.Frame{Opcode: petShieldOpcode, Payload: p}
}

/*
================
castPetShield

Tier I protects bear + owner; II/III also snapshot living nearby party members.
Replacing a shield never stacks absorption or detonates the old shield.
================
*/
func (rt *Runtime) castPetShield(division string, owner, snapshot *enterworld.Character, pet *domain.CharacterCOS, skill companion.SkillDef, now int64) OpResult {
	if rt.inTown(division, snapshot) {
		return petTrainingReceipt("Iron Hide cannot be cast in town.")
	}
	mods := companion.ModifiersFor(pet)
	level := companion.EffectiveLevel(pet, skill, mods)
	empowered := false
	// updated and shieldMaxHP are captured inside the cast door: the pet
	// is never re-read unlocked after it closes (checklist item 2). The
	// shield scales off the UNBUFFED base maximum, so 9 stacks and the
	// 10-stack empowered cast (which spends the stacks first) progress
	// monotonically instead of the base jumping when the spend lands.
	var updated *domain.CharacterCOS
	var shieldMaxHP uint32
	if !rt.deps.Update(owner, "pet-shield-cast", func() bool {
		live := owner.CompanionByGID(pet.GID)
		if live == nil || !companion.RowReady(&live.Training, skill.Row, now) {
			return false
		}
		companion.StartRow(&live.Training, skill.Row, skill, level, now, mods)
		live.Training.Queued = 0
		unbuffed := *live
		unbuffed.ResolveStacks = 0
		if ref, ok := rt.cosReference(&unbuffed); ok {
			shieldMaxHP = ref.MaxHP
		}
		empowered = rt.spendPetSpirit(owner, live)
		copy := *live
		updated = &copy
		return true
	}) {
		return petTrainingReceipt("The shield could not be cast.")
	}
	source := petOwnerKey{division: division, name: owner.Name, gid: pet.GID}
	var shields []petShield
	if shieldMaxHP > 0 {
		shields = append(shields, petShield{key: petShieldKey{division, pet.GID}, source: source, maxHP: shieldMaxHP, burst: skill.Option == 3})
	}
	from := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	members := map[uint32]bool{enterworld.ObjectIDForCharacter(snapshot): true}
	if skill.Option >= 2 {
		for gid := range rt.auraParty(division, snapshot) {
			members[gid] = true
		}
	}
	for gid := range members {
		member := rt.characterSnapshot(division, rt.findCharacterByGid(division, gid))
		if gid == enterworld.ObjectIDForCharacter(snapshot) {
			member = snapshot
		}
		if member == nil || !enterworld.CharacterAlive(member) || !rt.petExpansionAllowed(member, pet) || rt.inTown(division, member) {
			continue
		}
		at := rt.liveSpawn(simulation.WorldKey(division, member.Name), member, now)
		if !samePlaneAdjacent(from, at) || areaDistance(from, at) > petShieldPartyRange {
			continue
		}
		stats, _, err := rt.playerCombatStats(division, member)
		if err != nil {
			continue
		}
		maxHP, _, _, _ := keeperVitalsFrom(member, stats, nil)
		shields = append(shields, petShield{key: petShieldKey{division, gid}, source: source, name: member.Name, maxHP: uint32(maxHP), burst: skill.Option == 3})
	}
	public := []wire.Frame{{
		Opcode:  wire.OpActionEmote,
		Payload: wire.ActionEmotePush{Gid: pet.GID, Action: skill.ID}.Encode(),
	}}
	shieldPercent := min(100.0, skill.ValueAt(level))
	if empowered {
		shieldPercent *= 1.5
		public = append(public, petSpiritFrame(updated, now, now), wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(pet.GID, 0, updated.CurrentHP)})
	}
	rt.petShields.Lock()
	if rt.petShields.rows == nil {
		rt.petShields.rows = make(map[petShieldKey]petShield)
	}
	for _, s := range shields {
		s.amount = uint32(float64(s.maxHP) * shieldPercent / 100)
		s.until, s.publishAt = now+petShieldDuration, now+500
		if old, ok := rt.petShields.rows[s.key]; ok && old.until > now && old.amount > s.amount {
			continue
		}
		rt.petShields.rows[s.key] = s
		public = append(public, petShieldFrame(s, now, false))
	}
	rt.petShields.Unlock()
	return OpResult{Frames: append(append([]wire.Frame{}, public...), rt.petTrainingStateFrames(updated, now)...), Broadcast: public}
}

/*
================
absorbPetShield

Post-mitigation damage consumes absorption once, including periodic damage.
The next pet tick publishes the remaining amount. Expired shields absorb nothing.
================
*/
func (rt *Runtime) absorbPetShield(division string, gid, damage uint32, now int64) uint32 {
	if !rt.AttackPetExpansion {
		return damage
	}
	rt.petShields.Lock()
	defer rt.petShields.Unlock()
	key := petShieldKey{division, gid}
	s, ok := rt.petShields.rows[key]
	if !ok || s.until <= now {
		return damage
	}
	absorbed := min(damage, s.amount)
	s.amount -= absorbed
	s.publishAt = 0
	rt.petShields.rows[key] = s
	return damage - absorbed
}

/*
================
advancePetShields

No independent timers. Revalidate the caster and recipient before expiry damage;
death, logout, unsummon, town/instance entry cancel without detonating.
================
*/
func (rt *Runtime) advancePetShields(now int64) []simulation.DivisionFrames {
	rt.petShields.Lock()
	var due []petShield
	for key, s := range rt.petShields.rows {
		if s.publishAt <= now || s.until <= now {
			due = append(due, s)
			s.publishAt = now + 500
			rt.petShields.rows[key] = s
		}
	}
	rt.petShields.Unlock()
	var out []simulation.DivisionFrames
	for _, s := range due {
		unlock := rt.lockDivision(s.key.division)
		// A cast/hit may have changed this row while we waited for its division.
		rt.petShields.Lock()
		current, exists := rt.petShields.rows[s.key]
		rt.petShields.Unlock()
		if !exists {
			unlock()
			continue
		}
		s = current
		owner := rt.characterSnapshotCompanionAdmitted(s.key.division, rt.findCharacter(s.key.division, s.source.name), nil)
		valid := owner != nil && enterworld.CharacterAlive(owner) && !rt.inTown(s.key.division, owner)
		if valid {
			pet := owner.CompanionByGID(s.source.gid)
			session := rt.petSessionFor(s.key.division, owner.Name, s.source.gid)
			valid = pet != nil && pet.Summoned && pet.CurrentHP > 0 && session != nil && session.ready && rt.petExpansionAllowed(owner, pet)
		}
		var recipient *enterworld.Character
		if valid && s.name != "" {
			recipient = rt.characterSnapshotCompanionAdmitted(s.key.division, rt.findCharacter(s.key.division, s.name), nil)
			valid = recipient != nil && enterworld.CharacterAlive(recipient) &&
				rt.petExpansionAllowed(recipient, owner.CompanionByGID(s.source.gid)) && !rt.inTown(s.key.division, recipient)
		}
		if valid && recipient != nil && recipient.Name != owner.Name {
			_, valid = rt.auraParty(s.key.division, owner)[s.key.gid]
		}
		if valid && recipient != nil {
			a := rt.liveSpawn(simulation.WorldKey(s.key.division, owner.Name), owner, now)
			b := rt.liveSpawn(simulation.WorldKey(s.key.division, recipient.Name), recipient, now)
			valid = samePlaneAdjacent(a, b)
		}
		expired := now >= s.until
		explode := valid && expired && s.burst && s.amount > 0
		if expired || !valid || s.amount == 0 {
			rt.petShields.Lock()
			delete(rt.petShields.rows, s.key)
			rt.petShields.Unlock()
			remaining := s.amount
			s.amount = 0
			s.until = now
			if explode {
				at := rt.companionLiveSpawn(s.key.division, owner, owner.CompanionByGID(s.source.gid), now)
				if recipient != nil {
					at = rt.liveSpawn(simulation.WorldKey(s.key.division, recipient.Name), recipient, now)
				}
				out = append(out, rt.burstPetShield(s, owner, at, remaining/2, now)...)
			}
		}
		out = append(out, simulation.DivisionFrames{DivisionID: s.key.division, SourceGID: s.key.gid, Frames: simFrames([]wire.Frame{petShieldFrame(s, now, explode)})})
		unlock()
	}
	return out
}

/*
================
burstPetShield

Uses the established credited fixed-hit settlement, retaining loot/XP and threat.
Only ordinary world monsters are admitted; there is no player/PvP splash.
================
*/
func (rt *Runtime) burstPetShield(s petShield, owner *enterworld.Character, at simulation.Spawn, damage uint32, now int64) []simulation.DivisionFrames {
	if damage == 0 || rt.Monsters == nil {
		return nil
	}
	lease, ok := rt.casterPopulation(s.key.division, owner)
	if !ok {
		return nil
	}
	liveOwner := rt.findCharacter(s.key.division, owner.Name)
	pet := owner.CompanionByGID(s.source.gid)
	ref, ok := rt.cosReference(pet)
	if !ok || len(ref.Parameters.DefaultSkillIDs) == 0 {
		return nil
	}
	data := rt.deps.SkillData()
	if data == nil {
		return nil
	}
	row, ok := data.SkillByID(ref.Parameters.DefaultSkillIDs[0])
	if !ok {
		return nil
	}
	var out []simulation.DivisionFrames
	for _, mob := range rt.Monsters.CombatCandidatesInPopulation(s.key.division, lease, at, petShieldBurstRange, now, false) {
		formula := combat.Result{Damage: min(damage, wire.MaxSkillActionDamage), ResultFlags: 1}
		hit, ok := rt.commitCompanionCreditedMonsterHit(s.key.division, liveOwner, owner, row, mob, formula, "pet-shield-burst", now)
		if !ok {
			continue
		}
		rt.commitSkillHostility(s.key.division, s.source.gid, mob.Gid, row, hit.impacts, now)
		public := []wire.Frame{wire.SkillPulseFrame(s.key.gid, row.ID, []wire.SkillAreaTarget{{GID: mob.Gid, Impacts: []wire.SkillCastTargetImpact{committedSkillImpact(formula, hit.impacts[0])}}})}
		result := rt.creditedHitResult(s.key.division, mob, hit, public, now)
		out = append(out, simulation.DivisionFrames{DivisionID: s.key.division, SourceGID: s.key.gid, Frames: simFrames(result.Broadcast)})
		if len(result.ActorPrivate) > 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: s.key.division, OnlyCharacterID: owner.ID, Frames: simFrames(result.ActorPrivate)})
		}
		for _, r := range result.Recipients {
			out = append(out, simulation.DivisionFrames{DivisionID: s.key.division, OnlyCharacterID: r.CharacterID, Frames: simFrames(r.Frames)})
		}
	}
	return out
}

/*
================
petShieldRecipientAllowed

Hit-time exclusion without reading character storage inside its write door.
================
*/
func (rt *Runtime) petShieldRecipientAllowed(c *enterworld.Character) bool {
	return rt.AttackPetExpansion && !rt.inFreeBattle(c) && !wearsJobSuit(c, rt.deps.ItemReferences()) && domain.CharacterWorldInstance(c) == domain.DefaultWorldInstance
}

/*
================
retirePetShields

Actor teardown cancels both received shields and shields cast by that actor.
The next tick publishes zero; a disconnected party recipient cannot burst.
================
*/
func (rt *Runtime) retirePetShields(division, name string) {
	rt.petShields.Lock()
	defer rt.petShields.Unlock()
	for key, shield := range rt.petShields.rows {
		if key.division == division && (strings.EqualFold(shield.name, name) || strings.EqualFold(shield.source.name, name)) {
			shield.amount, shield.publishAt, shield.burst = 0, 0, false
			rt.petShields.rows[key] = shield
		}
	}
}
