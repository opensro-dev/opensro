/*
===========================================================================

flagwar_test.go - Capture the Flag registration and schedule

The toggle answers by level and window, the select byte follows the
queue, the standby edges broadcast in order and a short house cancels
at the start instant while a full one goes to the match owner.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// flagWarDay is the test schedule: a 20:00 UTC start on 2026-10-24.
func flagWarStart() time.Time {
	return time.Date(2026, 10, 24, 20, 0, 0, 0, time.UTC)
}

/*
================
TestFlagWarSettingsFromEnv
================
*/
func TestFlagWarSettingsFromEnv(t *testing.T) {
	t.Setenv(EnvFlagWarStartUTC, "")
	t.Setenv(EnvFlagWarMinPlayers, "")
	settings, err := FlagWarSettingsFromEnv()
	if err != nil || settings != DefaultFlagWarSettings() {
		t.Fatalf("defaults: %v, %v", settings, err)
	}

	t.Setenv(EnvFlagWarStartUTC, "07:30")
	t.Setenv(EnvFlagWarMinPlayers, "8")
	settings, err = FlagWarSettingsFromEnv()
	if err != nil || settings.StartMinuteOfDay != 7*60+30 || settings.MinPlayers != 8 {
		t.Fatalf("overrides: %v, %v", settings, err)
	}

	t.Setenv(EnvFlagWarStartUTC, "24:00")
	if _, err = FlagWarSettingsFromEnv(); err == nil {
		t.Fatal("a 24:00 start was accepted")
	}
	t.Setenv(EnvFlagWarStartUTC, "20:00")
	t.Setenv(EnvFlagWarMinPlayers, "17")
	if _, err = FlagWarSettingsFromEnv(); err == nil {
		t.Fatal("a minimum above the world player limit was accepted")
	}
}

/*
================
TestFlagWarSignupWindow
================
*/
func TestFlagWarSignupWindow(t *testing.T) {
	settings := DefaultFlagWarSettings()
	start := flagWarStart()
	for _, probe := range []struct {
		at   time.Time
		open bool
	}{
		{start.Add(-11 * time.Minute), false},
		{start.Add(-10 * time.Minute), true},
		{start.Add(-time.Minute), true},
		{start, false},
		{start.Add(time.Hour), false},
	} {
		if got := settings.signupOpen(probe.at); got != probe.open {
			t.Fatalf("signupOpen(%v) = %v, want %v", probe.at, got, probe.open)
		}
	}
}

/*
================
TestFlagWarSignupAgreesWithTheCalendarAcrossMidnight

A start in the first minutes after midnight UTC has its sign-up on the
day before. The runtime used to look only at today's start, so at 23:55
the sign-up for 00:05 stayed shut while the public calendar showed it
open. Minute by minute over two days, for starts on both sides of
midnight, the runtime's sign-up is exactly the calendar's (NextMatch).
================
*/
func TestFlagWarSignupAgreesWithTheCalendarAcrossMidnight(t *testing.T) {
	day := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	for _, minuteOfDay := range []int{0, 5, 9, 10, 20 * 60, 23*60 + 59} {
		settings := DefaultFlagWarSettings()
		settings.StartMinuteOfDay = minuteOfDay
		for at := day; at.Before(day.AddDate(0, 0, 2)); at = at.Add(time.Minute) {
			signupAt, _ := settings.NextMatch(at)
			if want := !at.Before(signupAt); settings.signupOpen(at) != want {
				t.Fatalf("start %02d:%02d at %v: signupOpen = %v, the calendar says %v",
					minuteOfDay/60, minuteOfDay%60, at, !want, want)
			}
		}
	}
}

/*
================
TestFlagWarSchedulerStartsAMatchJustAfterMidnight

A 00:05 start: the ten-minute notice fires at 23:55 the day before and
the start fires at 00:05, each once.
================
*/
func TestFlagWarSchedulerStartsAMatchJustAfterMidnight(t *testing.T) {
	character := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(character, testItems())
	rt.characterAdmissions.Store(simulation.WorldKey(testDivision, character.Name),
		populationAdmission{division: testDivision, name: character.Name})
	rt.FlagWar = DefaultFlagWarSettings()
	rt.FlagWar.StartMinuteOfDay = 5
	start := time.Date(2026, 11, 17, 0, 5, 0, 0, time.UTC)
	rt.advanceFlagWar(start.Add(-11 * time.Minute).UnixMilli())
	if out := rt.advanceFlagWar(start.Add(-10 * time.Minute).UnixMilli()); len(out) == 0 {
		t.Fatal("no ten-minute notice at 23:55 for a 00:05 start")
	}
	if out := rt.advanceFlagWar(start.Add(-10*time.Minute + time.Second).UnixMilli()); len(out) != 0 {
		t.Fatalf("the notice fired twice: %+v", out)
	}
}

/*
================
flagWarAnswerSubtype
================
*/
func flagWarAnswerSubtype(t *testing.T, result OpResult) uint8 {
	t.Helper()
	if len(result.Frames) != 1 || result.Frames[0].Opcode != wire.OpFlagWarEvent {
		t.Fatalf("frames %+v, want one 0x30A9 answer", result.Frames)
	}
	payload := result.Frames[0].Payload
	if len(payload) != 2 || payload[0] != wire.FlagWarSubtypeTag {
		t.Fatalf("payload %v, want [0xFF, subtype]", payload)
	}
	return payload[1]
}

/*
================
TestFlagWarRegisterTogglesAndAnswers
================
*/
func TestFlagWarRegisterTogglesAndAnswers(t *testing.T) {
	character := rebirthTestCharacter(20, 100)
	rt, clock := newTestRuntime(character, testItems())
	clock.now = flagWarStart().Add(-5 * time.Minute)

	if subtype := flagWarAnswerSubtype(t, rt.HandleFlagWarRegister(testDivision, character, nil)); subtype != wire.FlagWarJoinResult {
		t.Fatalf("join answer subtype %d, want %d", subtype, wire.FlagWarJoinResult)
	}
	if !rt.flagWarRegistered(testDivision, character.Name) {
		t.Fatal("the join did not queue the character")
	}
	if subtype := flagWarAnswerSubtype(t, rt.HandleFlagWarRegister(testDivision, character, nil)); subtype != wire.FlagWarJoinResult {
		t.Fatalf("cancel answer subtype %d, want %d", subtype, wire.FlagWarJoinResult)
	}
	if rt.flagWarRegistered(testDivision, character.Name) {
		t.Fatal("the second send did not cancel the registration")
	}

	clock.now = flagWarStart().Add(-11 * time.Minute)
	if subtype := flagWarAnswerSubtype(t, rt.HandleFlagWarRegister(testDivision, character, nil)); subtype != wire.FlagWarEnteredError {
		t.Fatalf("closed-window answer subtype %d, want %d", subtype, wire.FlagWarEnteredError)
	}

	clock.now = flagWarStart().Add(-5 * time.Minute)
	junior := rebirthTestCharacter(19, 100)
	junior.Name = "Junior"
	if subtype := flagWarAnswerSubtype(t, rt.HandleFlagWarRegister(testDivision, junior, nil)); subtype != wire.FlagWarLevelError {
		t.Fatalf("under-level answer subtype %d, want %d", subtype, wire.FlagWarLevelError)
	}

	if result := rt.HandleFlagWarRegister(testDivision, character, []byte{1}); result.DiagnosticRefusal == "" || len(result.Frames) != 0 {
		t.Fatalf("a non-empty body answered %+v, want a silent refusal", result)
	}
}

/*
================
TestFlagWarRegisterRefusesTheUnadmitted

The native registration path runs the same entry admission as the
match-start batch (flagWarCanEnter): a dead or mounted character is
answered the entered error and never queues.
================
*/
func TestFlagWarRegisterRefusesTheUnadmitted(t *testing.T) {
	dead := rebirthTestCharacter(20, 0)
	rt, clock := newTestRuntime(dead, testItems())
	clock.now = flagWarStart().Add(-5 * time.Minute)
	if subtype := flagWarAnswerSubtype(t, rt.HandleFlagWarRegister(testDivision, dead, nil)); subtype != wire.FlagWarEnteredError {
		t.Fatalf("dead answer subtype %d, want %d", subtype, wire.FlagWarEnteredError)
	}
	if rt.flagWarRegistered(testDivision, dead.Name) {
		t.Fatal("a refused dead registration queued")
	}

	mounted := rebirthTestCharacter(20, 100)
	mounted.Name = "Mounted"
	mounted.ActiveCOS = &enterworld.CharacterCOS{Summoned: true, Mounted: true}
	rt2, clock2 := newTestRuntime(mounted, testItems())
	clock2.now = flagWarStart().Add(-5 * time.Minute)
	if subtype := flagWarAnswerSubtype(t, rt2.HandleFlagWarRegister(testDivision, mounted, nil)); subtype != wire.FlagWarEnteredError {
		t.Fatalf("mounted answer subtype %d, want %d", subtype, wire.FlagWarEnteredError)
	}
	if rt2.flagWarRegistered(testDivision, mounted.Name) {
		t.Fatal("a refused mounted registration queued")
	}
}

/*
================
TestFlagWarRegisterCancelSkipsAdmission

The native register body checks the queue before any gate: a
registrant who dies or mounts after joining still cancels with the
ordinary join-result answer, and the queue shrinks.
================
*/
func TestFlagWarRegisterCancelSkipsAdmission(t *testing.T) {
	character := rebirthTestCharacter(20, 100)
	rt, clock := newTestRuntime(character, testItems())
	clock.now = flagWarStart().Add(-5 * time.Minute)

	if subtype := flagWarAnswerSubtype(t, rt.HandleFlagWarRegister(testDivision, character, nil)); subtype != wire.FlagWarJoinResult {
		t.Fatalf("join answer subtype %d, want %d", subtype, wire.FlagWarJoinResult)
	}
	if !rt.flagWarRegistered(testDivision, character.Name) {
		t.Fatal("the join did not queue the character")
	}

	*character.CurrentHP = 0
	character.ActiveCOS = &enterworld.CharacterCOS{Summoned: true, Mounted: true}
	if subtype := flagWarAnswerSubtype(t, rt.HandleFlagWarRegister(testDivision, character, nil)); subtype != wire.FlagWarJoinResult {
		t.Fatalf("cancel answer subtype %d, want %d", subtype, wire.FlagWarJoinResult)
	}
	if rt.flagWarRegistered(testDivision, character.Name) {
		t.Fatal("the gated cancel left the character queued")
	}
}

/*
================
TestFlagWarSelectByte
================
*/
func TestFlagWarSelectByte(t *testing.T) {
	character := rebirthTestCharacter(20, 100)
	rt, clock := newTestRuntime(character, testItems())
	clock.now = flagWarStart().Add(-5 * time.Minute)
	host := simulation.NpcDef{Codename: "NPC_CH_EVENT_KISAENG1"}
	other := simulation.NpcDef{Codename: "NPC_CH_SMITH"}

	if got := rt.flagWarSelectByte(testDivision, character, other); got != 0 {
		t.Fatalf("select byte for a smith = %d, want 0", got)
	}
	if got := rt.flagWarSelectByte(testDivision, character, host); got != 1 {
		t.Fatalf("select byte before joining = %d, want 1", got)
	}
	rt.HandleFlagWarRegister(testDivision, character, nil)
	if got := rt.flagWarSelectByte(testDivision, character, host); got != 2 {
		t.Fatalf("select byte after joining = %d, want 2", got)
	}
	clock.now = flagWarStart().Add(time.Hour)
	if got := rt.flagWarSelectByte(testDivision, character, host); got != 0 {
		t.Fatalf("select byte after the start = %d, want 0", got)
	}
}

/*
================
TestFlagWarSchedulerCancelsAShortHouse
================
*/
func TestFlagWarSchedulerCancelsAShortHouse(t *testing.T) {
	first := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(first, testItems())
	second := rebirthTestCharacter(20, 100)
	second.Name, second.ID = "Second", first.ID+1000
	rt.flagWarToggle(testDivision, first)
	rt.flagWarToggle(testDivision, second)

	start := flagWarStart()
	if out := rt.advanceFlagWar(start.Add(-11 * time.Minute).UnixMilli()); len(out) != 0 {
		t.Fatalf("frames before the window: %+v", out)
	}
	out := rt.advanceFlagWar(start.UnixMilli())
	if len(out) != 2 {
		t.Fatalf("cancel frames %+v, want one per registrant", out)
	}
	for _, batch := range out {
		if batch.OnlyCharacterID == 0 || len(batch.Frames) != 1 ||
			batch.Frames[0].Opcode != wire.OpFlagWarEvent ||
			string(batch.Frames[0].Payload) != string(wire.EncodeFlagWarEventSubtype(wire.FlagWarGameCancel)) {
			t.Fatalf("cancel batch %+v, want a targeted game-cancel notice", batch)
		}
	}
	if rt.flagWarRegistered(testDivision, first.Name) || rt.flagWarRegistered(testDivision, second.Name) {
		t.Fatal("the queue survived the cancelled start")
	}
	// The public schedule reads the cancel (#3706): this start only.
	if !rt.FlagWarCancelled(start) {
		t.Fatal("the cancelled start is not recorded for the schedule")
	}
	if rt.FlagWarCancelled(start.AddDate(0, 0, 1)) || rt.FlagWarCancelled(start.AddDate(0, 0, -1)) {
		t.Fatal("another day's start reads as cancelled")
	}
}

/*
================
TestFlagWarSchedulerFiresALateStartAcrossAStalledMidnight

A 23:59 UTC start whose tick stalls across midnight: the next tick is on
the next UTC day, and only yesterday's start names the edge it owes
(#3706). It fires once, late, and never again.
================
*/
func TestFlagWarSchedulerFiresALateStartAcrossAStalledMidnight(t *testing.T) {
	first := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(first, testItems())
	rt.FlagWar = FlagWarSettings{StartMinuteOfDay: 23*60 + 59, MinPlayers: 4}
	start := time.Date(2026, 10, 24, 23, 59, 0, 0, time.UTC)

	rt.advanceFlagWar(start.Add(-2 * time.Minute).UnixMilli())
	rt.flagWarToggle(testDivision, first)
	// The tick stalls from 23:57:30 to 00:00:30 on the next day.
	rt.advanceFlagWar(start.Add(-90 * time.Second).UnixMilli())
	out := rt.advanceFlagWar(start.Add(90 * time.Second).UnixMilli())
	var cancels int
	for _, batch := range out {
		for _, frame := range batch.Frames {
			if string(frame.Payload) == string(wire.EncodeFlagWarEventSubtype(wire.FlagWarGameCancel)) {
				cancels++
			}
		}
	}
	if cancels != 1 || !rt.FlagWarCancelled(start) {
		t.Fatalf("stalled midnight: %d cancel notices, cancelled=%v; want the 23:59 start resolved once", cancels, rt.FlagWarCancelled(start))
	}
	if again := rt.advanceFlagWar(start.Add(3 * time.Minute).UnixMilli()); len(again) != 0 {
		t.Fatalf("the late start fired again: %+v", again)
	}
}

/*
================
TestFlagWarSchedulerHandsAFullHouseToTheMatchOwner
================
*/
func TestFlagWarSchedulerHandsAFullHouseToTheMatchOwner(t *testing.T) {
	first := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(first, testItems())
	rt.FlagWar = FlagWarSettings{StartMinuteOfDay: 20 * 60, MinPlayers: 2}
	second := rebirthTestCharacter(20, 100)
	second.Name, second.ID = "Second", first.ID+1000
	rt.flagWarToggle(testDivision, second)
	rt.flagWarToggle(testDivision, first)

	var handed []FlagWarRegistrant
	rt.FlagWarMatchStart = func(registrants []FlagWarRegistrant) []simulation.DivisionFrames {
		handed = registrants
		return nil
	}
	start := flagWarStart()
	rt.advanceFlagWar(start.Add(-11 * time.Minute).UnixMilli())
	rt.advanceFlagWar(start.UnixMilli())
	if len(handed) != 2 || handed[0].Name != second.Name || handed[1].Name != first.Name {
		t.Fatalf("match owner received %+v, want sign-up order [Second, %s]", handed, first.Name)
	}
	if rt.FlagWarCancelled(start) {
		t.Fatal("a started match reads as cancelled")
	}
}
