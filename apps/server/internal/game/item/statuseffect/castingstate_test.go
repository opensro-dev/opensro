package statuseffect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"testing"
)

func TestCastingStateNativeComparisons(t *testing.T) {
	data, err := licensed.ServerFixture(t, "internal/game/item/statuseffect/testdata/native-casting-state.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Schema        string `json:"schema"`
		CandidateHash string `json:"candidate_sha256"`
		ServerHash    string `json:"server_sha256"`
		Cases         []struct {
			Packed        uint32
			Remove        bool
			Before, After [4]uint64
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Schema != "sro-native-casting-state-v1" || !licensed.SHA256Hex(fixture.ServerHash) || len(fixture.Cases) != 2312 {
		t.Fatal("unexpected native fixture identity")
	}
	source, err := os.ReadFile("castingstate.go")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	if hex.EncodeToString(digest[:]) != fixture.CandidateHash {
		t.Fatal("candidate changed; rerun native comparison generator")
	}
	for i, c := range fixture.Cases {
		s := CastingConflictSnapshot{Active: c.Before, CurrentPacked: 123, CurrentOvl2: 456}
		s.UpdateActive(c.Packed, c.Remove)
		if s.Active != c.After || s.CurrentPacked != 123 || s.CurrentOvl2 != 456 {
			t.Fatalf("native case %d differs: %+v", i, s)
		}
	}
}

func TestCastingStateInstallRemoveSequence(t *testing.T) {
	var s CastingConflictSnapshot
	s.UpdateActive(5, false)
	s.UpdateActive(5, false)
	if !s.Conflicts(5) {
		t.Fatal("installed state missing")
	}
	s.UpdateActive(5, true)
	if s.Conflicts(5) {
		t.Fatal("native clear is unconditional, not reference counted")
	}
	s.UpdateActive(0x231d, false)
	if s.Active[0] != uint64(1)<<0x1d|uint64(1)<<0x23 {
		t.Fatal("special states must still be stored")
	}
	if s.Conflicts(0x231d) {
		t.Fatal("reader exceptions lost")
	}
	s.UpdateActive(0x231d, true)
	if s.Active != [4]uint64{} {
		t.Fatal("special states not cleared")
	}
}

func TestRegistryCastingStateLifecycle(t *testing.T) {
	r := NewRegistry()
	e := Effect{DivisionID: "d", CharacterName: "a", SkillID: 1, SkillGroup: 1, InstanceToken: 1, ClientCancelable: true, Persistent: true, ExpiresAtMs: 1000, InstalledStates: [2]uint32{5, 6}}
	e.RetirementStates = e.InstalledStates
	if !r.Apply(e) || !r.CastingStates("d", "a").Conflicts(5) || !r.CastingStates("d", "a").Conflicts(6) {
		t.Fatal("installation missing")
	}
	other := e
	other.CharacterName = "b"
	other.InstalledStates = [2]uint32{7, 8}
	if r.Apply(other) || r.CastingStates("d", "b").Conflicts(7) {
		t.Fatal("rejected token collision installed states")
	}
	if _, ok := r.RequestVoluntaryStop("d", "a", 1, 1); !ok {
		t.Fatal("stop failed")
	}
	if !r.CastingStates("d", "a").Conflicts(5) {
		t.Fatal("stop request cleared before retirement")
	}
	r.DrainStopRequested()
	if r.CastingStates("d", "a") != (CastingConflictSnapshot{}) {
		t.Fatal("retirement leaked states")
	}
	if !r.Apply(e) {
		t.Fatal("reinstall")
	}
	r.Expire(1000)
	if len(r.DrainStopRequested()) != 0 || !r.CastingStates("d", "a").Conflicts(5) {
		t.Fatal("job equality bypassed the native >1 second tick gate")
	}
	r.Expire(1001)
	r.DrainStopRequested()
	if r.CastingStates("d", "a") != (CastingConflictSnapshot{}) {
		t.Fatal("expiry leaked states")
	}
	if !r.Apply(e) {
		t.Fatal("restore")
	}
	r.Forget("d", "a")
	if r.CastingStates("d", "a") != (CastingConflictSnapshot{}) {
		t.Fatal("disconnect leaked states")
	}
	if !r.Apply(e) || !r.CastingStates("d", "a").Conflicts(6) {
		t.Fatal("restoration failed")
	}
}

func TestRegistryCastingStatesAreNotRebuiltFromRemainingRows(t *testing.T) {
	r := NewRegistry()
	e := Effect{DivisionID: "d", CharacterName: "a", SkillID: 1, SkillGroup: 1, InstanceToken: 1, ClientCancelable: true, InstalledStates: [2]uint32{5, 0}}
	e.RetirementStates = e.InstalledStates
	if !r.Apply(e) {
		t.Fatal("first")
	}
	e.SkillID, e.SkillGroup, e.InstanceToken = 2, 2, 2
	if !r.Apply(e) {
		t.Fatal("second")
	}
	r.RequestVoluntaryStop("d", "a", 1, 1)
	r.DrainStopRequested()
	if len(r.Snapshot("d", "a")) != 1 || r.CastingStates("d", "a").Conflicts(5) {
		t.Fatal("clear incorrectly retained remaining row's bit")
	}
}

func TestUnlinkedStateOperationsBranches(t *testing.T) {
	for _, tc := range []struct {
		mode               uint8
		durable, ovl, dttp bool
		install, retire    [2]uint32
	}{
		{1, false, false, false, [2]uint32{5, 0}, [2]uint32{5, 0}},
		{2, false, false, false, [2]uint32{5, 0}, [2]uint32{5, 0}},
		{2, false, false, true, [2]uint32{0, 0}, [2]uint32{5, 0}},
		{1, false, true, true, [2]uint32{5, 6}, [2]uint32{5, 6}},
		{2, false, true, true, [2]uint32{6, 6}, [2]uint32{6, 6}},
		{2, true, true, true, [2]uint32{5, 6}, [2]uint32{5, 6}},
	} {
		d := ReplacementDescriptor{PackedStates: 5, Ovl2: 6, Ovl2Present: tc.ovl, DttpPresent: tc.dttp}
		i, r := UnlinkedStateOperations(d, tc.mode, tc.durable)
		if i != tc.install || r != tc.retire {
			t.Fatalf("%+v: got %v %v", tc, i, r)
		}
	}
	// A present zero ovl2 replaces the packed word, unlike an absent ovl2.
	i, r := UnlinkedStateOperations(ReplacementDescriptor{PackedStates: 5, Ovl2Present: true}, 2, false)
	if i != [2]uint32{} || r != [2]uint32{} {
		t.Fatal("present-zero conflated with absence")
	}
}
