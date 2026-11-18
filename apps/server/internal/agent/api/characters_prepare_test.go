/*
===========================================================================

characters_prepare_test.go - the creation door's prepare hook

The PVP shard finishes a character before it is stored; every other shard
creates it exactly as the native rules do.

===========================================================================
*/

package agentapi

import (
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/platform/readiness"
)

/*
================
newPrepareTestAPI

The package's test API with a store inventory seed that records whether
it ran and the gold it found.
================
*/
func newPrepareTestAPI(t *testing.T, prepare func(*domain.Character) error, seededGold *int64) (*API, *store.Store) {
	t.Helper()
	now := func() time.Time { return time.UnixMilli(testNowUnixMillis) }
	authority, err := store.Open(t.TempDir(), store.Options{
		Now:           now,
		DefaultSkills: agentapiSkillSeeder,
		DefaultInventory: func(c *domain.Character) {
			if c.Gold != nil {
				*seededGold = *c.Gold
			}
		},
	})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)
	ready := readiness.NewGate()
	api, err := New(Config{
		Store:                 authority,
		CharacterPresentation: testCharacterPresentation,
		CharacterCreationValid: func(character *domain.Character) bool {
			return enterworld.CharacterCreationValid(character, testCharacterRoster())
		},
		CharacterNameAllowed: testNameFilter(t).Allowed,
		PrepareCharacter:     prepare,
		ShardID:              testDivision,
		AgentSessionVerifier: testAgentSessionVerifier(t),
		EnterWorldAuthSecret: []byte(testEnterWorldSecret),
		Readiness:            ready,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ready.Open()
	return api, authority
}

/*
================
TestCreateRunsThePrepareHook
================
*/
func TestCreateRunsThePrepareHook(t *testing.T) {
	seededGold := int64(-1)
	prepare := func(c *domain.Character) error {
		level, gold := int64(110), int64(100_000_000)
		c.Level, c.Gold = &level, &gold
		return nil
	}
	api, authority := newPrepareTestAPI(t, prepare, &seededGold)
	handler := authenticatedHandler(t, api, testAccount)

	response := postJSON(t, handler, "/character/create", createBody("tester123"))
	if response["nativeResult"] != float64(1) {
		t.Fatalf("create response = %v", response)
	}
	characters := authority.Characters().CharactersForDivision(testDivision)
	if len(characters) != 1 || *characters[0].Level != 110 || *characters[0].Gold != 100_000_000 {
		t.Fatalf("stored %+v", characters)
	}
	if seededGold != 100_000_000 {
		t.Fatalf("the inventory seed ran with gold %d: prepare must run before the store seeds", seededGold)
	}
}

/*
================
TestCreateWithoutHookIsNative
================
*/
func TestCreateWithoutHookIsNative(t *testing.T) {
	seededGold := int64(-1)
	api, authority := newPrepareTestAPI(t, nil, &seededGold)
	handler := authenticatedHandler(t, api, testAccount)

	postJSON(t, handler, "/character/create", createBody("tester123"))
	characters := authority.Characters().CharactersForDivision(testDivision)
	if len(characters) != 1 || *characters[0].Level != 1 || *characters[0].Gold != 0 {
		t.Fatalf("stored %+v", characters)
	}
}

/*
================
TestCreateRefusesWhenPrepareFails
================
*/
func TestCreateRefusesWhenPrepareFails(t *testing.T) {
	seededGold := int64(-1)
	prepare := func(*domain.Character) error { return errors.New("no skill table") }
	api, authority := newPrepareTestAPI(t, prepare, &seededGold)
	handler := authenticatedHandler(t, api, testAccount)

	response := postJSON(t, handler, "/character/create", createBody("tester123"))
	if response["nativeResult"] != float64(0) || response["nativeErrorCode"] != float64(errCodeInvalidChargen) {
		t.Fatalf("create response = %v, want the invalid-chargen refusal", response)
	}
	if got := len(authority.Characters().CharactersForDivision(testDivision)); got != 0 {
		t.Fatalf("refused create persisted %d character(s)", got)
	}
}
