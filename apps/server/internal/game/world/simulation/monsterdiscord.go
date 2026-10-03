/*
===========================================================================

monsterdiscord.go - lowering a monster's hostility toward its target

The Bard's Discord Wave (dtnt) "removes Monsters' hostility toward their
target". The population owner cuts the aggression the monster's opponent
ledger holds for the target it currently fights; when none is left, the
record is forgotten and the monster's next planner step abandons that
target, as Fear abandons its caster (applyTacticsEvents).

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/monster"

const (
	// aiEventStart is the tactics event the abnormal callbacks post when
	// a status starts (4A4BD0 / 4A4F70: 0x14).
	aiEventStart = 0x14

	// aiEventKindDiscord marks a target released by a hostility cut.
	// Inferred: no native event for dtnt is known; this port-only kind
	// rides the same queue as Fear's (kind 9), whose BATTLE handling
	// (55A390) already abandons exactly the target the event names.
	aiEventKindDiscord = 0x80

	// fullHostilityPercent is dtnt's percent word at 100: all of it.
	fullHostilityPercent = 100
)

/*
================
ReduceTargetHostility

Cut the aggression gid holds toward its current target by flat plus
percent of that aggression, clamped at zero. Reports whether the
monster lost its hostility: then the record is forgotten and the target
abandoned on the next planner step. A target acquired by sight alone has
no record, hence no hostility to keep. A monster that fights nobody is
untouched.
================
*/
func (s *MonsterState) ReduceTargetHostility(division string, gid, flat, percent uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.CurrentHP == 0 {
		return false
	}
	mover, ok := state.movers.lookup(gid)
	if !ok || mover.TargetGID() == 0 {
		return false
	}
	target := mover.TargetGID()
	percent = min(percent, fullHostilityPercent)
	for i := range instance.Opponents {
		record := &instance.Opponents[i]
		if record.GID != target {
			continue
		}
		cut := int64(flat) + int64(record.Aggression)*int64(percent)/fullHostilityPercent
		record.Aggression = int32(max(0, int64(record.Aggression)-cut))
		if record.Aggression > 0 {
			state.instances.set(gid, instance)
			return false
		}
		// The second record moves up, as 548090 does when the first one
		// can no longer be chosen.
		if i == 0 {
			instance.Opponents[0] = instance.Opponents[1]
		}
		instance.Opponents[1] = monster.Opponent{}
		break
	}
	state.instances.set(gid, instance)
	state.queueAIEvents(gid, []monsterAIEvent{{Event: aiEventStart, Kind: aiEventKindDiscord, Source: target}})
	return true
}
