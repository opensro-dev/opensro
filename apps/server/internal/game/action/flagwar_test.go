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
}
