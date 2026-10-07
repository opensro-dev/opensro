package agentapi

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"opensro.online/server/internal/domain"
)

// BenchmarkFixtureResetPath is routed through Agent to the selected
// GameWorld. The GameWorld registers it only when the explicit development
// gate is enabled, so a production worker has no teleport-shaped route.
const BenchmarkFixtureResetPath = "/development/benchmark-fixture/reset"

const (
	// benchmarkFixtureMaxLevel and benchmarkFixtureMaxIntellect bound a
	// loadout to values a real character can reach; benchmarkFixtureMaxSkills
	// bounds the list one request may teach.
	benchmarkFixtureMaxLevel     = 140
	benchmarkFixtureMaxIntellect = 2000
	benchmarkFixtureMaxSkills    = 64
)

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

type benchmarkFixtureResetRequest struct {
	CharacterName string                   `json:"characterName"`
	FixtureID     string                   `json:"fixtureId"`
	MovementMode  int64                    `json:"movementMode"`
	Spawn         benchmarkFixtureSpawn    `json:"spawn"`
	Loadout       *benchmarkFixtureLoadout `json:"loadout,omitempty"`
}

func (api *API) handleBenchmarkFixtureReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request benchmarkFixtureResetRequest
	if err := decodeJSONRequest(r.Body, &request); err != nil || !validBenchmarkFixtureReset(request) {
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
		if benchmarkFixtureWorldMatches(character.World, request) && benchmarkFixtureLoadoutMatches(character, request.Loadout) {
			outcome = "already-reset"
			return false
		}
		applyBenchmarkFixtureLoadout(character, request.Loadout)
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

func validBenchmarkFixtureReset(request benchmarkFixtureResetRequest) bool {
	return strings.TrimSpace(request.CharacterName) != "" &&
		strings.TrimSpace(request.FixtureID) != "" && len(strings.TrimSpace(request.FixtureID)) <= 128 &&
		request.Spawn.RegionID > 0 && request.Spawn.RegionID <= 0xffff &&
		finiteInRange(request.Spawn.X, 0, 1920) &&
		finiteInRange(request.Spawn.Z, 0, 1920) &&
		finite(request.Spawn.Y) && request.Spawn.Y >= -32768 && request.Spawn.Y <= 32767 &&
		request.Spawn.Angle >= 0 && request.Spawn.Angle <= 0xffff &&
		(request.MovementMode == 1 || request.MovementMode == 3) &&
		validBenchmarkFixtureLoadout(request.Loadout)
}

/*
================
validBenchmarkFixtureLoadout
================
*/
func validBenchmarkFixtureLoadout(loadout *benchmarkFixtureLoadout) bool {
	if loadout == nil {
		return true
	}
	if loadout.Level < 1 || loadout.Level > benchmarkFixtureMaxLevel ||
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
benchmarkFixtureLoadoutMatches

Already at the loadout: the level, intellect, every skill learned, and no
stored MP or HP left to clamp the next login.
================
*/
func benchmarkFixtureLoadoutMatches(character *domain.Character, loadout *benchmarkFixtureLoadout) bool {
	if loadout == nil {
		return true
	}
	if character.Level == nil || *character.Level != loadout.Level ||
		character.Intellect == nil || *character.Intellect != loadout.Intellect ||
		character.CurrentMP != nil || character.CurrentHP != nil ||
		character.MaxLevel == nil || *character.MaxLevel < loadout.Level {
		return false
	}
	learned := make(map[uint32]bool, len(character.Skills))
	for _, id := range character.Skills {
		learned[id] = true
	}
	for _, id := range loadout.Skills {
		if !learned[id] {
			return false
		}
	}
	return true
}

/*
================
applyBenchmarkFixtureLoadout

Teaches the missing skills without removing any the character knows, and
clears stored vitals so they are derived full at the next login.
================
*/
func applyBenchmarkFixtureLoadout(character *domain.Character, loadout *benchmarkFixtureLoadout) {
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
	learned := make(map[uint32]bool, len(character.Skills))
	for _, id := range character.Skills {
		learned[id] = true
	}
	for _, id := range loadout.Skills {
		if !learned[id] {
			character.Skills = append(character.Skills, id)
			learned[id] = true
		}
	}
	character.CurrentMP = nil
	character.CurrentHP = nil
}

func finiteInRange(value, minimum, maximum float64) bool {
	return finite(value) && value >= minimum && value < maximum
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

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
