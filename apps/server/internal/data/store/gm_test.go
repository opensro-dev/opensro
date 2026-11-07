package store

import (
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// TestGMPrivilegePersistsAcrossReopen: the flag survives a full store
// round-trip (commit through the scoped door, close, reopen), and a
// non-GM record never carries the key at all (omitempty).
func TestGMPrivilegePersistsAcrossReopen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	c := &enterworld.Character{Name: "GmProbe"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	if raw := readCharacterRecordRaw(t, s, testDivision, "gmprobe"); strings.Contains(raw, "gmPrivilege") {
		t.Fatalf("non-GM record carries the gmPrivilege key: %s", raw)
	}
	s.MutateCharacter(c, "gm-grant test", func() { c.GMPrivilege = true })
	s.Close()

	reopened := openTest(t, dir, clock)
	found := false
	reopened.ReadCharacters(testDivision, func(characters []*enterworld.Character) {
		for _, rc := range characters {
			if rc.Name == "GmProbe" {
				found = true
				if !rc.GMPrivilege {
					t.Error("GM privilege did not survive the reopen")
				}
			}
		}
	})
	if !found {
		t.Fatal("character missing after reopen")
	}
}

// TestGMPrivilegeAbsentKeyDecodesNonGM pins the current optional-field
// semantics under the strict decoder.
func TestGMPrivilegeAbsentKeyDecodesNonGM(t *testing.T) {
	t.Parallel()

	ordinary, err := decodeCharacterStrict([]byte(`{"id":1,"accountId":"test-account","name":"Ordinary"}`))
	if err != nil {
		t.Fatalf("decoding a keyless record: %v", err)
	}
	if ordinary.GMPrivilege {
		t.Error("record without the gmPrivilege key decoded as GM")
	}
	promoted, err := decodeCharacterStrict([]byte(`{"id":2,"accountId":"test-account","name":"Gm","gmPrivilege":true}`))
	if err != nil {
		t.Fatalf("decoding a promoted record: %v", err)
	}
	if !promoted.GMPrivilege {
		t.Error("record with gmPrivilege:true decoded as non-GM")
	}
}

// TestReconcileGMPrivilegeAllowlistIsSoleAuthority: listed names promote
// (case-insensitively), unlisted GMs demote, the result persists, and a
// second run with the same list is a no-op.
func TestReconcileGMPrivilegeAllowlistIsSoleAuthority(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	for _, name := range []string{"GmAlpha", "PlainBeta"} {
		if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: name}); err != nil {
			t.Fatalf("CreateCharacter(%s): %v", name, err)
		}
	}

	promoted, demoted := s.ReconcileGMPrivilege([]GMIdentity{
		{DivisionID: testDivision, CharacterName: "gmalpha"},
		{DivisionID: testDivision, CharacterName: "NoSuchName"},
	})
	if len(promoted) != 1 || promoted[0] != testDivision+":GmAlpha" || len(demoted) != 0 {
		t.Fatalf("first reconcile = promoted %v demoted %v, want [%s:GmAlpha] []", promoted, demoted, testDivision)
	}
	if raw := readCharacterRecordRaw(t, s, testDivision, "gmalpha"); !strings.Contains(raw, `"gmPrivilege":true`) {
		t.Fatalf("promoted record not persisted: %s", raw)
	}
	if raw := readCharacterRecordRaw(t, s, testDivision, "plainbeta"); strings.Contains(raw, "gmPrivilege") {
		t.Fatalf("unlisted record grew the gmPrivilege key: %s", raw)
	}

	promoted, demoted = s.ReconcileGMPrivilege([]GMIdentity{{
		DivisionID: testDivision, CharacterName: "GmAlpha",
	}})
	if len(promoted) != 0 || len(demoted) != 0 {
		t.Fatalf("idempotent reconcile = promoted %v demoted %v, want none", promoted, demoted)
	}

	// The allowlist is the sole authority: dropping the name demotes.
	promoted, demoted = s.ReconcileGMPrivilege(nil)
	if len(promoted) != 0 || len(demoted) != 1 || demoted[0] != testDivision+":GmAlpha" {
		t.Fatalf("empty-list reconcile = promoted %v demoted %v, want [] [%s:GmAlpha]", promoted, demoted, testDivision)
	}
	if raw := readCharacterRecordRaw(t, s, testDivision, "gmalpha"); strings.Contains(raw, "gmPrivilege") {
		t.Fatalf("demoted record still carries the key: %s", raw)
	}

	// The demotion persists across a reopen.
	s.Close()
	reopened := openTest(t, dir, clock)
	reopened.ReadCharacters(testDivision, func(characters []*enterworld.Character) {
		for _, rc := range characters {
			if rc.GMPrivilege {
				t.Errorf("%s still GM after demote + reopen", rc.Name)
			}
		}
	})
}

// TestGMCharactersFromEnv: the parse trims, drops empties, and answers
// nil for an unset/empty variable (= NO GMs, the strict posture).
func TestGMCharactersFromEnv(t *testing.T) {
	t.Setenv(EnvGMCharacters, " d1:Alpha , ,d2:Beta ")
	got, err := GMCharactersFromEnv()
	if err != nil || len(got) != 2 ||
		got[0] != (GMIdentity{DivisionID: "d1", CharacterName: "Alpha"}) ||
		got[1] != (GMIdentity{DivisionID: "d2", CharacterName: "Beta"}) {
		t.Fatalf("parsed allowlist = %v, %v", got, err)
	}
	t.Setenv(EnvGMCharacters, "")
	if got, err := GMCharactersFromEnv(); err != nil || got != nil {
		t.Fatalf("empty env parsed as %v, %v; want nil, nil", got, err)
	}
	t.Setenv(EnvGMCharacters, "Alpha")
	if got, err := GMCharactersFromEnv(); err == nil || got != nil {
		t.Fatalf("unqualified GM identity parsed as %v, %v; want explicit error", got, err)
	}
}

func TestParseGMCharactersMatchesEnvironmentContract(t *testing.T) {
	t.Parallel()

	got, err := ParseGMCharacters(" global-official:asd2 , test:Probe ")
	if err != nil {
		t.Fatal(err)
	}
	want := []GMIdentity{
		{DivisionID: "global-official", CharacterName: "asd2"},
		{DivisionID: "test", CharacterName: "Probe"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed GM identities = %#v, want %#v", got, want)
	}
}

func TestReconcileGMPrivilegeIsDivisionQualified(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	for _, divisionID := range []string{"d1", "d2"} {
		if err := s.CreateCharacter(divisionID, "test-account", &enterworld.Character{Name: "SameName"}); err != nil {
			t.Fatal(err)
		}
	}
	s.ReconcileGMPrivilege([]GMIdentity{{DivisionID: "d1", CharacterName: "SameName"}})

	for _, divisionID := range []string{"d1", "d2"} {
		s.ReadCharacters(divisionID, func(characters []*enterworld.Character) {
			if len(characters) != 1 {
				t.Fatalf("%s characters = %d", divisionID, len(characters))
			}
			want := divisionID == "d1"
			if characters[0].GMPrivilege != want {
				t.Fatalf("%s GM privilege = %v, want %v", divisionID, characters[0].GMPrivilege, want)
			}
		})
	}
}
