/*
===========================================================================

benchmark_fixture.go - the development-only scenario reset

POST /development/benchmark-fixture/reset puts one offline probe character
into a known state: its spawn, movement mode and, optionally, a loadout
(level, intellect, skills) a scenario needs. It exists only when the
development gate is on, mutates through the GameWorld's own store under the
offline character-mutation control, and never runs on production workers.

===========================================================================
*/
package agentapi

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"opensro.online/server/internal/domain"
)

// SkillGroupResolver names a skill's group (all ranks of one skill share
// it). ok=false for an id the server's skill data does not know.
type SkillGroupResolver func(id uint32) (group uint32, ok bool)

// BenchmarkFixtureResetPath is routed through Agent to the selected
// GameWorld. The GameWorld registers it only when the explicit development
// gate is enabled, so a production worker has no teleport-shaped route.
const BenchmarkFixtureResetPath = "/development/benchmark-fixture/reset"

const (
	// benchmarkFixtureMaxIntellect bounds intellect to what a character can
	// reach; benchmarkFixtureMaxSkills bounds the list one request may teach.
	// The level bound is the game's own cap, injected as Config.LevelCap.
	benchmarkFixtureMaxIntellect = 2000
	benchmarkFixtureMaxSkills    = 64
)

/*
================
benchmarkFixtureSpawn
================
*/
type benchmarkFixtureSpawn struct {
	RegionID int64   `json:"regionId"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	Angle    int64   `json:"angle"`
}

/*
================
benchmarkFixtureLoadout

What a scenario needs its probe character to be able to do: a level and
intellect high enough to pay a skill's MP, and the skills themselves.
Stored MP and HP are cleared so the next login derives them full.
================
*/
type benchmarkFixtureLoadout struct {
	Level     int64    `json:"level"`
	Intellect int64    `json:"intellect"`
	Skills    []uint32 `json:"skills"`
}

/*
================
benchmarkFixtureResetRequest
================
*/
type benchmarkFixtureResetRequest struct {
	CharacterName string                   `json:"characterName"`
	FixtureID     string                   `json:"fixtureId"`
	MovementMode  int64                    `json:"movementMode"`
	Spawn         benchmarkFixtureSpawn    `json:"spawn"`
	Loadout       *benchmarkFixtureLoadout `json:"loadout,omitempty"`
}

/*
================
API.handleBenchmarkFixtureReset

Validates, takes the offline mutation control, and resets the world (and
loadout) only when they differ from the request; a match is already-reset.
================
*/
func (api *API) handleBenchmarkFixtureReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request benchmarkFixtureResetRequest
	if err := decodeJSONRequest(r.Body, &request); err != nil || !validBenchmarkFixtureReset(request, api.levelCap) ||
		!api.knownBenchmarkFixtureSkills(request.Loadout) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "BAD_REQUEST"})
		return
	}

	division := requestShardID(r)
	characterName := strings.TrimSpace(request.CharacterName)
	character := api.findCharacter(division, requestAccountID(r), characterName)
	if character == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "code": "UNKNOWN_CHARACTER"})
		return
	}
	releaseControl, acquired := api.acquireCharacterMutationControl(division, characterName)
	if !acquired {
		writeCharacterInPlay(w)
		return
	}
	defer releaseControl()

	refusal := ""
	outcome := "reset"
	changed := api.store.UpdateCharacter(character, "benchmark-fixture-reset "+strings.TrimSpace(request.FixtureID), func() bool {
		if character.DeletePending {
			refusal = "CHARACTER_UNAVAILABLE"
			return false
		}
		if benchmarkFixtureWorldMatches(character.World, request) && benchmarkFixtureLoadoutMatches(character, request.Loadout, api.skillGroup) {
			outcome = "already-reset"
			return false
		}
		applyBenchmarkFixtureLoadout(character, request.Loadout, api.skillGroup)
		world := domain.CharacterWorld{}
		if character.World != nil {
			world = *character.World
		}
		regionID, x, y, z, angle := request.Spawn.RegionID, request.Spawn.X, request.Spawn.Y, request.Spawn.Z, request.Spawn.Angle
		movementMode := request.MovementMode
		world.Spawn = &domain.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle}
		world.AuthoredAreaReturn = nil
		world.MovementMode = &movementMode
		world.SpawnSet = true
		world.MovementSourceSeeded = true
		world.MoveSegment = nil
		world.DungeonFloorIndex = nil
		character.World = &world
		return true
	})
	if !changed && refusal != "" {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "code": refusal})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"outcome":      outcome,
		"fixtureId":    strings.TrimSpace(request.FixtureID),
		"movementMode": request.MovementMode,
		"spawn":        request.Spawn,
	})
}

/*
================
validBenchmarkFixtureReset
================
*/
func validBenchmarkFixtureReset(request benchmarkFixtureResetRequest, levelCap int64) bool {
	return strings.TrimSpace(request.CharacterName) != "" &&
		strings.TrimSpace(request.FixtureID) != "" && len(strings.TrimSpace(request.FixtureID)) <= 128 &&
		request.Spawn.RegionID > 0 && request.Spawn.RegionID <= 0xffff &&
		finiteInRange(request.Spawn.X, 0, 1920) &&
		finiteInRange(request.Spawn.Z, 0, 1920) &&
		finite(request.Spawn.Y) && request.Spawn.Y >= -32768 && request.Spawn.Y <= 32767 &&
		request.Spawn.Angle >= 0 && request.Spawn.Angle <= 0xffff &&
		(request.MovementMode == 1 || request.MovementMode == 3) &&
		validBenchmarkFixtureLoadout(request.Loadout, levelCap)
}

/*
================
validBenchmarkFixtureLoadout
================
*/
func validBenchmarkFixtureLoadout(loadout *benchmarkFixtureLoadout, levelCap int64) bool {
	if loadout == nil {
		return true
	}
	if loadout.Level < 1 || loadout.Level > levelCap ||
		loadout.Intellect < 1 || loadout.Intellect > benchmarkFixtureMaxIntellect ||
		len(loadout.Skills) > benchmarkFixtureMaxSkills {
		return false
	}
	for _, id := range loadout.Skills {
		if id == 0 {
			return false
		}
	}
	return true
}

/*
================
API.knownBenchmarkFixtureSkills

Every requested skill must exist in the server's skill data, so a typo is
refused here instead of failing later at login or cast. Without a resolver
no loadout skill can be checked, so none is accepted.
================
*/
func (api *API) knownBenchmarkFixtureSkills(loadout *benchmarkFixtureLoadout) bool {
	if loadout == nil || len(loadout.Skills) == 0 {
		return true
	}
	if api.skillGroup == nil {
		return false
	}
	for _, id := range loadout.Skills {
		if _, ok := api.skillGroup(id); !ok {
			return false
		}
	}
	return true
}

/*
================
benchmarkFixtureLoadoutMatches

Already at the loadout: the level, intellect, every requested skill as the
only rank of its group, and no stored MP or HP left to clamp the next login.
================
*/
func benchmarkFixtureLoadoutMatches(character *domain.Character, loadout *benchmarkFixtureLoadout, group SkillGroupResolver) bool {
	if loadout == nil {
		return true
	}
	if character.Level == nil || *character.Level != loadout.Level ||
		character.Intellect == nil || *character.Intellect != loadout.Intellect ||
		character.CurrentMP != nil || character.CurrentHP != nil ||
		character.MaxLevel == nil || *character.MaxLevel < loadout.Level {
		return false
	}
	want := mergeBenchmarkFixtureSkills(character.Skills, loadout.Skills, group)
	if len(want) != len(character.Skills) {
		return false
	}
	for index, id := range want {
		if character.Skills[index] != id {
			return false
		}
	}
	return true
}

/*
================
applyBenchmarkFixtureLoadout

Teaches the requested skills, replacing another rank of the same group so a
character keeps one current id per skill group, and clears stored vitals so
they are derived full at the next login.
================
*/
func applyBenchmarkFixtureLoadout(character *domain.Character, loadout *benchmarkFixtureLoadout, group SkillGroupResolver) {
	if loadout == nil {
		return
	}
	level, intellect := loadout.Level, loadout.Intellect
	character.Level = &level
	character.Intellect = &intellect
	// The highest level reached never sits below the current one.
	if character.MaxLevel == nil || *character.MaxLevel < level {
		maxLevel := level
		character.MaxLevel = &maxLevel
	}
	character.Skills = mergeBenchmarkFixtureSkills(character.Skills, loadout.Skills, group)
	character.CurrentMP = nil
	character.CurrentHP = nil
}

/*
================
mergeBenchmarkFixtureSkills

Known skills keep their order; a requested skill replaces a known skill of
its group in place, or is appended when its group is new. Skills whose
group cannot be resolved are kept as they are.
================
*/
func mergeBenchmarkFixtureSkills(known, requested []uint32, group SkillGroupResolver) []uint32 {
	merged := append([]uint32(nil), known...)
	for _, id := range requested {
		requestedGroup, ok := group(id)
		replaced := false
		for index, current := range merged {
			if current == id {
				replaced = true
				break
			}
			if currentGroup, known := group(current); ok && known && currentGroup == requestedGroup {
				merged[index] = id
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, id)
		}
	}
	return merged
}

/*
================
finiteInRange
================
*/
func finiteInRange(value, minimum, maximum float64) bool {
	return finite(value) && value >= minimum && value < maximum
}

/*
================
finite
================
*/
func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

/*
================
benchmarkFixtureWorldMatches
================
*/
func benchmarkFixtureWorldMatches(world *domain.CharacterWorld, request benchmarkFixtureResetRequest) bool {
	if world == nil || world.Spawn == nil || world.Spawn.RegionID == nil || world.Spawn.X == nil ||
		world.Spawn.Y == nil || world.Spawn.Z == nil || world.Spawn.Angle == nil || world.MovementMode == nil {
		return false
	}
	return *world.Spawn.RegionID == request.Spawn.RegionID && *world.Spawn.X == request.Spawn.X &&
		*world.Spawn.Y == request.Spawn.Y && *world.Spawn.Z == request.Spawn.Z &&
		*world.Spawn.Angle == request.Spawn.Angle && *world.MovementMode == request.MovementMode &&
		world.SpawnSet && world.MovementSourceSeeded && world.AuthoredAreaReturn == nil &&
		world.DungeonFloorIndex == nil && len(json.RawMessage(world.MoveSegment)) == 0
}
