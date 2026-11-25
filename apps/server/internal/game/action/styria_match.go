/*
===========================================================================

styria_match.go - the Styria Clash match owner

One match per division (the arena world's single authored layer):
the entered Hunters and Thieves, the match clock, and the world's
end. The instance lane (instance_world.go) owns the layer, the
transfers and the returns; this file owns who is in the match and
when it stops being one.

PORT INFERENCES, recorded where the sources are silent: no match
length is authored in any table here (the later server's sits in
its schedule and world rows beyond reach), so the clock is a named
port constant the scoring slice can retune; a side that empties
does not end the match early in this slice - the clock does, and
the scoring slice owns win-by-attrition. Death returns the fallen
to their entry anchor at once (the epic's rule: return on the end,
death or logout), it does not revive them at the gate.

===========================================================================
*/

package action

import (
	"sort"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/styria"
)

// Match pacing (PORT CHOICES, see the file header).
const (
	styriaMatchDurationMs int64 = 10 * 60 * 1000
	styriaCloseAfterEndMs int64 = 60 * 1000
	styriaDeadlineSlackMs int64 = 5 * 60 * 1000
)

// The match outcome, decided on the score when the clock ends and
// read by the reward slice (#3648 slice 4). While the match runs
// it is styriaOutcomeOpen.
const (
	styriaOutcomeOpen = iota
	styriaOutcomeHunters
	styriaOutcomeThieves
	styriaOutcomeDraw
)

/*
================
styriaParticipant

One entered player: their application and whether they still stand
in the match.
================
*/
type styriaParticipant struct {
	registrant StyriaRegistrant
	entered    bool
}

/*
================
styriaMatch

The live match in one division. mu guards every field below: the
tick and the reconciliation reach the book on different locks of
their own. Lock order is the division lane first, then mu (the
lanes that already hold the division lock may take mu); mu is never
held across a call that takes the division lane (transfers, returns
and world closes all run after it is released).
================
*/
type styriaMatch struct {
	mu           sync.Mutex
	division     string
	lease        instance.Lease
	seq          uint32 // the event's match number (styria_status.go)
	startMs      int64
	endsMs       int64
	closeMs      int64 // 0 until the match clock ends
	participants map[int64]*styriaParticipant
	order        []int64 // participant ids in entry order (deterministic fan-out)

	// The score (styria_score.go): points per side in styriaSide*
	// order, the clock's verdict once decided, and each treasure
	// box's next-open instant (0 = full). All under mu, like the rest.
	score   [2]int
	outcome int
	boxes   []int64

	// The rewards owed (styria_reward.go): collected under mu when
	// the outcome lands, paid by the match tick's drain with no
	// lane held.
	pendingRewards []styriaDue

	// Each side's next blessing (styria_buffs.go): the family's
	// grant cursor, advanced by that side's box openings.
	blessings [2]int
}

/*
================
styriaGateSpawn

The entry landing for one side, from the arena's authored gates
(styria.HunterGate / styria.ThiefGate). The height resolves on
entry like every outdoor landing.
================
*/
func styriaGateSpawn(side int) simulation.Spawn {
	gate := styria.HunterGate
	if side == styriaSideThieves {
		gate = styria.ThiefGate
	}
	return simulation.Spawn{RegionID: gate.RegionID, X: gate.X, Z: gate.Z}
}

/*
================
styriaCanEnter

The entry admission, re-run at transfer time on the character
record: alive, standing in the open world with no other programme's
return anchor, still wearing the suit of the side they applied for,
and still partyless (a party forms only after entering).
================
*/
func (rt *Runtime) styriaCanEnter(division string, character *enterworld.Character, side int) bool {
	if character == nil || !enterworld.CharacterAlive(character) {
		return false
	}
	if domain.CharacterWorldInstance(character) != domain.DefaultWorldInstance {
		return false
	}
	if character.World != nil && character.World.InstanceReturn != nil {
		return false
	}
	if styriaSideOf(enterworld.DressedJob(character)) != side {
		return false
	}
	return rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(character)) == nil
}

/*
================
styriaStartMatch

One division's match start: the instance open, the native entry
admission re-run on each entrant, the transfers to the side gates.
An entrant whose state changed since applying (suit off, party
joined, already inside a programme, logged out) simply has no seat;
if nobody arrives the layer goes straight back.
================
*/
func (rt *Runtime) styriaStartMatch(division string, entrants []StyriaRegistrant, nowMs int64) {
	if rt.Monsters == nil || rt.styria == nil {
		return
	}
	lease, status := rt.OpenInstanceWorld(InstanceOpenRequest{
		Division:   division,
		Definition: instance.DefinitionID(styria.WorldID),
		DeadlineMs: nowMs + styriaMatchDurationMs + styriaDeadlineSlackMs,
	})
	if status != instance.Success {
		return
	}

	match := &styriaMatch{
		division:     division,
		lease:        lease,
		startMs:      nowMs,
		endsMs:       nowMs + styriaMatchDurationMs,
		participants: make(map[int64]*styriaParticipant),
		boxes:        make([]int64, len(styria.BoxSpots)),
	}
	for _, registrant := range entrants {
		participant := &styriaParticipant{registrant: registrant}
		unlock := rt.lockDivision(division)
		character := rt.findCharacter(division, registrant.Name)
		admitted := false
		if character != nil {
			rt.deps.Read(division, func() {
				admitted = rt.styriaCanEnter(division, character, registrant.Side)
			})
		}
		unlock()
		if character == nil || !admitted {
			continue // applied, then left or changed state: no seat
		}
		if rt.TransferCharacterToInstance(division, character, lease, styriaGateSpawn(registrant.Side)) != instance.Success {
			continue
		}
		participant.entered = true
		match.participants[registrant.CharacterID] = participant
		match.order = append(match.order, registrant.CharacterID)
	}
	if len(match.order) == 0 {
		// Nobody arrived: give the layer back and forget the match.
		rt.CloseInstanceWorld(division, lease)
		return
	}

	// Publish last: until this store no other lane can see the match,
	// so the build above needs no lock of its own. The match takes
	// the event's next number as it publishes (styria_status.go).
	rt.styria.mu.Lock()
	rt.styria.matchSeq++
	match.seq = rt.styria.matchSeq
	rt.styria.matches[division] = match
	rt.styria.mu.Unlock()
}

/*
================
advanceStyriaMatches

The match tick step: reconciliation, the ending, the close. Runs
after the scheduler half of advanceStyriaClash.
================
*/
func (rt *Runtime) advanceStyriaMatches(nowMs int64) []simulation.DivisionFrames {
	rt.styria.mu.Lock()
	divisions := make([]string, 0, len(rt.styria.matches))
	for division := range rt.styria.matches {
		divisions = append(divisions, division)
	}
	sort.Strings(divisions)
	matches := make([]*styriaMatch, 0, len(divisions))
	for _, division := range divisions {
		if match := rt.styria.matches[division]; match != nil {
			matches = append(matches, match)
		}
	}
	rt.styria.mu.Unlock()

	var frames []simulation.DivisionFrames
	for _, match := range matches {
		frames = append(frames, rt.styriaTickMatch(match, nowMs)...)

		// Rewards pay here, after the match lock is released, in
		// the flag war's drain shape: the grant lane takes the
		// division lock itself.
		match.mu.Lock()
		dues := match.pendingRewards
		match.pendingRewards = nil
		match.mu.Unlock()
		for _, due := range dues {
			rt.styriaPayReward(match, due)
		}

		match.mu.Lock()
		closeNow := match.closeMs != 0 && nowMs >= match.closeMs
		match.mu.Unlock()
		if !closeNow {
			continue
		}
		rt.CloseInstanceWorld(match.division, match.lease)
		// A fallible evacuation leaves the layer retiring. Keep the
		// match book until the instance owner has actually released
		// that lease, so a second close never aims at a new world.
		if live, open := rt.Monsters.PopulationLease(match.division, match.lease.ID); open && live == match.lease {
			continue
		}
		rt.styria.mu.Lock()
		if rt.styria.matches[match.division] == match {
			delete(rt.styria.matches, match.division)
		}
		rt.styria.mu.Unlock()
	}
	return frames
}

/*
================
styriaTickMatch

One match, one tick: reconciliation first, then the clock. When the
clock ends the match the outcome is decided on the score and told
to every entered participant (styriaEndFrames); the close itself
(the world closes styriaCloseAfterEndMs later) evacuates everyone
onto their entry anchors.
================
*/
func (rt *Runtime) styriaTickMatch(match *styriaMatch, nowMs int64) []simulation.DivisionFrames {
	rt.styriaReconcile(match)

	match.mu.Lock()
	ended := false
	if match.closeMs == 0 && nowMs >= match.endsMs {
		match.closeMs = nowMs + styriaCloseAfterEndMs
		match.outcome = styriaVerdict(match.score)
		ended = true
		// Everyone still standing in the match is owed its reward
		// (styria_reward.go); the drain pays it off this lock.
		for _, id := range match.order {
			if p := match.participants[id]; p != nil && p.entered {
				match.pendingRewards = append(match.pendingRewards,
					styriaDue{name: p.registrant.Name, side: p.registrant.Side})
			}
		}
	}
	match.mu.Unlock()
	if !ended {
		return nil
	}
	// The clock ends every blessing with the match: the holders
	// still stand in the arena until the close evacuates them, but
	// the blessing is the match's, not theirs to carry out.
	match.mu.Lock()
	standing := make([]*styriaParticipant, 0, len(match.order))
	for _, id := range match.order {
		if p := match.participants[id]; p != nil && p.entered {
			standing = append(standing, p)
		}
	}
	match.mu.Unlock()
	for _, p := range standing {
		unlock := rt.lockDivision(match.division)
		character := rt.findCharacter(match.division, p.registrant.Name)
		unlock()
		rt.styriaRetireBlessings(match.division, character, nowMs)
	}
	return styriaEndFrames(match)
}

/*
================
styriaVerdict

The clock's verdict on a score: the side holding more wins, an
equal score draws - the arena manager's own rule.
================
*/
func styriaVerdict(score [2]int) int {
	switch {
	case score[styriaSideHunters] > score[styriaSideThieves]:
		return styriaOutcomeHunters
	case score[styriaSideThieves] > score[styriaSideHunters]:
		return styriaOutcomeThieves
	}
	return styriaOutcomeDraw
}

/*
================
styriaReconcile

Entered participants who left the arena world (logged out, rebirthed
to town, walked out) leave the match: their return anchor is cleared
so the closing world cannot pull them back from wherever they now
play. A participant found dead inside is returned to their anchor
at once - death ends a Styria seat - and then leaves the same way.

Phase 1 reads world truth under the division lane with the match
lock dropped (lock order: lane, then match). Phase 2 applies the
departures to the book. The dead are returned after both, on the
instance lane's own lock.
================
*/
func (rt *Runtime) styriaReconcile(match *styriaMatch) {
	match.mu.Lock()
	entered := make([]*styriaParticipant, 0, len(match.order))
	for _, id := range match.order {
		if p := match.participants[id]; p != nil && p.entered {
			entered = append(entered, p)
		}
	}
	match.mu.Unlock()

	type sighting struct {
		p         *styriaParticipant
		character *enterworld.Character
		gone      bool
		dead      bool
	}
	sightings := make([]sighting, 0, len(entered))
	for _, p := range entered {
		s := sighting{p: p, gone: true}
		unlock := rt.lockDivision(match.division)
		s.character = rt.findCharacter(match.division, p.registrant.Name)
		if s.character != nil &&
			instance.ID(domain.CharacterWorldInstance(s.character)) == match.lease.ID {
			s.gone = false
			s.dead = !enterworld.CharacterAlive(s.character)
		}
		unlock()
		if s.character != nil && s.gone && !s.dead {
			character := s.character
			rt.deps.UpdateDeferred(character, "styria-anchor-clear", func() bool {
				if character.World != nil {
					character.World.InstanceReturn = nil
				}
				return true
			})
		}
		sightings = append(sightings, s)
	}

	var dead []*enterworld.Character
	var departed []*enterworld.Character
	match.mu.Lock()
	for _, s := range sightings {
		if !s.p.entered {
			continue // another lane settled this departure first
		}
		if s.gone || s.dead {
			s.p.entered = false
			if s.character != nil {
				departed = append(departed, s.character)
			}
			if s.dead && s.character != nil {
				dead = append(dead, s.character)
			}
		}
	}
	match.mu.Unlock()

	// Leaving the match ends the leaver's blessings with their seat.
	nowMs := rt.Now().UnixMilli()
	for _, character := range departed {
		rt.styriaRetireBlessings(match.division, character, nowMs)
	}
	for _, character := range dead {
		rt.ReturnCharacterFromInstance(match.division, character)
	}
}
