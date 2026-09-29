package agentapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
)

const (
	testDivision         = "global-official"
	testAccount          = "test-account"
	testEnterWorldSecret = "test-enterworld-secret-at-least-32-bytes"
	testNowUnixMillis    = int64(1_785_000_000_000)
)

var (
	testAgentPrivateKey = ed25519.NewKeyFromSeed(bytes.Repeat(
		[]byte{0x5a},
		ed25519.SeedSize,
	))
	testAgentPublicKey = testAgentPrivateKey.Public().(ed25519.PublicKey)
	testAgentKeyID     = fmt.Sprintf(
		"%x",
		sha256.Sum256(testAgentPublicKey),
	)[:32]
)

func testAgentSessionVerifier(t *testing.T) *auth.AgentSessionVerifier {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-session-public-keys.json")
	payload := fmt.Sprintf(
		"{\"keys\":[{\"id\":%q,\"publicKey\":%q,\"createdAt\":\"2026-07-30T00:00:00Z\"}]}\n",
		testAgentKeyID,
		base64.RawURLEncoding.EncodeToString(testAgentPublicKey),
	)
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	verifier, err := auth.NewAgentSessionVerifier(path)
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}

func agentapiSkillSeeder(
	raceKey string,
	learned []uint32,
) ([]uint32, error) {
	ids := []uint32{1, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970}
	if raceKey == enterworld.RaceKeyChina {
		ids = []uint32{1, 2, 40, 70}
	}
	have := make(map[uint32]bool, len(learned))
	for _, id := range learned {
		have[id] = true
	}
	out := append([]uint32(nil), learned...)
	for _, id := range ids {
		if !have[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func testCharacterRoster() *enterworld.Roster {
	return &enterworld.Roster{Models: []enterworld.RosterModel{
		{Codename: "CHAR_CH_MAN_ADVENTURER", RefObjID: 1907, BodyRadius: 4},
		{Codename: "CHAR_EU_MAN_ADVENTURER", RefObjID: 14726, BodyRadius: 4},
	}}
}

func testCharacterPresentation(character *domain.Character) CharacterPresentation {
	entry := enterworld.ResolveLocalPlayerEntry(character, testCharacterRoster())
	raceIndex := domain.RaceEurope
	if entry.RaceKey == enterworld.RaceKeyChina {
		raceIndex = domain.RaceChina
	}
	gender := domain.GenderFemale
	if entry.SexSelector1AC != 0 {
		gender = domain.GenderMale
	}
	loadout := entry.VisualLoadout
	return CharacterPresentation{
		RaceIndex: raceIndex,
		Gender:    gender,
		VisualLoadout: CharacterVisualLoadout{
			ModelCodename:    loadout.ModelCodename,
			Items:            characterItemsForTest(loadout.Items),
			Avatars:          characterItemsForTest(loadout.Avatars),
			AnimationSetName: loadout.AnimationSetName,
			HeightScale:      loadout.HeightScale,
			VolumeScale:      loadout.VolumeScale,
		},
	}
}

func newTestAPI(t *testing.T) (*API, *store.Store) {
	return newTestAPIWithSkillSeeder(t, agentapiSkillSeeder)
}

func newTestAPIWithSkillSeeder(
	t *testing.T,
	seeder func(string, []uint32) ([]uint32, error),
) (*API, *store.Store) {
	t.Helper()
	now := func() time.Time { return time.UnixMilli(testNowUnixMillis) }
	authority, err := store.Open(t.TempDir(), store.Options{
		Now:           now,
		DefaultSkills: seeder,
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

func TestCharacterCreateDoesNotMisclassifyInternalCodenameFailureAsInvalidName(t *testing.T) {
	brokenSkills := func(string, []uint32) ([]uint32, error) {
		return nil, fmt.Errorf("default skill codename does not resolve")
	}
	api, authority := newTestAPIWithSkillSeeder(t, brokenSkills)
	handler := authenticatedHandler(t, api, testAccount)

	response := postJSON(t, handler, "/character/create", createBody("tester123"))
	if response["nativeResult"] != float64(0) ||
		response["nativeErrorCode"] != float64(errCodeCreateFailed) {
		t.Fatalf("create response = %v, want generic create failure", response)
	}
	if got := len(authority.Characters().CharactersForDivision(testDivision)); got != 0 {
		t.Fatalf("failed create persisted %d character(s), want 0", got)
	}
}

func authenticatedHandler(
	t *testing.T,
	api *API,
	accountID string,
) http.Handler {
	t.Helper()
	if accountID == "" {
		accountID = testAccount
	}
	token, err := auth.MintAgentSession(
		testAgentKeyID,
		testAgentPrivateKey,
		accountID,
		testDivision,
		api.now().Add(auth.AgentSessionLifetime),
	)
	if err != nil {
		t.Fatalf("MintAgentSession: %v", err)
	}
	return bearerHandler(api.Handler(), token)
}

func bearerHandler(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
		next.ServeHTTP(w, r)
	})
}

func TestReadinessStopsNewGameWorldRequests(t *testing.T) {
	api, _ := newTestAPI(t)

	health := httptest.NewRecorder()
	api.Handler().ServeHTTP(
		health,
		httptest.NewRequest(http.MethodGet, readiness.PathHealth, nil),
	)
	if health.Code != http.StatusOK {
		t.Fatalf("health = %d: %s", health.Code, health.Body)
	}

	api.readiness.Close()
	characters := httptest.NewRecorder()
	authenticatedHandler(t, api, "").ServeHTTP(
		characters,
		httptest.NewRequest(http.MethodGet, "/character/list", nil),
	)
	if characters.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"character admission while draining = %d: %s",
			characters.Code,
			characters.Body,
		)
	}
	var body map[string]any
	if err := json.Unmarshal(characters.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "PROCESS_DRAINING" {
		t.Fatalf("draining refusal = %#v", body)
	}
	notReady := httptest.NewRecorder()
	api.Handler().ServeHTTP(
		notReady,
		httptest.NewRequest(http.MethodGet, readiness.PathReady, nil),
	)
	if notReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness while draining = %d", notReady.Code)
	}
}

func postJSON(
	t *testing.T,
	handler http.Handler,
	path string,
	body any,
) map[string]any {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST %s = %d: %s", path, recorder.Code, recorder.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("POST %s response: %v", path, err)
	}
	return out
}

func getJSON(
	t *testing.T,
	handler http.Handler,
	path string,
	out any,
) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, path, nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), out); err != nil {
		t.Fatalf("GET %s response: %v", path, err)
	}
}

func createBody(name string) map[string]any {
	return map[string]any{
		"characterName":  name,
		"modelCodename":  "CHAR_CH_MAN_ADVENTURER",
		"heightIndex":    0,
		"volumeIndex":    0,
		"weaponIndex":    1,
		"protectorIndex": 0,
		"armorSelected":  false,
		"weaponSelected": true,
	}
}

func requireCharacterRosterContractVersion(t *testing.T, response map[string]any) {
	t.Helper()
	if response["characterRosterContractVersion"] != float64(CharacterRosterContractVersion) {
		t.Fatalf(
			"CharacterRosterContractVersion = %v, want %d",
			response["characterRosterContractVersion"],
			CharacterRosterContractVersion,
		)
	}
}

func TestCharacterVisualLoadoutWireCollectionsAreNeverNull(t *testing.T) {
	payload, err := json.Marshal(characterVisualLoadoutForWire(CharacterVisualLoadout{}))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"items", "avatars"} {
		if _, ok := decoded[key].([]any); !ok {
			t.Fatalf("%s = %#v, want JSON array", key, decoded[key])
		}
	}
}

/*
================
characterItemsForTest
================
*/
func characterItemsForTest(items []enterworld.VisualItem) []CharacterItem {
	out := make([]CharacterItem, 0, len(items))
	for _, item := range items {
		out = append(out, CharacterItem{RefObjID: item.RefObjID, Plus: item.Plus})
	}
	return out
}

func TestCharacterLifecycleUsesOwnedShardAndAccount(t *testing.T) {
	api, authority := newTestAPI(t)
	handler := authenticatedHandler(t, api, "alice")

	created := postJSON(t, handler, "/character/create", createBody("AliceHero"))
	if created["nativeResult"] != float64(1) {
		t.Fatalf("create = %v", created)
	}
	requireCharacterRosterContractVersion(t, created)
	characters := authority.Characters().CharactersForDivision(testDivision)
	if len(characters) != 1 || characters[0].AccountID != "alice" {
		t.Fatalf("stored characters = %+v", characters)
	}

	var listed map[string]any
	getJSON(t, handler, "/character/list", &listed)
	requireCharacterRosterContractVersion(t, listed)
	if got := len(listed["characters"].([]any)); got != 1 {
		t.Fatalf("list count = %d, want 1", got)
	}

	started := postJSON(t, handler, "/agent/packet", map[string]any{
		"nativeOpcode":  selectStartReqOpcode,
		"characterName": "AliceHero",
	})
	if started["nativeOpcode"] != float64(selectStartRespOpcode) ||
		started["nextScene"] != nextSceneMission {
		t.Fatalf("select start = %v", started)
	}

	deleted := postJSON(t, handler, "/character/delete-action", map[string]any{
		"action": 3, "characterName": "AliceHero",
	})
	requireCharacterRosterContractVersion(t, deleted)
	if deleted["nativeResult"] != float64(1) || deleted["character"] == nil {
		t.Fatalf("delete = %v", deleted)
	}

	duplicate := postJSON(t, handler, "/character/create", createBody("AliceHero"))
	requireCharacterRosterContractVersion(t, duplicate)
	if duplicate["nativeResult"] != float64(0) {
		t.Fatalf("duplicate create = %v", duplicate)
	}
}

func TestCharacterListProjectsOneIdentityForAnInconsistentLegacyRecord(t *testing.T) {
	api, authority := newTestAPI(t)
	handler := authenticatedHandler(t, api, "alice")
	created := postJSON(t, handler, "/character/create", createBody("LegacyHero"))
	if created["nativeResult"] != float64(1) {
		t.Fatalf("create = %v", created)
	}

	character := authority.Characters().CharactersForDivision(testDivision)[0]
	authority.MutateCharacter(character, "seed inconsistent legacy appearance", func() {
		wrongRace := int64(domain.RaceEurope)
		character.RaceIndex = &wrongRace
		character.AnimationSetName = "dagger"
	})

	var listed map[string]any
	getJSON(t, handler, "/character/list", &listed)
	requireCharacterRosterContractVersion(t, listed)
	rows := listed["characters"].([]any)
	row := rows[0].(map[string]any)
	if row["raceIndex"] != float64(domain.RaceChina) {
		t.Fatalf("raceIndex = %v, want codename-owned China", row["raceIndex"])
	}
	loadout := row["visualLoadout"].(map[string]any)
	if loadout["modelCodename"] != "CHAR_CH_MAN_ADVENTURER" {
		t.Fatalf("modelCodename = %v", loadout["modelCodename"])
	}
	if loadout["animationSetName"] != "sword" {
		t.Fatalf("animationSetName = %v, want canonical sword", loadout["animationSetName"])
	}
}

func TestConstructionRequiresWorkerIdentityAndSecrets(t *testing.T) {
	authority, err := store.Open(t.TempDir(), store.Options{
		DefaultSkills: agentapiSkillSeeder,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)

	cases := []Config{
		{Store: authority},
		{Store: authority, ShardID: testDivision},
		{
			Store:                authority,
			ShardID:              testDivision,
			AgentSessionVerifier: testAgentSessionVerifier(t),
		},
	}
	for index, config := range cases {
		if _, err := New(config); err == nil {
			t.Fatalf("incomplete config %d started", index)
		}
	}
}
