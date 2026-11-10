/*
===========================================================================

levelcap_test.go - optional cap configuration, transitions and rollback

Synthetic curves isolate stopping behavior. Packet checks ensure the client
receives the applied delta and cannot walk past the server's chosen cap.

===========================================================================
*/
package progression

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestLevelCapConfiguration
================
*/
func TestLevelCapConfiguration(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int64
	}{
		{"", LevelCap}, {"off", LevelCap}, {"false", LevelCap},
		{"0", LevelCap}, {"90", LevelCap}, {" 110 ", ExpansionLevelCap},
		{"on", 0}, {"120", 0}, {"-1", 0}, {"garbage", 0},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv(EnvLevelCap, test.value)
			got, err := LevelCapFromEnv()
			if got != test.want || (err != nil) != (test.want == 0) {
				t.Fatalf("cap = %d, error = %v; want %d", got, err, test.want)
			}
		})
	}
}

/*
================
TestExpansionCapTransitionsAndAppliedDelta
================
*/
func TestExpansionCapTransitionsAndAppliedDelta(t *testing.T) {
	for _, test := range []struct {
		name      string
		cap       int64
		start     int64
		wantLevel int64
		wantExp   int64
		wantDelta int64
	}{
		{"native", 0, 90, 90, 99, 99},
		{"crossNativeBoundary", ExpansionLevelCap, 90, 91, 50, 150},
		{"crossExpansionBoundary", ExpansionLevelCap, 109, 110, 50, 150},
		{"freezeExpansion", ExpansionLevelCap, 110, 110, 99, 99},
		{"rollbackKeepsExistingLevel", 0, 110, 110, 99, 99},
	} {
		t.Run(test.name, func(t *testing.T) {
			character := levelupTestCharacter()
			character.Level = int64Ptr(test.start)
			character.MaxLevel = int64Ptr(test.start)
			rt := newTestRuntime(character)
			rt.LevelLimit = test.cap
			rt.deps.(*enterworld.Deps).Levels = staticLevels{exp: map[int64]int64{
				90: 100, 91: 100, 109: 100, 110: 100,
			}}
			result := rt.GrantExperience(character, 150, 0, 0)
			if *character.Level != test.wantLevel || *character.Experience != test.wantExp {
				t.Fatalf("level/exp = %d/%d, want %d/%d", *character.Level, *character.Experience, test.wantLevel, test.wantExp)
			}
			if len(result.Frames) == 0 {
				t.Fatal("grant emitted no frames")
			}
			payload := result.Frames[len(result.Frames)-1].Payload
			if got := int64(int32(binary.LittleEndian.Uint32(payload[4:8]))); got != test.wantDelta {
				t.Fatalf("wire delta = %d, want %d", got, test.wantDelta)
			}
			if test.wantLevel > test.start && (*character.StatPoints != 4 || *character.Strength != enterworld.BaseStat+1) {
				t.Fatal("expansion crossing did not grant the ordinary level rewards")
			}
		})
	}
}

/*
================
TestExpansionCapMissingCurveRefusesAtomically
================
*/
func TestExpansionCapMissingCurveRefusesAtomically(t *testing.T) {
	character := levelupTestCharacter()
	character.Level = int64Ptr(90)
	rt := newTestRuntime(character)
	rt.LevelLimit = ExpansionLevelCap
	result := rt.GrantExperience(character, 0x7fffffff, 0, 0)
	if len(result.Frames) != 0 || *character.Level != 90 || character.Experience != nil || *character.StatPoints != 1 {
		t.Fatal("missing level-91 curve leaked a partial transition")
	}
}

/*
================
TestEnterworldExpansionRuntimeFollowsTheLevelCap

enterworld cannot import this package, so it mirrors the flag's name and its expansion value;
this keeps the mirror and LevelCapFromEnv in step.
================
*/
func TestEnterworldExpansionRuntimeFollowsTheLevelCap(t *testing.T) {
	if enterworld.EnvLevelCap != EnvLevelCap {
		t.Fatalf("enterworld names the flag %q, progression %q", enterworld.EnvLevelCap, EnvLevelCap)
	}
	for _, value := range []string{"", "off", "false", "0", "90", "110"} {
		t.Setenv(EnvLevelCap, value)
		limit, err := LevelCapFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if enterworld.ExpansionRuntime() != (limit == ExpansionLevelCap) {
			t.Fatalf("%s=%q: progression says cap %d, enterworld expansion %v", EnvLevelCap, value, limit, enterworld.ExpansionRuntime())
		}
	}
}
