package enterworld

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// petSkillRefSource answers item codenames from a fixed table and inherits
// the COS characterdata lookup the bootstrap validates the pet against.
type petSkillRefSource struct {
	cosBootstrapRefSource
	items map[string]*ItemRef
}

func (s petSkillRefSource) ItemRefByCodename(codename string) (*ItemRef, bool) {
	ref, ok := s.items[codename]
	return ref, ok
}

func petSkillFixture(summoned bool) (*Deps, *Character, *[][2]string, int64) {
	cos := &CharacterRef{
		RefObjID: 3914, TidWord: 0x11C6, Codename: "COS_T_DHORSE3", Name: "Red Horse",
		WalkSpeed: 20, RunSpeed: 40, Scale: 100, MaxHP: 87829,
	}
	petSkill := [4]int64{3, 3, 13, 15}
	items := map[string]*ItemRef{
		"ITEM_MALL_PET_SKILL_COLD": {RefObjID: 24001, Codename: "ITEM_MALL_PET_SKILL_COLD", TypeIDs: petSkill,
			NativeFields: NewNativeFields(map[string]float64{"itemParam1_29c": 1800})},
		"ITEM_MALL_PET_SKILL_FIRE": {RefObjID: 24002, Codename: "ITEM_MALL_PET_SKILL_FIRE", TypeIDs: petSkill,
			NativeFields: NewNativeFields(map[string]float64{"itemParam1_29c": 1800})},
		// Republished under a different id since the window was raised.
		"ITEM_MALL_PET_SKILL_LIGHTNING": {RefObjID: 24999, Codename: "ITEM_MALL_PET_SKILL_LIGHTNING", TypeIDs: petSkill},
		// A summoner: Param1 in minutes, never a kind-3 producer.
		"ITEM_COS_T_DHORSE3": {RefObjID: 3915, Codename: "ITEM_COS_T_DHORSE3", TypeIDs: [4]int64{3, 3, 3, 2}},
	}
	const nowMs = int64(1_000_000_000)
	var tracked [][2]string
	deps := &Deps{
		Items: petSkillRefSource{cosBootstrapRefSource{character: cos}, items},
		Now:   func() time.Time { return time.UnixMilli(nowMs) },
		TrackTimedWindows: func(divisionID, characterName string) {
			tracked = append(tracked, [2]string{divisionID, characterName})
		},
	}
	character := &Character{
		ID: 3, Name: "asd2",
		PetSkillWindows: []domain.PetSkillWindow{
			{ItemRefObjID: 24001, Codename: "ITEM_MALL_PET_SKILL_COLD", EndUnixMs: nowMs + 90_500},
			{ItemRefObjID: 24002, Codename: "ITEM_MALL_PET_SKILL_FIRE", EndUnixMs: nowMs},
			{ItemRefObjID: 24003, Codename: "ITEM_MALL_PET_SKILL_LIGHTNING", EndUnixMs: nowMs + 60_000},
			{ItemRefObjID: 3915, Codename: "ITEM_COS_T_DHORSE3", EndUnixMs: nowMs + 60_000},
		},
	}
	gid, _ := CosObjectIDForCharacter(character)
	character.ActiveCOS = &CharacterCOS{
		GID: gid, RefObjID: cos.RefObjID, Codename: cos.Codename,
		Name: cos.Name, CurrentHP: cos.MaxHP, Summoned: summoned, Mounted: summoned,
	}
	return deps, character, &tracked, nowMs
}

func petSkillPackets(packets []Packet) (indexes []int) {
	for index, packet := range packets {
		if packet.NativeOpcode == wire.OpCosStateRefresh {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func TestBootstrapReRaisesOnlyLiveStillResolvingPetSkillWindowsAfterTheReset(t *testing.T) {
	deps, character, tracked, _ := petSkillFixture(true)
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}
	packets, err := buildBootstrapPackets(deps, "global-official", character, entry, nil, ObjectIDForCharacter(character))
	if err != nil {
		t.Fatal(err)
	}
	windows := petSkillPackets(packets)
	if len(windows) != 1 {
		t.Fatalf("0x3691 packets = %d, want only the live, still-resolving pet skill", len(windows))
	}
	// 90.5 s left reads 91: the window is spent only once it truly is.
	want := wire.EncodeCosSummonTimer3691(24001, 91, 0)
	got := packets[windows[0]].Payload
	if len(got) != len(want) {
		t.Fatalf("0x3691 length = %d, want %d", len(got), len(want))
	}
	for index, value := range want {
		if got[index] != int(value) {
			t.Fatalf("0x3691[%d] = 0x%02X, want 0x%02X (payload %v)", index, got[index], value, got)
		}
	}
	// 0x3369 opens the sequence and empties the board, so the window must
	// follow it; it also trails the pet's record, spawn and ride state.
	if packets[0].NativeOpcode != OpcodeResetClient {
		t.Fatalf("first opcode = 0x%04X, want the 0x3369 reset", packets[0].NativeOpcode)
	}
	for index, packet := range packets {
		if (packet.NativeOpcode == wire.OpCosRecordCreate || packet.NativeOpcode == OpcodeObjectListFinalize ||
			packet.NativeOpcode == wire.OpCosRideState) && index > windows[0] {
			t.Fatalf("opcode 0x%04X at %d follows the window at %d", packet.NativeOpcode, index, windows[0])
		}
	}
	if len(*tracked) != 1 || (*tracked)[0] != [2]string{"global-official", "asd2"} {
		t.Fatalf("tracked = %v, want the character handed to the sweep once", *tracked)
	}
}

// A dismissed pet does not end a window: retail's only kind-3 erasers are the
// 0x3691 zero pair and the reset sweep, so the row outlives the pet in session
// and a re-entry must restore it just the same.
func TestBootstrapReRaisesPetSkillWindowsWithoutASummonedPet(t *testing.T) {
	deps, character, tracked, _ := petSkillFixture(false)
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}
	packets, err := buildBootstrapPackets(deps, "global-official", character, entry, nil, ObjectIDForCharacter(character))
	if err != nil {
		t.Fatal(err)
	}
	if windows := petSkillPackets(packets); len(windows) != 1 {
		t.Fatalf("0x3691 packets = %v, want the live window restored without its pet", windows)
	}
	if len(*tracked) != 1 {
		t.Fatalf("tracked = %v, want the stored deadlines retired by the sweep", *tracked)
	}

	character.PetSkillWindows = nil
	*tracked = nil
	if _, err := buildBootstrapPackets(deps, "global-official", character, entry, nil, ObjectIDForCharacter(character)); err != nil {
		t.Fatal(err)
	}
	if len(*tracked) != 0 {
		t.Fatalf("tracked = %v, want no sweep entry for a character with no windows", *tracked)
	}
}

func TestRefItemSnapshotSeedsAPetSkillWindowWhoseStackIsSpent(t *testing.T) {
	deps, character, _, _ := petSkillFixture(true)
	snapshot := buildRefItemSnapshot(deps, "global-official", character)
	for _, row := range snapshot {
		if row.RefObjID == 24001 {
			if row.NativeFields.Get("itemParam1_29c") != 1800 {
				t.Fatalf("seeded row fields = %v, want Param1 1800 for the browser's limit", row.NativeFields)
			}
			return
		}
	}
	t.Fatalf("snapshot %+v lacks the window's reference; the re-raised row would have no limit", snapshot)
}

func TestPetSkillWindowRemainingRoundsUpAndFailsClosed(t *testing.T) {
	for _, c := range []struct {
		end, now int64
		want     uint32
	}{
		{end: 5_000, now: 5_000, want: 0},
		{end: 4_999, now: 5_000, want: 0},
		{end: 5_001, now: 5_000, want: 1},
		{end: 6_000, now: 5_000, want: 1},
		{end: 6_001, now: 5_000, want: 2},
		{end: int64(^uint32(0))*1000 + 1_000_000, now: 0, want: 0},
	} {
		if got := PetSkillWindowRemaining(c.end, c.now); got != c.want {
			t.Errorf("PetSkillWindowRemaining(%d, %d) = %d, want %d", c.end, c.now, got, c.want)
		}
	}
}

func TestDepsClockFallsBackToTheWallClock(t *testing.T) {
	before := time.Now()
	for _, deps := range []*Deps{nil, {}} {
		if got := deps.clock(); got.Before(before) {
			t.Fatalf("clock() = %v, want the wall clock", got)
		}
	}
	fixed := time.UnixMilli(42)
	if got := (&Deps{Now: func() time.Time { return fixed }}).clock(); !got.Equal(fixed) {
		t.Fatalf("clock() = %v, want the injected %v", got, fixed)
	}
}
