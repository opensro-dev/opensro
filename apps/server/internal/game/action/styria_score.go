/*
===========================================================================

styria_score.go - Styria Clash: treasure, points and the score

Port-only, not native (epic #3648, the treasure-and-scoring slice).
The later client's own words carry the rules this file builds: the
arena manager says the side seizing more treasure wins, and the
registration window says points come from acquiring treasure boxes
and from PK against the opposing team. The five arena buffs are
evidenced too (the later client's skills 34622-34626) but wait for
their own slice: no table the port ships carries them and no
server-initiated cast lane exists yet.

PORT CHOICES, recorded because no source carries the native
numbers (the reference server's arena engine has no job-war world
at all): one point per box opened and one per opposing kill, no
early-win threshold (the clock decides, an equal score draws), a
box refills 60 seconds after an opening, and the click range is
the flag war's channel range. Kills inside a live match settle as
special-world deaths, as the flag war's do, never on the open-field
PK ladder. The plain-English notices are the server's until the
client slice binds the arena strings.

===========================================================================
*/

package action

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/styria"
)

// Scoring (PORT CHOICES, see the file header).
const (
	styriaPointsPerBox     = 1
	styriaPointsPerKill    = 1
	styriaBoxRefillMs      = 60 * 1000
	styriaBoxRange         = flagWarChannelRange
	styriaOutcomeNoticeWin = "Styria Clash: the %s win, %d to %d."
)

/*
================
styriaMatchFor

The live match in a division, or nil (the engine off, or no match
running there).
================
*/
func (rt *Runtime) styriaMatchFor(division string) *styriaMatch {
	if rt.styria == nil {
		return nil
	}
	rt.styria.mu.Lock()
	defer rt.styria.mu.Unlock()
	return rt.styria.matches[division]
}

/*
================
styriaSidesText

The standing score as the notices speak it, in side order.
================
*/
func styriaScoreText(score [2]int) string {
	return fmt.Sprintf("Hunters %d - Thieves %d", score[styriaSideHunters], score[styriaSideThieves])
}

/*
================
styriaAnnounce

One notice to every entered participant, in entry order. Nil-safe:
an unwired push lane (tests) simply does not push.
================
*/
func (rt *Runtime) styriaAnnounce(match *styriaMatch, text string) {
	if rt.PushCharacterFrames == nil {
		return
	}
	frames := []wire.Frame{wire.NotificationFrame(text)}
	match.mu.Lock()
	names := make([]string, 0, len(match.order))
	for _, id := range match.order {
		if p := match.participants[id]; p != nil && p.entered {
			names = append(names, p.registrant.Name)
		}
	}
	match.mu.Unlock()
	for _, name := range names {
		rt.PushCharacterFrames(match.division, name, frames)
	}
}

/*
================
styriaEndFrames

The clock's verdict, told to every entered participant: the same
words for the whole match (the reward slice pays each side its
own). Read under the match lock; the outcome is already decided.
================
*/
func styriaEndFrames(match *styriaMatch) []simulation.DivisionFrames {
	match.mu.Lock()
	text := fmt.Sprintf("Styria Clash: a draw, %d to %d.",
		match.score[styriaSideHunters], match.score[styriaSideThieves])
	if match.outcome == styriaOutcomeHunters {
		text = fmt.Sprintf(styriaOutcomeNoticeWin, "Hunters",
			match.score[styriaSideHunters], match.score[styriaSideThieves])
	}
	if match.outcome == styriaOutcomeThieves {
		text = fmt.Sprintf(styriaOutcomeNoticeWin, "Thieves",
			match.score[styriaSideThieves], match.score[styriaSideHunters])
	}
	participants := make([]*styriaParticipant, 0, len(match.order))
	for _, id := range match.order {
		if p := match.participants[id]; p != nil && p.entered {
			participants = append(participants, p)
		}
	}
	match.mu.Unlock()

	notice := wire.NotificationFrame(text)
	out := make([]simulation.DivisionFrames, 0, len(participants))
	for _, p := range participants {
		out = append(out, simulation.DivisionFrames{
			DivisionID:      match.division,
			OnlyCharacterID: p.registrant.CharacterID,
			Frames:          []simulation.Frame{{Opcode: notice.Opcode, Payload: notice.Payload}},
		})
	}
	return out
}

/*
================
styriaHostile

Two entered participants of the same live match on opposite sides
are enemies - the relation the death classification and the score
read. (Combat itself turns on the worn job suits, as in the open
field: a Hunter-side suit and a Thief suit are already opposed.)
================
*/
func (rt *Runtime) styriaHostile(division string, a, b *enterworld.Character) bool {
	if a == nil || b == nil {
		return false
	}
	match := rt.styriaMatchFor(division)
	if match == nil {
		return false
	}
	match.mu.Lock()
	defer match.mu.Unlock()
	pa := match.participants[a.ID]
	pb := match.participants[b.ID]
	if pa == nil || pb == nil || !pa.entered || !pb.entered {
		return false
	}
	return pa.registrant.Side != pb.registrant.Side
}

/*
================
styriaRecordDeath

The fatal-blow hook, beside the flag war's: an opposing participant
who lands the kill scores for their side, and the whole match hears
the standing. Runs under the fatal-hit publish (the division lane);
the match lock nests inside it, and nothing here takes the lane.
A victim who already left the match scores nothing.
================
*/
func (rt *Runtime) styriaRecordDeath(division string, victim, killer *enterworld.Character, nowMs int64) {
	match := rt.styriaMatchFor(division)
	if match == nil || victim == nil || killer == nil {
		return
	}
	match.mu.Lock()
	if match.closeMs != 0 {
		match.mu.Unlock()
		return
	}
	dead := match.participants[victim.ID]
	hunter := match.participants[killer.ID]
	if dead == nil || hunter == nil || !dead.entered || !hunter.entered ||
		dead.registrant.Side == hunter.registrant.Side {
		match.mu.Unlock()
		return
	}
	match.score[hunter.registrant.Side] += styriaPointsPerKill
	text := fmt.Sprintf("Styria Clash: %s fells %s. %s.",
		hunter.registrant.Name, dead.registrant.Name, styriaScoreText(match.score))
	match.mu.Unlock()
	rt.styriaAnnounce(match, text)
}

/*
================
styriaSelect

A treasure box's select: the click is the opening (the box plays
its open animation in the later client; here the click itself
scores). An entered participant beside a full box banks its point
for their side and starts its refill; an empty box says so. Anyone
else just selects it. Mirrors the flag war's object selects: runs
under the caller's division lane, the match lock nests inside.
================
*/
func (rt *Runtime) styriaSelect(divisionID string, character *enterworld.Character, gid uint32) (SelectOutcome, bool) {
	if rt.styria == nil {
		return SelectOutcome{}, false
	}
	index := simulation.StyriaBoxIndex(gid)
	if index < 0 {
		return SelectOutcome{}, false
	}
	match := rt.styriaMatchFor(divisionID)
	if match == nil {
		return SelectOutcome{}, false
	}
	nowMs := rt.Now().UnixMilli()
	pose := rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, nowMs)
	spot := styria.BoxSpots[index]
	dx := pose.X - spot.X
	dz := pose.Z - spot.Z
	if dx*dx+dz*dz > styriaBoxRange*styriaBoxRange {
		return SelectOutcome{
			Refusal: "styria box beyond interaction range",
			Frames: []wire.Frame{{
				Opcode:  wire.OpObjectSelectResult,
				Payload: wire.EncodeObjectSelectRefusal(hitRangeTooFar),
			}},
		}, true
	}
	rt.Selected.Set(divisionID, character.Name, gid)
	rt.NpcDialogs.Clear(divisionID, character.Name)
	outcome := SelectOutcome{
		Selected: gid,
		Frames: []wire.Frame{{
			Opcode:  wire.OpObjectSelectResult,
			Payload: wire.EncodeNpcObjectSelectResult(gid, 0, 0),
		}},
	}

	match.mu.Lock()
	participant := match.participants[character.ID]
	if participant == nil || !participant.entered || match.closeMs != 0 {
		match.mu.Unlock()
		return outcome, true
	}
	if nowMs < match.boxes[index] {
		match.mu.Unlock()
		outcome.Frames = append(outcome.Frames,
			wire.NotificationFrame("Styria Clash: the treasure box is empty; it refills soon."))
		return outcome, true
	}
	side := participant.registrant.Side
	match.score[side] += styriaPointsPerBox
	match.boxes[index] = nowMs + styriaBoxRefillMs
	text := fmt.Sprintf("Styria Clash: the %s take a treasure box. %s.",
		styriaSideNamePlural(side), styriaScoreText(match.score))
	match.mu.Unlock()

	rt.styriaAnnounce(match, text)
	return outcome, true
}

/*
================
styriaSideNamePlural
================
*/
func styriaSideNamePlural(side int) string {
	if side == styriaSideThieves {
		return "Thieves"
	}
	return "Hunters"
}
