package agentapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opensro.online/server/internal/domain"
)

func benchmarkFixtureRequest(characterName string) map[string]any {
	return map[string]any{
		"characterName": characterName,
		"fixtureId":     "europe-field-ordinary-run-v1",
		"movementMode":  3,
		"spawn": map[string]any{
			"regionId": 0x6e4a,
			"x":        1756.0,
			"y":        7.0,
			"z":        525.0,
			"angle":    0x4000,
		},
	}
}

func postBenchmarkFixtureReset(t *testing.T, handler http.Handler, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, BenchmarkFixtureResetPath, bytes.NewReader(payload))
	declareBrowser(request)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var response map[string]any
	if recorder.Body.Len() > 0 && recorder.Header().Get("Content-Type") == "application/json" {
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
	}
	return recorder, response
}

func TestBenchmarkFixtureResetRouteIsAbsentByDefault(t *testing.T) {
	api, _ := newTestAPI(t)
	recorder, _ := postBenchmarkFixtureReset(t, authenticatedHandler(t, api, testAccount), benchmarkFixtureRequest("FixtureHero"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("disabled fixture route = %d, want 404", recorder.Code)
	}
}

func TestBenchmarkFixtureResetIsAuthenticatedExactIdempotentAndStateScoped(t *testing.T) {
	api, authority := newTestAPI(t)
	api.benchmarkFixtureControl = true
	handler := authenticatedHandler(t, api, testAccount)
	postJSON(t, handler, "/character/create", createBody("FixtureHero"))
	character := authority.Characters().CharactersForDivision(testDivision)[0]
	rebirthRegion, returnRegion, dungeonFloor := int64(0x62a8), int64(0x7e7e), int64(4)
	oldRegion, oldAngle, oldMode := int64(0x6d49), int64(7), int64(1)
	oldX, oldY, oldZ := 1639.0, -10.0, 1150.0
	authority.MutateCharacter(character, "seed benchmark fixture reset", func() {
		character.World = &domain.CharacterWorld{
			Spawn:              &domain.WorldSpawn{RegionID: &oldRegion, X: &oldX, Y: &oldY, Z: &oldZ, Angle: &oldAngle},
			AuthoredAreaReturn: &domain.WorldSpawn{RegionID: &returnRegion},
			RebirthPoint:       &domain.WorldSpawn{RegionID: &rebirthRegion},
			MovementMode:       &oldMode,
			DungeonFloorIndex:  &dungeonFloor,
			UpdatedAt:          "preserve-me",
			MoveSegment:        json.RawMessage(`{"moving":true}`),
		}
	})

	recorder, response := postBenchmarkFixtureReset(t, handler, benchmarkFixtureRequest("FixtureHero"))
	if recorder.Code != http.StatusOK || response["ok"] != true || response["outcome"] != "reset" {
		t.Fatalf("fixture reset = %d %#v", recorder.Code, response)
	}
	authority.ReadCharacters(testDivision, func([]*domain.Character) {
		world := character.World
		if world == nil || !worldSpawnIsSettled(world.Spawn) || *world.Spawn.RegionID != 0x6e4a ||
			*world.Spawn.X != 1756 || *world.Spawn.Y != 7 || *world.Spawn.Z != 525 || *world.Spawn.Angle != 0x4000 ||
			world.MovementMode == nil || *world.MovementMode != 3 || !world.SpawnSet || !world.MovementSourceSeeded {
			t.Fatalf("fixture world not reset exactly: %+v", world)
		}
		if world.AuthoredAreaReturn != nil || world.DungeonFloorIndex != nil || len(world.MoveSegment) != 0 {
			t.Fatalf("fixture reset retained transient world state: %+v", world)
		}
		if world.RebirthPoint == nil || world.RebirthPoint.RegionID == nil || *world.RebirthPoint.RegionID != rebirthRegion ||
			world.UpdatedAt != "preserve-me" {
			t.Fatalf("fixture reset dropped unrelated world state: %+v", world)
		}
	})

	recorder, response = postBenchmarkFixtureReset(t, handler, benchmarkFixtureRequest("FixtureHero"))
	if recorder.Code != http.StatusOK || response["outcome"] != "already-reset" {
		t.Fatalf("idempotent fixture reset = %d %#v", recorder.Code, response)
	}

	foreign := authenticatedHandler(t, api, "different-account")
	recorder, response = postBenchmarkFixtureReset(t, foreign, benchmarkFixtureRequest("FixtureHero"))
	if recorder.Code != http.StatusNotFound || response["code"] != "UNKNOWN_CHARACTER" {
		t.Fatalf("foreign fixture reset = %d %#v", recorder.Code, response)
	}
}

func TestBenchmarkFixtureResetRefusesLiveCharacterAndInvalidCoordinates(t *testing.T) {
	api, _ := newTestAPI(t)
	api.benchmarkFixtureControl = true
	handler := authenticatedHandler(t, api, testAccount)
	postJSON(t, handler, "/character/create", createBody("FixtureHero"))

	api.acquireCharacterControl = func(string, string) (func(), bool) { return nil, false }
	recorder, response := postBenchmarkFixtureReset(t, handler, benchmarkFixtureRequest("FixtureHero"))
	if recorder.Code != http.StatusConflict || response["code"] != "CHARACTER_IN_PLAY" || recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf("live fixture reset = %d %#v", recorder.Code, response)
	}

	api.acquireCharacterControl = nil
	invalid := benchmarkFixtureRequest("FixtureHero")
	invalid["spawn"].(map[string]any)["x"] = 1920
	recorder, response = postBenchmarkFixtureReset(t, handler, invalid)
	if recorder.Code != http.StatusBadRequest || response["code"] != "BAD_REQUEST" {
		t.Fatalf("invalid fixture reset = %d %#v", recorder.Code, response)
	}
}

func TestBenchmarkFixtureResetRequiresSessionWhenEnabled(t *testing.T) {
	api, _ := newTestAPI(t)
	api.benchmarkFixtureControl = true
	recorder, _ := postBenchmarkFixtureReset(t, api.Handler(), benchmarkFixtureRequest("FixtureHero"))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated fixture reset = %d, want 401", recorder.Code)
	}
}

/*
================
TestBenchmarkFixtureLoadoutTeachesSkillsAndDerivesVitals

A probe character gets what a scenario needs (Ghost Walk 114 and 19639 at
a level and intellect that can pay their MP) without losing a skill it
knew; stored vitals are cleared so login derives them full; a repeat is a
no-op; an out-of-range loadout is refused.
================
*/
func TestBenchmarkFixtureLoadoutTeachesSkillsAndDerivesVitals(t *testing.T) {
	api, authority := newTestAPI(t)
	api.benchmarkFixtureControl = true
	handler := authenticatedHandler(t, api, testAccount)
	postJSON(t, handler, "/character/create", createBody("FixtureHero"))
	character := authority.Characters().CharactersForDivision(testDivision)[0]
	hp, mp, maxLevel := int64(40), int64(25), int64(3)
	authority.MutateCharacter(character, "seed loadout", func() {
		character.Skills = []uint32{1, 2, 114}
		character.CurrentHP, character.CurrentMP, character.MaxLevel = &hp, &mp, &maxLevel
	})
	body := benchmarkFixtureRequest("FixtureHero")
	body["loadout"] = map[string]any{"level": 90, "intellect": 109, "skills": []uint32{114, 19639}}

	recorder, response := postBenchmarkFixtureReset(t, handler, body)
	if recorder.Code != http.StatusOK || response["outcome"] != "reset" {
		t.Fatalf("loadout reset = %d %#v", recorder.Code, response)
	}
	authority.ReadCharacters(testDivision, func([]*domain.Character) {
		if character.Level == nil || *character.Level != 90 || character.Intellect == nil || *character.Intellect != 109 ||
			character.MaxLevel == nil || *character.MaxLevel != 90 {
			t.Fatalf("level/intellect/maxLevel = %v/%v/%v", character.Level, character.Intellect, character.MaxLevel)
		}
		if got := character.Skills; len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 114 || got[3] != 19639 {
			t.Fatalf("skills = %v, want known skills kept and 19639 added once", got)
		}
		if character.CurrentHP != nil || character.CurrentMP != nil {
			t.Fatalf("stored vitals kept: hp %v mp %v", character.CurrentHP, character.CurrentMP)
		}
	})
	if recorder, response = postBenchmarkFixtureReset(t, handler, body); recorder.Code != http.StatusOK || response["outcome"] != "already-reset" {
		t.Fatalf("repeat = %d %#v, want already-reset", recorder.Code, response)
	}
	for _, loadout := range []map[string]any{
		{"level": 0, "intellect": 109, "skills": []uint32{114}},
		{"level": 90, "intellect": 0, "skills": []uint32{114}},
		{"level": benchmarkFixtureMaxLevel + 1, "intellect": 109, "skills": []uint32{114}},
		{"level": 90, "intellect": 109, "skills": []uint32{0}},
	} {
		body["loadout"] = loadout
		if recorder, _ := postBenchmarkFixtureReset(t, handler, body); recorder.Code != http.StatusBadRequest {
			t.Fatalf("loadout %v = %d, want 400", loadout, recorder.Code)
		}
	}
}
