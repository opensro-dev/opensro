/*
===========================================================================

styria_score_test.go - Styria Clash: boxes, kill points, the verdict

===========================================================================
*/
package action

import (
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// styriaPushLog records the notices the engine pushed, per character.
type styriaPushLog struct {
	byName map[string][]string
}

/*
================
arm

Install the recording push lane on the runtime.
================
*/
func (log *styriaPushLog) arm(rt *Runtime) {
	log.byName = make(map[string][]string)
	rt.PushCharacterFrames = func(_ string, name string, frames []wire.Frame) {
		for _, frame := range frames {
			log.byName[name] = append(log.byName[name], styriaNoticeText(frame.Payload))
		}
	}
}

/*
================
heard

Whether the named character was pushed a notice containing text.
================
*/
func (log *styriaPushLog) heard(name, text string) bool {
	for _, notice := range log.byName[name] {
		if strings.Contains(notice, text) {
			return true
		}
	}
	return false
}

/*
================
styriaNoticeText

The text of a notification payload (channel byte, unit count,
UTF-16 units), the layout styriaAnswerText reads.
================
*/
func styriaNoticeText(payload []byte) string {
	if len(payload) < 3 {
		return ""
	}
	payload = payload[3:]
	units := make([]uint16, 0, len(payload)/2)
	for i := 0; i+1 < len(payload); i += 2 {
		units = append(units, uint16(payload[i])|uint16(payload[i+1])<<8)
	}
	return string(utf16.Decode(units))
}

/*
================
styriaStandAt

Stand a character on a pose in the simulated world.
================
*/
func styriaStandAt(rt *Runtime, c *enterworld.Character, spawn simulation.Spawn) {
	key := simulation.WorldKey(testDivision, c.Name)
	rt.Worlds.Update(key,
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) { w.Spawn = spawn })
}

/*
================
styriaScoreOf
================
*/
func styriaScoreOf(match *styriaMatch) [2]int {
	match.mu.Lock()
	defer match.mu.Unlock()
	return match.score
}

/*
================
TestStyriaBoxScoresRefillsAndRefusesAfar

A participant beside a full box banks its point for their side and
the whole match hears it; the emptied box refuses until its refill,
and a click from across the arena is out of range.
================
*/
func TestStyriaBoxScoresRefillsAndRefusesAfar(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	rt, clock := newStyriaRuntime(t, hunter, thief)
	var log styriaPushLog
	log.arm(rt)
	match := styriaStartApplied(t, rt, clock, hunter, thief)

	// The thief clicks the west box from their own gate: too far.
	outcome, handled := rt.styriaSelect(testDivision, thief, 242000)
	if !handled || outcome.Refusal == "" {
		t.Fatalf("far box click = %+v, %v", outcome, handled)
	}
	if score := styriaScoreOf(match); score != [2]int{0, 0} {
		t.Fatalf("score after a refused click: %v", score)
	}

	// The hunter stands at the east box and opens it.
	styriaStandAt(rt, hunter, simulation.Spawn{RegionID: 32242, X: 1100, Z: -1832})
	outcome, handled = rt.styriaSelect(testDivision, hunter, 242003)
	if !handled || outcome.Selected != 242003 {
		t.Fatalf("box click = %+v, %v", outcome, handled)
	}
	if score := styriaScoreOf(match); score != [2]int{1, 0} {
		t.Fatalf("score after the opening: %v", score)
	}
	if !log.heard("Thief", "take a treasure box") || !log.heard("Thief", "Hunters 1 - Thieves 0") {
		t.Fatalf("the match did not hear the opening: %v", log.byName)
	}

	// The emptied box refuses a second opening until it refills.
	outcome, _ = rt.styriaSelect(testDivision, hunter, 242003)
	if text := styriaFramesText(outcome.Frames); !strings.Contains(text, "empty") {
		t.Fatalf("second opening answer: %q", text)
	}
	clock.now = clock.now.Add(61 * time.Second)
	if _, handled = rt.styriaSelect(testDivision, hunter, 242003); !handled {
		t.Fatal("the refilled box refused its click")
	}
	if score := styriaScoreOf(match); score != [2]int{2, 0} {
		t.Fatalf("score after the refill: %v", score)
	}

	// The thief takes the west box for their own side.
	styriaStandAt(rt, thief, simulation.Spawn{RegionID: 31985, X: 650, Z: -1832})
	if _, handled = rt.styriaSelect(testDivision, thief, 242000); !handled {
		t.Fatal("the thief's box click went unanswered")
	}
	if score := styriaScoreOf(match); score != [2]int{2, 1} {
		t.Fatalf("score after the thief's opening: %v", score)
	}
}

/*
================
styriaFramesText

Every notice text in a frame batch, joined.
================
*/
func styriaFramesText(frames []wire.Frame) string {
	var texts []string
	for _, frame := range frames {
		if text := styriaNoticeText(frame.Payload); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, " ")
}

/*
================
TestStyriaKillScoresOnlyForAnOpponent

The fatal-blow hook: an opposing participant's kill scores, a
spectator's or a finished match's does not.
================
*/
func TestStyriaKillScoresOnlyForAnOpponent(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	spectator := styriaTestCharacter(3, "Watcher", domain.JobHunter)
	rt, clock := newStyriaRuntime(t, hunter, thief, spectator)
	var log styriaPushLog
	log.arm(rt)
	match := styriaStartApplied(t, rt, clock, hunter, thief)

	rt.styriaRecordDeath(testDivision, thief, spectator, clock.NowMs())
	if score := styriaScoreOf(match); score != [2]int{0, 0} {
		t.Fatalf("a non-participant's kill scored: %v", score)
	}
	rt.styriaRecordDeath(testDivision, thief, hunter, clock.NowMs())
	if score := styriaScoreOf(match); score != [2]int{1, 0} {
		t.Fatalf("the hunter's kill did not score: %v", score)
	}
	if !log.heard("Thief", "fells Thief") {
		t.Fatalf("the match did not hear the kill: %v", log.byName)
	}
	if !rt.styriaHostile(testDivision, hunter, thief) || rt.styriaHostile(testDivision, hunter, spectator) {
		t.Fatal("arena hostility misread the seats")
	}
}

/*
================
TestStyriaClockDecidesWinAndDraw

At the clock the higher score wins and an equal score draws; every
entered participant hears the verdict, and the outcome stays on the
match for the reward slice.
================
*/
func TestStyriaClockDecidesWinAndDraw(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	rt, clock := newStyriaRuntime(t, hunter, thief)
	match := styriaStartApplied(t, rt, clock, hunter, thief)
	rt.styriaRecordDeath(testDivision, thief, hunter, clock.NowMs())

	clock.now = time.UnixMilli(match.endsMs)
	frames := rt.advanceStyriaMatches(clock.NowMs())
	match.mu.Lock()
	outcome := match.outcome
	match.mu.Unlock()
	if outcome != styriaOutcomeHunters {
		t.Fatalf("outcome = %d, want the Hunters' win", outcome)
	}
	var texts []string
	for _, batch := range frames {
		for _, frame := range batch.Frames {
			texts = append(texts, styriaNoticeText(frame.Payload))
		}
	}
	joined := strings.Join(texts, " ")
	if !strings.Contains(joined, "Hunters win, 1 to 0") || len(frames) != 2 {
		t.Fatalf("verdict frames: %q (%d batches)", joined, len(frames))
	}
	// A finished match scores no more kills.
	rt.styriaRecordDeath(testDivision, hunter, thief, clock.NowMs())
	if score := styriaScoreOf(match); score != [2]int{1, 0} {
		t.Fatalf("a kill after the clock scored: %v", score)
	}

	hunter2 := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief2 := styriaTestCharacter(2, "Thief", domain.JobThief)
	rt2, clock2 := newStyriaRuntime(t, hunter2, thief2)
	match2 := styriaStartApplied(t, rt2, clock2, hunter2, thief2)
	clock2.now = time.UnixMilli(match2.endsMs)
	frames = rt2.advanceStyriaMatches(clock2.NowMs())
	match2.mu.Lock()
	outcome = match2.outcome
	match2.mu.Unlock()
	if outcome != styriaOutcomeDraw {
		t.Fatalf("scoreless outcome = %d, want the draw", outcome)
	}
	texts = texts[:0]
	for _, batch := range frames {
		for _, frame := range batch.Frames {
			texts = append(texts, styriaNoticeText(frame.Payload))
		}
	}
	if joined = strings.Join(texts, " "); !strings.Contains(joined, "a draw, 0 to 0") {
		t.Fatalf("draw verdict frames: %q", joined)
	}
}

/*
================
TestStyriaBoxNeedsALiveMatch

No match, no box: the select falls through unanswered, and with the
engine off the box identity does not exist at all.
================
*/
func TestStyriaBoxNeedsALiveMatch(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	rt, _ := newStyriaRuntime(t, hunter)
	if _, handled := rt.styriaSelect(testDivision, hunter, 242000); handled {
		t.Fatal("a box answered with no match running")
	}
	rt.styria = nil
	if _, handled := rt.styriaSelect(testDivision, hunter, 242000); handled {
		t.Fatal("a box answered with the engine off")
	}
}
