/*
===========================================================================

character_namefilter_test.go - server admission through the native name filter

Exercises both authenticated name prechecks and direct character creation.
The UTF-16 fixture uses the production parser without licensed game data.

===========================================================================
*/

package agentapi

import (
	"strings"
	"testing"
	"unicode/utf16"

	"opensro.online/server/internal/game/namefilter"
)

/*
================
testNameFilter

Keeps the native ASCII ranges and harmless whole-word / substring entries.
================
*/
func testNameFilter(t *testing.T) *namefilter.Filter {
	t.Helper()
	rows := []string{
		"#ALLOW_ID_TABLE\t0x0030\t0x0039",
		"#ALLOW_ID_TABLE\t0x0041\t0x005A",
		"#ALLOW_ID_TABLE\t0x005F\t0x005F",
		"#ALLOW_ID_TABLE\t0x0061\t0x007A",
		"forbidden\t2\t0\t0\t0",
		"reserved\t1\t0\t0\t0",
	}
	units := utf16.Encode([]rune(strings.Join(rows, "\r\n")))
	data := []byte{0xff, 0xfe}
	for _, unit := range units {
		data = append(data, byte(unit), byte(unit>>8))
	}
	filter, err := namefilter.Parse(data)
	if err != nil {
		t.Fatalf("namefilter.Parse: %v", err)
	}
	return filter
}

/*
================
TestCharacterNameFilterAdmission

Create is exercised independently, so a skipped client precheck cannot
persist a name refused by the filter or the native ASCII shape.
================
*/
func TestCharacterNameFilterAdmission(t *testing.T) {
	cases := []struct {
		name    string
		allowed bool
	}{
		{name: "Hero_01", allowed: true},
		{name: "FoRbIdDeNX"},
		{name: "ReSeRvEd"},
		{name: "ReservedX", allowed: true},
		{name: "Ab", allowed: true},
		{name: "TwelveChars1", allowed: true},
		{name: "A"},
		{name: "ThirteenChars"},
		{name: "Hero!"},
		{name: "Ünicode"},
	}
	for _, route := range []string{"/character/name-overlap", "/character/create"} {
		t.Run(route, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					api, authority := newTestAPI(t)
					handler := authenticatedHandler(t, api, testAccount)
					body := createBody(tc.name)
					if route == "/character/name-overlap" {
						body = map[string]any{"characterName": tc.name}
					}
					response := postJSON(t, handler, route, body)
					wantResult := float64(0)
					if tc.allowed {
						wantResult = 1
					}
					if response["nativeResult"] != wantResult {
						t.Fatalf("response = %v, want nativeResult %v", response, wantResult)
					}
					if !tc.allowed && response["nativeErrorCode"] != float64(errCodeInvalidName) {
						t.Fatalf("refusal = %v, want invalid-name code %#x", response, errCodeInvalidName)
					}
					wantCount := 0
					if route == "/character/create" {
						requireCharacterRosterContractVersion(t, response)
						if tc.allowed {
							wantCount = 1
						}
					}
					characters := authority.Characters().CharactersForDivision(testDivision)
					if len(characters) != wantCount {
						t.Fatalf("persisted characters = %d, want %d", len(characters), wantCount)
					}
					if wantCount == 1 && characters[0].Name != tc.name {
						t.Fatalf("persisted name = %q, want %q", characters[0].Name, tc.name)
					}
				})
			}
		})
	}
}

/*
================
TestCharacterNameFilterPreservesOverlapRefusal
================
*/
func TestCharacterNameFilterPreservesOverlapRefusal(t *testing.T) {
	api, authority := newTestAPI(t)
	handler := authenticatedHandler(t, api, testAccount)
	created := postJSON(t, handler, "/character/create", createBody("TakenHero"))
	if created["nativeResult"] != float64(1) {
		t.Fatalf("create = %v", created)
	}
	for _, route := range []string{"/character/name-overlap", "/character/create"} {
		body := createBody("takenhero")
		if route == "/character/name-overlap" {
			body = map[string]any{"characterName": "takenhero"}
		}
		response := postJSON(t, handler, route, body)
		if response["nativeResult"] != float64(0) ||
			response["nativeErrorCode"] != float64(errCodeNameOverlap) {
			t.Fatalf("%s response = %v, want overlap refusal", route, response)
		}
		if route == "/character/create" {
			requireCharacterRosterContractVersion(t, response)
		}
	}
	characters := authority.Characters().CharactersForDivision(testDivision)
	if len(characters) != 1 || characters[0].Name != "TakenHero" {
		t.Fatalf("overlap changed roster = %+v", characters)
	}
}

/*
================
TestConstructionRequiresNameFilter
================
*/
func TestConstructionRequiresNameFilter(t *testing.T) {
	api, authority := newTestAPI(t)
	config := Config{
		Store:                  authority,
		CharacterPresentation:  api.characterPresentation,
		CharacterCreationValid: api.characterCreationValid,
		ShardID:                api.workerShardID,
		AgentSessionVerifier:   api.agentSessionVerifier,
		EnterWorldAuthSecret:   api.enterWorldAuthSecret,
		Readiness:              api.readiness,
		Now:                    api.now,
	}
	if _, err := New(config); err == nil {
		t.Fatal("New succeeded without a name filter")
	}
	config.CharacterNameAllowed = testNameFilter(t).Allowed
	if _, err := New(config); err != nil {
		t.Fatalf("same config with a name filter: %v", err)
	}
}
