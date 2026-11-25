/*
===========================================================================

tomb_unique_test.go - a tomb floor unique counts as a unique

Regression coverage for #4235: a Ch'in Tomb second-floor unique kill
counted on neither the unique leaderboard nor the website. The floor's
Tomb General clones are authored at rarity grade 8, and the counting
gates once read grade 3 only. This test runs the shipped tomb
population: the clone is listed by the unique tracker, its spawn and
death are announced, and its kill is recorded.

===========================================================================
*/

package simulation

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
uniqueNoticeFor

The queued 0x3058 notice of the given kind naming ref, if any.
================
*/
func uniqueNoticeFor(frames []Frame, kind uint8, ref uint32) bool {
	for _, frame := range frames {
		if frame.Opcode == 0x3058 && len(frame.Payload) >= 5 && frame.Payload[0] == kind &&
			binary.LittleEndian.Uint32(frame.Payload[1:5]) == ref {
			return true
		}
	}
	return false
}

/*
================
TestTombFloor2UniqueKillIsRecorded

The B2 unique spawns from its shipped nest at grade 8; the tracker
lists it, the division hears of its spawn and death, and
RecordUniqueKiller records the kill the leaderboard counts.
================
*/
func TestTombFloor2UniqueKillIsRecorded(t *testing.T) {
	base := monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG", Level: 1, MaxHP: 59},
	}, nil)
	withTomb, err := base.WithQinShiTomb("")
	if err != nil {
		t.Fatalf("WithQinShiTomb: %v", err)
	}
	s := NewMonsterState(withTomb)
	base0 := time.UnixMilli(tombRespawnBaseSec * 1000)
	s.SetTimeSource(func() time.Time { return base0 })
	s.SetRandomSource(constantWord(16384))
	s.StartDivision("division")
	state := s.divs["division"]
	if state == nil {
		t.Fatal("no division state")
	}
	index := -1
	var cloneRef uint32
	for i, nest := range withTomb.Nests {
		if nest.RegionID == tombFloor2Region && withTomb.Refs[nest.RefObjID].Codename == tombFloor2Unique {
			index, cloneRef = i, nest.RefObjID
		}
	}
	if index < 0 {
		t.Fatal("the B2 unique's nest is not in the shipped tomb template")
	}
	n := state.nests[index]
	n.lastMs = 0
	n.intervalMs = 1
	s.AdvancePopulation(base0.Add(2 * time.Second).UnixMilli())
	var clone monster.Instance
	found := false
	for _, in := range s.InstancesInRegions("division", []uint16{tombFloor2Region}) {
		if in.Ref.Codename == tombFloor2Unique {
			clone, found = in, true
		}
	}
	if !found {
		t.Fatal("the B2 unique did not spawn")
	}
	if grade := clone.Rarity() & 15; grade != 8 {
		t.Fatalf("the B2 unique's authored grade is %d, want 8", grade)
	}
	if !clone.IsUnique() {
		t.Fatal("the B2 unique is not a unique under the one rule")
	}
	listed := false
	for _, entry := range s.UniqueStates("division") {
		if entry.Codename == tombFloor2Unique {
			listed = entry.Alive
		}
	}
	if !listed {
		t.Fatal("the unique tracker does not list the living B2 unique")
	}
	if !uniqueNoticeFor(s.DrainUniqueNotices("division"), uniqueNoticeAppear, cloneRef) {
		t.Fatal("the B2 unique's spawn was not announced")
	}
	if hit, ok := s.ApplyDamage("division", clone.Gid, clone.CurrentHP); !ok || !hit.Fatal {
		t.Fatal("the B2 unique could not be killed")
	}
	ref, recorded := s.RecordUniqueKiller("division", clone.Gid, "tester")
	if !recorded || ref != cloneRef {
		t.Fatalf("the B2 unique's kill was not recorded (ref %d, recorded %v)", ref, recorded)
	}
	if !uniqueNoticeFor(s.DrainUniqueNotices("division"), uniqueNoticeDeath, cloneRef) {
		t.Fatal("the B2 unique's death was not announced")
	}
	if !s.Defeat("division", clone.Gid, base0.Add(3*time.Second)) {
		t.Fatal("the B2 unique's defeat was refused")
	}
	for _, entry := range s.UniqueStates("division") {
		if entry.Codename == tombFloor2Unique && entry.Alive {
			t.Fatal("the unique tracker still lists the dead B2 unique as alive")
		}
	}
}
