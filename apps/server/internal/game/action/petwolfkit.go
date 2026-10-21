/*
===========================================================================
petwolfkit.go - port-only wolf attack stacks, prey marks and manual howl.

Transient combat memory never rewrites learned ranks or native reference rows.
The division owns attacks; the howl store protects cross-session stat readers.
===========================================================================
*/
package action

import (
	"encoding/binary"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
wolfKitState / wolfHowl / wolfHowlStore
================
*/
type wolfKitState struct {
	bloodBook   uint8
	blood       [2]uint8
	bloodUntil  [2]int64
	huntBook    uint8
	huntHits    uint8
	huntTarget  uint32
	huntUntil   int64
	howlUntil   int64
	howlPercent float64
	// howlHoldUntil keeps the wolf still while its pet-howl clip plays.
	howlHoldUntil int64
}

// Port-only: the wolf holds still for most of the client's 1.6 s pet-howl clip.
const wolfHowlHoldMs int64 = 1400

type wolfHowl struct {
	until   int64
	percent float64
	members map[uint32]string
}
type wolfHowlStore struct {
	sync.Mutex
	rows  map[petOwnerKey]wolfHowl
	blood map[petOwnerKey]wolfBloodProjection
}

/*
================
wolfBloodCap
================
*/
func wolfBloodCap(tier int) uint8 {
	return [3]uint8{5, 8, 10}[max(1, min(tier, 3))-1]
}

/*
================
wolfKitAttack

One call per committed attack action, regardless of impacts/secondary targets.
================
*/
func (rt *Runtime) wolfKitAttack(step petCombatStep, actor int, targets []uint32) {
	state := &step.state.wolf
	mods := companion.ModifiersFor(step.pet)
	if s, _, ok := companion.Passive(step.pet, "Bloodscent", mods); ok && (actor == 0 || s.Option >= 2) {
		if state.bloodBook != s.ID {
			state.bloodBook, state.blood = s.ID, [2]uint8{}
		}
		if state.bloodUntil[actor] <= step.nowMs {
			state.blood[actor] = 0
		}
		before := state.blood[actor]
		state.blood[actor] = min(wolfBloodCap(s.Option), state.blood[actor]+1)
		state.bloodUntil[actor] = step.nowMs + 6000
		if before < state.blood[actor] && state.blood[actor] == wolfBloodCap(s.Option) {
			gid := step.pet.GID
			if actor == 1 {
				gid = enterworld.ObjectIDForCharacter(step.snapshot)
			}
			rt.wolfEmote(step.key.division, gid, wolfEmoteBloodFull)
		}
	}
	rt.wolfHuntAttack(step, actor, targets)
	rt.refreshWolfBlood(step, true)
}

/*
================
wolfDamageBonus

Bloodscent adds once, with separate owner/wolf stacks. Every read
checks the learned book and deadline, so changing slots cannot retain a bonus.
================
*/
func wolfDamageBonus(step petCombatStep, actor int) (float64, float64) {
	run := &step.state.wolf
	mods := companion.ModifiersFor(step.pet)
	damage, crit := 0.0, 0.0
	if s, level, ok := companion.Passive(step.pet, "Bloodscent", mods); ok && run.bloodBook == s.ID && run.bloodUntil[actor] > step.nowMs && (actor == 0 || s.Option >= 2) {
		stacks := float64(min(run.blood[actor], wolfBloodCap(s.Option)))
		damage = stacks * s.ValueAt(level)
		if s.Option == 3 {
			crit = min(5, stacks*0.5)
		}
	}
	return min(damage, 100), crit
}

/*
================
wolfBonusDamage
================
*/
func wolfBonusDamage(hit *combat.Result, percent float64) {
	if !hit.Blocked && hit.Damage > 0 && percent > 0 {
		hit.Damage = uint32(min(float64(wire.MaxSkillActionDamage), float64(hit.Damage)*(1+percent/100)))
	}
}

/*
================
castWolfHowl

No target or fear. The normal row timer and native action-speed projection
own the buff; party membership/range is snapshotted. Like Bear Roar, the wolf
stops and the emote (the skill ID) plays the client's pet-howl clip.
================
*/
func (rt *Runtime) castWolfHowl(division string, owner *enterworld.Character, gid uint32, skill companion.SkillDef, now int64) OpResult {
	step, ok := rt.wolfCompanion(division, owner, gid, now)
	if !ok {
		return petTrainingReceipt("Hunting Howl requires a living nearby wolf outside town.")
	}
	mods := companion.ModifiersFor(step.pet)
	level := companion.EffectiveLevel(step.pet, skill, mods)
	if !rt.deps.Update(owner, "wolf-hunting-howl", func() bool {
		live := owner.CompanionByGID(gid)
		if live == nil || !companion.RowReady(&live.Training, skill.Row, now) {
			return false
		}
		companion.StartRow(&live.Training, skill.Row, skill, level, now, mods)
		live.Training.Queued = 0
		return true
	}) {
		return petTrainingReceipt("The howl could not be cast.")
	}
	howl := wolfHowl{until: now + 8000, percent: skill.ValueAt(level), members: make(map[uint32]string)}
	if skill.Option >= 2 {
		howl.members[enterworld.ObjectIDForCharacter(step.snapshot)] = owner.Name
	}
	if skill.Option == 3 {
		from := rt.liveSpawn(simulation.WorldKey(division, owner.Name), step.snapshot, now)
		for gid := range rt.auraParty(division, step.snapshot) {
			member := rt.characterSnapshot(division, rt.findCharacterByGid(division, gid))
			if member == nil || !enterworld.CharacterAlive(member) || !rt.petExpansionAllowed(member, step.pet) || rt.inTown(division, member) {
				continue
			}
			at := rt.liveSpawn(simulation.WorldKey(division, member.Name), member, now)
			if samePlaneAdjacent(from, at) && areaDistance(from, at) <= 20*petAreaPerMeter {
				howl.members[gid] = member.Name
			}
		}
	}
	step.state.wolf.howlUntil, step.state.wolf.howlPercent = howl.until, howl.percent
	step.state.wolf.howlHoldUntil = now + wolfHowlHoldMs
	rt.wolfHowls.Lock()
	if rt.wolfHowls.rows == nil {
		rt.wolfHowls.rows = make(map[petOwnerKey]wolfHowl)
	}
	rt.wolfHowls.rows[step.key] = howl
	rt.wolfHowls.Unlock()
	var public []wire.Frame
	for _, frame := range step.state.follower.Stop(now) {
		public = append(public, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
	}
	public = append(public, rt.wolfHowlFrames(division, howl)...)
	public = append(public, wire.Frame{Opcode: wire.OpActionEmote, Payload: wire.ActionEmotePush{Gid: gid, Action: skill.ID}.Encode()})
	public = append(public, wire.ActionSpeedFrame(gid, cosParameter(step.ref, step.pet, rt.cosAbnormal(division, owner.Name, gid), actionSpeedParameter)/float32(rt.petAttackSpeed(step))))
	return OpResult{Frames: append(append([]wire.Frame{}, public...), rt.petTrainingStateFrames(owner.CompanionByGID(gid), now)...), Broadcast: public}
}

/*
================
wolfHowlWrites

Multiple wolves contribute the strongest living grant, never a product.
No authority reads here: stat projections also run inside character writes.
================
*/
func (rt *Runtime) wolfHowlWrites(division string, c *enterworld.Character) []paramkeeper.Write {
	if !rt.AttackPetExpansion || c == nil || !enterworld.CharacterAlive(c) || rt.inFreeBattle(c) || wearsJobSuit(c, rt.deps.ItemReferences()) || rt.inTown(division, c) || domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance {
		return nil
	}
	now, percent := rt.Now().UnixMilli(), 0.0
	rt.wolfHowls.Lock()
	for key, howl := range rt.wolfHowls.rows {
		if key.division == division && howl.until > now && howl.members[enterworld.ObjectIDForCharacter(c)] == c.Name {
			percent = max(percent, howl.percent)
		}
	}
	rt.wolfHowls.Unlock()
	if percent <= 0 {
		return nil
	}
	return []paramkeeper.Write{{Parameter: actionSpeedParameter, Channel: paramkeeper.FactorProduct, Source: 0x7ffffd00, Value: float32(100 / (1 + percent/100))}}
}

/*
================
wolfHowlFrames
================
*/
func (rt *Runtime) wolfHowlFrames(division string, howl wolfHowl) []wire.Frame {
	var frames []wire.Frame
	for gid, name := range howl.members {
		frames = append(frames, wire.ActionSpeedFrame(gid, rt.EntryActionSpeed(division, name)))
	}
	return frames
}

/*
================
advanceWolfKit

No timers/goroutines: the existing pet tick expires stacks and howl grants.
================
*/
func (rt *Runtime) advanceWolfKit(key petOwnerKey, now int64) []simulation.Frame {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil {
		return nil
	}
	step, eligible := rt.wolfCompanion(key.division, state.character, key.gid, now)
	run := &state.wolf
	if !eligible {
		*run = wolfKitState{}
	} else {
		if s, _, ok := companion.Passive(step.pet, "Blood Hunt", companion.ModifiersFor(step.pet)); !ok || run.huntBook != s.ID || run.huntUntil <= now {
			run.huntHits, run.huntTarget = 0, 0
		} else if mob, ok := rt.Monsters.Get(key.division, run.huntTarget); !ok || mob.CurrentHP == 0 {
			run.huntHits, run.huntTarget = 0, 0
		}
		for actor := range run.blood {
			if s, _, ok := companion.Passive(step.pet, "Bloodscent", companion.ModifiersFor(step.pet)); !ok || run.bloodBook != s.ID || run.bloodUntil[actor] <= now {
				run.blood[actor] = 0
			}
		}
	}
	step.key, step.state = key, state
	rt.refreshWolfBlood(step, eligible)
	rt.wolfHowls.Lock()
	howl, found := rt.wolfHowls.rows[key]
	if found && (!eligible || howl.until <= now) {
		delete(rt.wolfHowls.rows, key)
	} else {
		found = false
	}
	rt.wolfHowls.Unlock()
	if !found {
		return nil
	}
	run.howlUntil, run.howlPercent = 0, 0
	frames := rt.wolfHowlFrames(key.division, howl)
	if eligible {
		frames = append(frames, wire.ActionSpeedFrame(key.gid, cosParameter(step.ref, step.pet, rt.cosAbnormal(key.division, key.name, key.gid), actionSpeedParameter)/float32(rt.petAttackSpeed(step))))
	}
	// The simulation route includes the recipient itself and its own viewers.
	for _, frame := range frames {
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: key.division, SourceGID: binary.LittleEndian.Uint32(frame.Payload), ExceptCharacterID: state.character.ID, Frames: simFrames([]wire.Frame{frame})}})
	}
	return simFrames(frames)
}

/*
================
wolfHowlActionMs

Port-only animation speed must also shorten the server's attack bracket.
Preserve preparation, projectile flight, native reuse tails and buff durations.
================
*/
func (rt *Runtime) wolfHowlActionMs(division string, c *enterworld.Character, ms uint32) int64 {
	writes := rt.wolfHowlWrites(division, c)
	if len(writes) == 0 {
		return int64(ms)
	}
	return int64(float64(ms) * float64(writes[0].Value) / 100)
}
