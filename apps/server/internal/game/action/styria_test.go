/*
===========================================================================

styria_test.go - Styria Clash: settings, the switch, talk rows, applying

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
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/styria"
)

// styriaSuitFlags packs a job suit's type word (3/1/7/<job>), the
// worn row DressedJob reads.
func styriaSuitFlags(job uint8) uint16 {
	return uint16(3<<2 | 1<<5 | 7<<7 | uint16(job)<<11)
}

/*
================
styriaTestCharacter

A level-20 character at the test start pose, dressed for job (0
leaves the suit off).
================
*/
func styriaTestCharacter(id int64, name string, job uint8) *enterworld.Character {
	character := rebirthTestCharacter(20, 100)
	character.ID = id
	character.Name = name
	if job != 0 {
		character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
			Slot:      int64(enterworld.JobSuitSlot),
			TypeFlags: styriaSuitFlags(job),
		})
	}
	return character
}

/*
================
newStyriaRuntime

The engine rig: the flag on, a 20:00 UTC start, the fake clock
inside the participation window (19:52), characters admitted at
the test start pose.
================
*/
func newStyriaRuntime(t *testing.T, characters ...*enterworld.Character) (*Runtime, *fakeClock) {
	t.Helper()
	if err := instance.RegisterPortOnly(instance.StyriaDefinitions()); err != nil {
		t.Fatal(err)
	}
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: characters},
		Items:      testItems(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.Date(2026, 1, 5, 19, 52, 0, 0, time.UTC)}
	rt.Now = clock.Now
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	rt.Styria = StyriaSettings{StartMinuteOfDay: 20 * 60, MinPerSide: 1}
	rt.ConfigureStyria("on")
	if rt.styria == nil {
		t.Fatal("ConfigureStyria(on) left the engine off")
	}
	for index, c := range characters {
		admitInstanceWorldCharacter(t, rt, c, uint64(index+1), instanceWorldTestStart)
	}
	return rt, clock
}

/*
================
styriaAnswerText

The notice text of an application answer (its first frame).
================
*/
func styriaAnswerText(t *testing.T, frames []wire.Frame) string {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("application answered with no frames")
	}
	// The notice layout: channel byte, unit count, UTF-16 units.
	payload := frames[0].Payload
	if len(payload) < 3 {
		t.Fatalf("notice payload too short: %x", payload)
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
TestStyriaSettingsFromEnv
================
*/
func TestStyriaSettingsFromEnv(t *testing.T) {
	t.Setenv(EnvStyriaStartUTC, "")
	t.Setenv(EnvStyriaMinPerSide, "")
	settings, err := StyriaSettingsFromEnv()
	if err != nil || settings != DefaultStyriaSettings() {
		t.Fatalf("defaults = %+v, %v", settings, err)
	}

	t.Setenv(EnvStyriaStartUTC, "06:30")
	t.Setenv(EnvStyriaMinPerSide, "3")
	settings, err = StyriaSettingsFromEnv()
	if err != nil || settings.StartMinuteOfDay != 6*60+30 || settings.MinPerSide != 3 {
		t.Fatalf("overrides = %+v, %v", settings, err)
	}

	t.Setenv(EnvStyriaStartUTC, "24:00")
	if _, err = StyriaSettingsFromEnv(); err == nil {
		t.Fatal("a 24:00 start parsed")
	}
	t.Setenv(EnvStyriaStartUTC, "")
	t.Setenv(EnvStyriaMinPerSide, "0")
	if _, err = StyriaSettingsFromEnv(); err == nil {
		t.Fatal("a zero minimum parsed")
	}
}

/*
================
TestStyriaOffByDefault

Off is native: no engine state, no talk rows, a nil tick - and an
"on" without the monster state still installs nothing.
================
*/
func TestStyriaOffByDefault(t *testing.T) {
	character := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	rt, _ := newStyriaRuntime(t, character)
	rt.styria = nil
	rt.ConfigureStyria("off")
	if rt.styria != nil {
		t.Fatal("the engine installed itself while off")
	}
	if options := rt.styriaTalkOptions(testDivision, character, styria.ManagerCodename); options != nil {
		t.Fatalf("talk rows while off: %+v", options)
	}
	if out := rt.advanceStyriaClash(0); out != nil {
		t.Fatalf("tick frames while off: %+v", out)
	}

	bare := NewRuntime(&enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
	}, nil)
	bare.ConfigureStyria("on")
	if bare.styria != nil {
		t.Fatal("the engine installed itself without a monster state")
	}
}

/*
================
TestStyriaTalkOptionsFollowApplication
================
*/
func TestStyriaTalkOptionsFollowApplication(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	rt, _ := newStyriaRuntime(t, hunter)

	if options := rt.styriaTalkOptions(testDivision, hunter, "NPC_CH_SMITH"); options != nil {
		t.Fatalf("rows at another NPC: %+v", options)
	}
	options := rt.styriaTalkOptions(testDivision, hunter, styria.ManagerCodename)
	if len(options) != 1 || options[0].Codename != styria.ServiceApply || !options[0].Immediate {
		t.Fatalf("rows before applying: %+v", options)
	}
	rt.styriaApply(testDivision, hunter)
	options = rt.styriaTalkOptions(testDivision, hunter, styria.ManagerCodename)
	if len(options) != 1 || options[0].Codename != styria.ServiceCancel {
		t.Fatalf("rows after applying: %+v", options)
	}
}

/*
================
TestStyriaApplyConditions

The manager's conditions, one refusal each; then the acceptances
land on the suit's side.
================
*/
func TestStyriaApplyConditions(t *testing.T) {
	plain := styriaTestCharacter(1, "Plain", 0)
	hunter := styriaTestCharacter(2, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(3, "Thief", domain.JobThief)
	trader := styriaTestCharacter(4, "Trader", domain.JobTrader)
	partied := styriaTestCharacter(5, "Partied", domain.JobHunter)
	dead := styriaTestCharacter(6, "Dead", domain.JobHunter)
	*dead.CurrentHP = 0
	rt, clock := newStyriaRuntime(t, plain, hunter, thief, trader, partied, dead)
	rt.RewardParties = func(string) []RewardParty {
		gid := enterworld.ObjectIDForCharacter(partied)
		return []RewardParty{{Members: []uint32{gid, gid + 100}}}
	}

	if text := styriaAnswerText(t, rt.styriaApply(testDivision, plain)); !strings.Contains(text, "job suit") {
		t.Fatalf("no-suit answer: %q", text)
	}
	if text := styriaAnswerText(t, rt.styriaApply(testDivision, partied)); !strings.Contains(text, "party") {
		t.Fatalf("partied answer: %q", text)
	}
	if text := styriaAnswerText(t, rt.styriaApply(testDivision, dead)); !strings.Contains(text, "dead") {
		t.Fatalf("dead answer: %q", text)
	}

	clock.now = time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	if text := styriaAnswerText(t, rt.styriaApply(testDivision, hunter)); !strings.Contains(text, "participation is closed") {
		t.Fatalf("closed-window answer: %q", text)
	}
	clock.now = time.Date(2026, 1, 5, 19, 52, 0, 0, time.UTC)

	if text := styriaAnswerText(t, rt.styriaApply(testDivision, hunter)); !strings.Contains(text, "Hunter side") {
		t.Fatalf("hunter answer: %q", text)
	}
	if !rt.styriaRegistered(testDivision, hunter.Name) {
		t.Fatal("the hunter holds no application")
	}
	if text := styriaAnswerText(t, rt.styriaApply(testDivision, thief)); !strings.Contains(text, "Thief side") {
		t.Fatalf("thief answer: %q", text)
	}
	// Port inference (styria.go): the trader fights on the Hunter side.
	if text := styriaAnswerText(t, rt.styriaApply(testDivision, trader)); !strings.Contains(text, "Hunter side") {
		t.Fatalf("trader answer: %q", text)
	}
	if text := styriaAnswerText(t, rt.styriaApply(testDivision, hunter)); !strings.Contains(text, "already applied") {
		t.Fatalf("double-apply answer: %q", text)
	}

	if text := styriaAnswerText(t, rt.styriaCancel(testDivision, hunter)); !strings.Contains(text, "cancelled") {
		t.Fatalf("cancel answer: %q", text)
	}
	if rt.styriaRegistered(testDivision, hunter.Name) {
		t.Fatal("the hunter still holds an application after cancelling")
	}
	if text := styriaAnswerText(t, rt.styriaCancel(testDivision, hunter)); !strings.Contains(text, "no application") {
		t.Fatalf("second cancel answer: %q", text)
	}
	// The rest of the queue is untouched by one withdrawal.
	if !rt.styriaRegistered(testDivision, thief.Name) || !rt.styriaRegistered(testDivision, trader.Name) {
		t.Fatal("a cancel withdrew another application")
	}
}
