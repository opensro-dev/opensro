/*
===========================================================================

styria_status.go - Styria Clash: the status snapshot's server half

Port-only, not native (epic #3648, slice 5). The registration,
score and result windows on the client read one snapshot
(wire.EncodeStyriaStatus, 0x3FC8); this file builds it per viewer
and keeps the viewers told: entered participants and the division's
applicants, pushed on the clash tick when their bytes change, plus
one snapshot riding the arena manager's conversation so the
registration window opens current. The clocks are absolute
instants - the client counts down locally, so a quiet standing
sends nothing. With SRO_STYRIA_CLASH off none of this runs: no
snapshot is built, pushed or appended.

===========================================================================
*/

package action

import (
	"bytes"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
styriaStatusFor

One viewer's snapshot. An entered participant reads the match book
(its score, its clock, and at the end their own reward figures -
the same computation that pays them, styria_reward.go); everyone
else reads the registration book and the schedule: their applied
side when they hold one, the division's applications per side, and
the next start with its application window.
================
*/
func (rt *Runtime) styriaStatusFor(division string, character *enterworld.Character, nowMs int64) wire.StyriaStatus {
	status := wire.StyriaStatus{
		Phase: wire.StyriaPhaseRegistration, OwnSide: -1, ServerNowMs: nowMs,
	}
	if rt.styria == nil || character == nil {
		return status
	}
	if match := rt.styriaMatchFor(division); match != nil {
		match.mu.Lock()
		participant := match.participants[character.ID]
		if participant != nil && participant.entered {
			status.Phase = wire.StyriaPhaseMatch
			status.PhaseEndsMs = match.endsMs
			status.MatchSeq = match.seq
			status.OwnState = wire.StyriaStateEntered
			status.OwnSide = participant.registrant.Side
			status.ScoreHunters, status.ScoreThieves = match.score[styriaSideHunters], match.score[styriaSideThieves]
			status.Outcome = match.outcome
			for _, id := range match.order {
				if p := match.participants[id]; p != nil && p.entered {
					if p.registrant.Side == styriaSideThieves {
						status.Thieves++
					} else {
						status.Hunters++
					}
				}
			}
			if match.closeMs != 0 {
				status.Phase = wire.StyriaPhaseResult
				status.PhaseEndsMs = match.closeMs
				status.JobExp, status.SkillExp, _ = styriaRewardFor(
					styriaResultFor(match.outcome, participant.registrant.Side),
					int(rewardLevel(character)))
			}
			match.mu.Unlock()
			return status
		}
		match.mu.Unlock()
	}

	rt.styria.mu.Lock()
	status.MatchSeq = rt.styria.matchSeq
	for _, registrant := range rt.styria.registrants {
		if registrant.Division != division {
			continue
		}
		if registrant.Side == styriaSideThieves {
			status.Thieves++
		} else {
			status.Hunters++
		}
	}
	if registrant, applied := rt.styria.registrants[styriaKey(division, character.Name)]; applied {
		status.OwnState = wire.StyriaStateApplied
		status.OwnSide = registrant.Side
	}
	rt.styria.mu.Unlock()

	settings := rt.styriaSettings()
	for _, start := range settings.nearStarts(time.UnixMilli(nowMs)) {
		if start.UnixMilli() > nowMs {
			status.NextStartMs = start.UnixMilli()
			break
		}
	}
	if status.NextStartMs != 0 {
		status.WindowOpensMs = status.NextStartMs - int64(styriaParticipationMinutes)*60*1000
	}
	return status
}

/*
================
styriaStatusFrame

The talker's snapshot as a frame: the arena manager's conversation
carries it, so the registration window opens current. The bytes
are recorded, so the tick's fan-out does not repeat them.
================
*/
func (rt *Runtime) styriaStatusFrame(division string, character *enterworld.Character) (wire.Frame, bool) {
	if rt.styria == nil || character == nil {
		return wire.Frame{}, false
	}
	payload := wire.EncodeStyriaStatus(rt.styriaStatusFor(division, character, rt.Now().UnixMilli()))
	rt.styria.mu.Lock()
	rt.styria.statusSent[styriaKey(division, character.Name)] = payload
	rt.styria.mu.Unlock()
	return wire.Frame{Opcode: wire.OpStyriaStatus, Payload: payload}, true
}

/*
================
styriaStandingChanged

Whether a fresh snapshot says anything the pushed one did not:
every byte but the trailing build instant (EncodeStyriaStatus
keeps it last), so a standing whose only news is the clock stays
silent.
================
*/
func styriaStandingChanged(sent, payload []byte) bool {
	if len(sent) != len(payload) || len(payload) < 8 {
		return true
	}
	return !bytes.Equal(sent[:len(sent)-8], payload[:len(payload)-8])
}

/*
================
styriaPushStatuses

The status fan-out, on the clash tick: entered participants and
the division's applicants are pushed a fresh snapshot when theirs
changes. A viewer who left both audiences (the match book dropped,
the application consumed) is forgotten - the client holds the last
result snapshot itself until the player dismisses it.
================
*/
func (rt *Runtime) styriaPushStatuses(nowMs int64) {
	if rt.styria == nil || rt.PushCharacterFrames == nil {
		return
	}
	type viewer struct{ division, name string }
	audience := make(map[string]viewer)

	rt.styria.mu.Lock()
	for _, registrant := range rt.styria.registrants {
		audience[styriaKey(registrant.Division, registrant.Name)] =
			viewer{registrant.Division, registrant.Name}
	}
	matches := make([]*styriaMatch, 0, len(rt.styria.matches))
	for _, match := range rt.styria.matches {
		if match != nil {
			matches = append(matches, match)
		}
	}
	rt.styria.mu.Unlock()

	for _, match := range matches {
		match.mu.Lock()
		for _, id := range match.order {
			if p := match.participants[id]; p != nil && p.entered {
				audience[styriaKey(match.division, p.registrant.Name)] =
					viewer{match.division, p.registrant.Name}
			}
		}
		match.mu.Unlock()
	}

	rt.styria.mu.Lock()
	for key := range rt.styria.statusSent {
		if _, live := audience[key]; !live {
			delete(rt.styria.statusSent, key)
		}
	}
	rt.styria.mu.Unlock()

	for key, who := range audience {
		character := rt.findCharacter(who.division, who.name)
		if character == nil {
			rt.styria.mu.Lock()
			delete(rt.styria.statusSent, key)
			rt.styria.mu.Unlock()
			continue
		}
		payload := wire.EncodeStyriaStatus(rt.styriaStatusFor(who.division, character, nowMs))
		rt.styria.mu.Lock()
		sent := rt.styria.statusSent[key]
		changed := styriaStandingChanged(sent, payload)
		if changed {
			rt.styria.statusSent[key] = payload
		}
		rt.styria.mu.Unlock()
		if !changed {
			continue
		}
		rt.PushCharacterFrames(who.division, who.name,
			[]wire.Frame{{Opcode: wire.OpStyriaStatus, Payload: payload}})
	}
}
