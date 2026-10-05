/*
===========================================================================

skillhawk.go - the attacking hawk of Black and Light Hawk Summon

A summ buff (enterworld.SkillSummonedHawk) keeps a hawk beside its caster.
Native shape, v1.188 SR_GameServer:

	SkillCombat_EngageSkill 593540   the buff's engage allocates the hawk's
	                                 periodic-damage record (+0xC10)
	autocommand 4ADD44 / 4AEB6E      every basic attack and every attack
	                                 skill the caster starts stamps the
	                                 record: its target, and the time
	Skill_ProcessPeriodicDamage      once per interval, while the caster
	582750                           attacked within the last interval, the
	                                 hawk strikes that target (30D1, the
	                                 v1.150 client's 357A)

The record lives while the buff does; this module keeps one per caster and
drops it when its effect instance is gone. Inferred: the port strikes only
monsters, the only targets the action lane's damage door commits for a
player's skills.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
summonedHawk

One caster's periodic-damage record (the esi of 582750): the effect
instance that owns it, its target (+0x10), the caster's last attack (+0x8)
and the hawk's last strike (+0xC).
================
*/
type summonedHawk struct {
	division, owner string
	token, skill    uint32
	target          uint32
	ownerAttackAt   int64
	strikeAt        int64
}

/*
================
hawkKey
================
*/
func hawkKey(division, name string) string {
	return division + "\x00" + name
}

/*
================
activeHawk

The caster's live summ effect instance, if any.
================
*/
func (rt *Runtime) activeHawk(division, name string) (uint32, enterworld.SkillRow, bool) {
	skills := rt.deps.SkillData()
	if rt.effects == nil || skills == nil {
		return 0, enterworld.SkillRow{}, false
	}
	for _, e := range rt.effects.Snapshot(division, name) {
		if e.StopRequested {
			continue
		}
		if row, ok := skills.SkillByID(e.SkillID); ok && row.TimedEffect.Hawk.Present {
			return e.InstanceToken, row, true
		}
	}
	return 0, enterworld.SkillRow{}, false
}

/*
================
noteHawkOwnerAttack

4ADD44 (basic attack) and 4AEB6E (a skill with an attack block): the
caster's attack re-aims its hawk and restarts the window it may strike in.
================
*/
func (rt *Runtime) noteHawkOwnerAttack(division string, c *enterworld.Character, target uint32, now int64) {
	if c == nil || target == 0 {
		return
	}
	token, row, ok := rt.activeHawk(division, c.Name)
	key := hawkKey(division, c.Name)
	rt.hawkMu.Lock()
	defer rt.hawkMu.Unlock()
	if !ok {
		delete(rt.hawks, key)
		return
	}
	if rt.hawks == nil {
		rt.hawks = map[string]*summonedHawk{}
	}
	hawk := rt.hawks[key]
	if hawk == nil || hawk.token != token {
		hawk = &summonedHawk{division: division, owner: c.Name, token: token, skill: row.ID}
		rt.hawks[key] = hawk
	}
	hawk.target, hawk.ownerAttackAt = target, now
}

/*
================
advanceHawks

582750 for every caster with a hawk, after the persistent casts it rides
with (5830B0 calls it).
================
*/
func (rt *Runtime) advanceHawks(now int64) []simulation.DivisionFrames {
	rt.hawkMu.Lock()
	hawks := make([]summonedHawk, 0, len(rt.hawks))
	for _, h := range rt.hawks {
		hawks = append(hawks, *h)
	}
	rt.hawkMu.Unlock()
	var out []simulation.DivisionFrames
	for _, h := range hawks {
		out = append(out, rt.advanceHawk(h, now)...)
	}
	return out
}

/*
================
advanceHawk

One record's turn: a lost instance drops it, a dead or missing target
clears its aim, and a strike needs a full interval since the last one and
a caster attack within the last interval.
================
*/
func (rt *Runtime) advanceHawk(h summonedHawk, now int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(h.division)
	defer unlock()
	key := hawkKey(h.division, h.owner)
	c := rt.findCharacter(h.division, h.owner)
	token, row, ok := rt.activeHawk(h.division, h.owner)
	if c == nil || !ok || token != h.token {
		rt.dropHawk(key, h.token)
		return nil
	}
	snapshot := rt.characterSnapshot(h.division, c)
	if snapshot == nil || !enterworld.CharacterAlive(snapshot) || h.target == 0 {
		return nil
	}
	target, exists := rt.resolveCombatTarget(h.division, snapshot, h.target, now)
	if !exists {
		rt.updateHawk(key, h.token, func(live *summonedHawk) { live.target = 0 })
		return nil
	}
	interval := int64(row.TimedEffect.Hawk.IntervalMs)
	if now-h.strikeAt < interval || now-h.ownerAttackAt > interval {
		return nil
	}
	rt.updateHawk(key, h.token, func(live *summonedHawk) { live.strikeAt = now })
	result := rt.hawkStrike(h, c, snapshot, row, target, now)
	if len(result.Broadcast) == 0 {
		return nil
	}
	out := []simulation.DivisionFrames{hawkDivisionFrames(h.division, enterworld.ObjectIDForCharacter(c), 0, result.Broadcast)}
	if len(result.ActorPrivate) != 0 {
		out = append(out, hawkDivisionFrames(h.division, 0, c.ID, result.ActorPrivate))
	}
	return append(out, recipientDivisionFrames(h.division, result.Recipients)...)
}

/*
================
hawkDivisionFrames

A strike's public frames go to the sessions around the caster; its
progression frames to the caster alone.
================
*/
func hawkDivisionFrames(division string, source uint32, only int64, frames []wire.Frame) simulation.DivisionFrames {
	batch := simulation.DivisionFrames{DivisionID: division, SourceGID: source, OnlyCharacterID: only}
	for _, f := range frames {
		batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
	}
	return batch
}

/*
================
hawkStrike

The strike of 582750: 40F1B0 + 40F3D0 on the target, committed through the
target's HP door with the caster credited (vtable +0x4FC), then the fatal
bit when the target died. A strike never misses. A player target takes it
as the caster's credited hit (pvpstrike.go).
================
*/
func (rt *Runtime) hawkStrike(h summonedHawk, c, snapshot *enterworld.Character, row enterworld.SkillRow, victim combatTarget, now int64) OpResult {
	var defender combat.Stats
	var err error
	if victim.monster != nil {
		defender, err = combat.MonsterInstanceStats(*victim.monster)
	} else {
		defender, _, err = rt.playerCombatStats(h.division, victim.snapshot)
	}
	if err != nil {
		return OpResult{}
	}
	rank := combat.SkillMasteryRank(snapshot.Masteries, row.Attack)
	damage, err := combat.HawkDamage(defender, row.TimedEffect.Hawk.Physical, row.TimedEffect.Hawk.Magical, rank, rt.CombatRoll)
	if err != nil {
		return OpResult{}
	}
	formula := combat.Result{Damage: uint32(damage)}
	if victim.player != nil {
		attacker, _, err := rt.playerCombatStats(h.division, snapshot)
		if err != nil {
			return OpResult{}
		}
		hit, result, landed := rt.creditPlayerHit(playerHitInput{division: h.division, caster: c, snapshot: snapshot,
			attacker: attacker, skill: row, target: victim, impacts: 1, fixed: &formula, now: now})
		if !landed {
			return OpResult{}
		}
		word := damage
		if hit.struck.fatal {
			word |= wire.HawkFatalBit
		}
		result.Broadcast = append([]wire.Frame{wire.HawkStrikeFrame(h.token, victim.gid, word)}, result.Broadcast...)
		return result
	}
	target := *victim.monster
	hit, ok := rt.commitCreditedMonsterHit(h.division, c, snapshot, row, target, formula, "hawk-kill", now)
	if !ok {
		return OpResult{}
	}
	word := damage
	if hit.impacts[0].Fatal {
		word |= wire.HawkFatalBit
	}
	return rt.creditedHitResult(h.division, target, hit, []wire.Frame{wire.HawkStrikeFrame(h.token, target.Gid, word)}, now)
}

/*
================
updateHawk

Mutates the caster's record when it is still the instance h read.
================
*/
func (rt *Runtime) updateHawk(key string, token uint32, change func(*summonedHawk)) {
	rt.hawkMu.Lock()
	defer rt.hawkMu.Unlock()
	if live := rt.hawks[key]; live != nil && live.token == token {
		change(live)
	}
}

/*
================
dropHawk
================
*/
func (rt *Runtime) dropHawk(key string, token uint32) {
	rt.hawkMu.Lock()
	defer rt.hawkMu.Unlock()
	if live := rt.hawks[key]; live != nil && live.token == token {
		delete(rt.hawks, key)
	}
}
