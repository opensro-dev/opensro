package agentapi

import (
	"net/http"
	"net/http/httptest"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/security/auth"
	"strings"
	"testing"
)

func passiveFixtureReport(*domain.Character) (map[string]any, error) {
	return map[string]any{}, nil
}

func TestPassiveFixtureAdmissionBoundaries(t *testing.T) {
	for _, name := range []string{"PassiveProbe", "PowerProbe"} {
		t.Run(name, func(t *testing.T) { testPassiveFixtureAdmission(t, name) })
	}
}

func testPassiveFixtureAdmission(t *testing.T, name string) {
	api, authority := newTestAPI(t)
	post := func(h http.Handler, name string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", PassiveCriticalFixturePath, strings.NewReader(`{"characterName":"`+name+`","command":"seed"}`))
		declareBrowser(r)
		h.ServeHTTP(w, r)
		return w.Code
	}
	api.InstallPassiveCriticalFixture(passiveFixtureReport)
	if got := post(api.Handler(), name); got != 404 {
		t.Fatalf("disabled=%d", got)
	}
	api.benchmarkFixtureControl = true
	api.InstallPassiveCriticalFixture(passiveFixtureReport)
	if got := post(api.Handler(), name); got != 401 {
		t.Fatalf("unauthenticated=%d", got)
	}
	if got := post(authenticatedHandler(t, api, testAccount), name); got != 400 {
		t.Fatalf("wrong shard=%d", got)
	}
	api.workerShardID = "test"
	token, err := auth.MintAgentSession(testAgentKeyID, testAgentPrivateKey, testAccount, "test", api.now().Add(auth.AgentSessionLifetime))
	if err != nil {
		t.Fatal(err)
	}
	h := bearerHandler(api.Handler(), token)
	if got := post(h, "asd"); got != 400 {
		t.Fatalf("protected name=%d", got)
	}
	if got := post(h, name); got != 404 {
		t.Fatalf("unowned actor=%d", got)
	}
	level, race, strength, intellect := int64(1), int64(0), int64(20), int64(20)
	seed := &domain.Character{Name: name, ModelCodename: "CHAR_EU_MAN_NOBLE", RaceIndex: &race, Level: &level, Strength: &strength, Intellect: &intellect}
	if err := authority.CreateCharacter("test", testAccount, seed); err != nil {
		t.Fatal(err)
	}
	foreign, err := auth.MintAgentSession(testAgentKeyID, testAgentPrivateKey, "other-account", "test", api.now().Add(auth.AgentSessionLifetime))
	if err != nil {
		t.Fatal(err)
	}
	if got := post(bearerHandler(api.Handler(), foreign), name); got != 404 {
		t.Fatalf("foreign account=%d", got)
	}
	api.acquireCharacterControl = func(string, string) (func(), bool) { return nil, false }
	if got := post(h, name); got != 409 {
		t.Fatalf("in-play actor=%d", got)
	}
	api.acquireCharacterControl = nil
	if got := post(h, name); got != 200 {
		t.Fatalf("seed=%d", got)
	}
	if *seed.Level != 30 || *seed.SkillPoints != 1000000 {
		t.Fatal("initial seed missing")
	}
	for _, id := range seed.Skills {
		if id >= 7522 && id <= 7529 {
			t.Fatal("fixture granted learned passive")
		}
	}
	authority.UpdateCharacter(seed, "test spend", func() bool { spent := int64(100); seed.SkillPoints = &spent; return true })
	if got := post(h, name); got != 200 || *seed.SkillPoints != 100 {
		t.Fatalf("repeat seed refilled points: %d %+v", got, seed.SkillPoints)
	}
}
