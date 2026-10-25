/*
===========================================================================

flagwar_rebirth.go - a participant's camp and its reconstructed arena scene

The match owns camp selection; rebirth.go owns the prepared revival. Camp
revival keeps the admitted layer and the return anchor. The full re-entry
reconstructs the match-owned objects and HUD after the ordinary actor entry.

The camp rule and geometry retain the flag-war audit's evidence (#887,
#1385). The reference-server assembly review on #1616 confirms that the
event's due return entry revives a dead participant with maximum HP/MP,
revival grace and a teleport to the camp. It does not revive participants
when the event ends. The v1.150 client uses the ordinary rebirth choices.

Port choice: the due entry's insertion site and delay are unpinned, so the
player's rebirth request triggers the camp revival. The existing revival
owner supplies the full keeper vitals. Native also applies heal-reduction
parameters; this path retains the ordinary revival owner's unreduced
recovery until that shared heal behavior is ported.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
flagWarResidentMatch

The caller holds the division door. The book alone is not membership:
reconciliation can lag a departure, and a reused layer has a new lease.
================
*/
func (rt *Runtime) flagWarResidentMatch(division string, character *enterworld.Character) *flagWarMatch {
	if character == nil || rt.Monsters == nil {
		return nil
	}
	match := rt.flagWarMatchFor(division)
	if match == nil {
		return nil
	}
	match.mu.Lock()
	lease := match.lease
	p := match.participants[character.ID]
	member := p != nil && p.entered && p.team >= 0 && p.team < len(flagWarTeamSpawns) &&
		p.registrant.Name == character.Name && p.registrant.Division == division
	match.mu.Unlock()
	if !member || instance.ID(domain.CharacterWorldInstance(character)) != lease.ID {
		return nil
	}
	admitted, ok := rt.EntryPopulationLease(division, character.Name)
	if !ok || admitted != lease {
		return nil
	}
	return match
}

/*
================
campRebirthPoint

Both rebirth choices name the participant's camp while the match runs.
Inference: once its clock has ended, ordinary town revival applies; the
closed event cannot become active again through a delayed rebirth choice.
The caller holds the division door; this method only reads the match book.
================
*/
func (m *flagWarMatch) campRebirthPoint(characterID, nowMs int64) (travelPoint, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.participants[characterID]
	if p == nil || !p.entered || p.team < 0 || p.team >= len(flagWarTeamSpawns) ||
		m.closeMs != 0 || nowMs >= m.endsMs {
		return travelPoint{}, false
	}
	return travelPoint{spawn: flagWarTeamSpawns[p.team], world: m.lease.ID}, true
}

/*
================
flagWarSceneFrames

The match lock is held. Initial entry and camp re-entry share the same
current scene. ResetClient clears the client team's timer and carrier as
well as its actors, so replay only to this participant after its bootstrap.
MissionStart's twenty-minute clock is immediately corrected to the elapsed
match time. No global game-start notices, rewards or channel actions replay.
================
*/
func (rt *Runtime) flagWarSceneFrames(match *flagWarMatch, p *flagWarParticipant, nowMs int64) []wire.Frame {
	if p == nil || !p.entered {
		return nil
	}
	elapsed := nowMs - match.startMs
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > flagWarMatchDurationMs {
		elapsed = flagWarMatchDurationMs
	}
	frames := []wire.Frame{
		{Opcode: wire.OpFlagWarEvent, Payload: wire.EncodeFlagWarMissionStart(uint16(p.team))},
		{Opcode: wire.OpFlagWarEvent, Payload: wire.EncodeFlagWarElapsed(uint32(elapsed))},
	}
	for _, object := range match.sortedObjects() {
		if object.live {
			frames = append(frames, wire.Frame{Opcode: wire.OpObjectListChunk, Payload: rt.flagWarObjectRow(object)})
		}
	}
	if carrier := match.participants[match.carrierID]; carrier != nil && carrier.entered {
		frames = append(frames, wire.Frame{Opcode: wire.OpFlagWarEvent, Payload: wire.EncodeFlagWarCarrier(carrier.registrant.Gid)})
	}
	if match.closeMs != 0 {
		frames = append(frames, wire.Frame{Opcode: wire.OpFlagWarEvent, Payload: wire.EncodeFlagWarEventType(wire.FlagWarTimerStop)})
	}
	return frames
}
