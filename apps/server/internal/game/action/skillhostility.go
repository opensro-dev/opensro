/*
===========================================================================

skillhostility.go - committed aggression and monster opponent selection

Damage credit and aggression are independent. Linked threat moves aggression
to its source without changing HP, reward attribution or transmitted damage.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
commitSkillHostility

Each impact contributes to the cumulative aggression (5903EC), while damage
remains separate. Fatal results no longer own a live opponent ledger, but
their damage still feeds the attacker's Mana Switch links (linkedmana.go),
so the area owners call this for a killed victim too.
================
*/
func (rt *Runtime) commitSkillHostility(division string, attacker, target uint32, skill enterworld.SkillRow, impacts []simulation.MonsterDamageResult, now int64) {
	rt.commitLinkedMana(division, attacker, impacts, now)
	if len(impacts) == 0 || impacts[len(impacts)-1].Fatal {
		return
	}
	var damage, aggression uint32
	for _, impact := range impacts {
		damage += impact.Applied
		aggression = combat.AccumulateThreat(aggression, impact.Applied, skill.Threat)
	}
	rt.commitAggression(division, target, simulation.HostilityEvent{Attacker: attacker, Damage: damage, Aggression: int32(aggression)}, now)
}

/*
================
commitAggression

Both damaging hits and taunts share link transfer and live target resolution.
================
*/
func (rt *Runtime) commitAggression(division string, target uint32, event simulation.HostilityEvent, now int64) {
	if event.Damage == 0 && event.Aggression == 0 {
		return
	}
	rt.dispatchAggression(division, target, event, now)
}

/*
================
dispatchAggression

The healing caller dispatches even when halving a nonzero heal yields zero
threat (5A0752..5A07A6). Keep that event and any linked source in the ledger.
================
*/
func (rt *Runtime) dispatchAggression(division string, target uint32, event simulation.HostilityEvent, now int64) {
	attacker, damage, aggression := event.Attacker, event.Damage, uint32(event.Aggression)
	var events []simulation.HostilityEvent
	if c := rt.findCharacterByGid(division, attacker); c != nil {
		if link, ok := rt.effects.ThreatLink(division, c.Name, now); ok {
			// Native dispatches the linked source event before the original
			// attacker's event, including a zero transfer. Never transfer HP.
			if rt.findCharacterByGid(division, link.SourceGID) != nil {
				var transferred uint32
				aggression, transferred = combat.SplitLinkedThreat(aggression, link.ThreatPercent)
				events = append(events, simulation.HostilityEvent{Attacker: link.SourceGID, Aggression: int32(transferred)})
			}
		}
	}
	events = append(events, simulation.HostilityEvent{Attacker: attacker, Damage: damage, Aggression: int32(aggression)})
	rt.recordSkillHostility(division, target, events, now)
}

/*
================
recordSkillHostility

Resolve live eligible opponents before the population owner updates its ledger.
================
*/
func (rt *Runtime) recordSkillHostility(division string, target uint32, events []simulation.HostilityEvent, now int64) {
	instance, ok := rt.Monsters.Get(division, target)
	if !ok {
		return
	}
	mover, ok := rt.Monsters.Mover(division, target)
	if !ok {
		return
	}
	pose := mover.LivePoseAt(now, nil)
	from := simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
	candidates := make(map[uint32]monster.OpponentCandidate)
	gids := []uint32{instance.Opponents[0].GID, instance.Opponents[1].GID}
	for _, event := range events {
		gids = append(gids, event.Attacker)
	}
	for _, gid := range gids {
		character := rt.findCharacterByGid(division, gid)
		if character == nil {
			// A monster in a Temptation fight (temptation.go).
			if candidate, ok := rt.temptedOpponentCandidate(division, instance, gid, from, now); ok {
				candidates[gid] = candidate
			}
			continue
		}
		snapshot := rt.characterSnapshot(division, character)
		if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) || !monster.AllowsTargetStatus(instance.Ref.TidWord, instance.Nest.NativeTacticsFlags, snapshot.NativeBodyStatus) {
			continue
		}
		to := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
		candidates[gid] = monster.OpponentCandidate{GID: gid, Eligible: true, Distance: simulation.WorldDistance2D(from, to), ActorDistance: monster.NativeActorDistance(pose, monster.Pose{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z})}
	}
	rt.Monsters.RecordHostilitySequence(division, target, events, candidates, now)
}
