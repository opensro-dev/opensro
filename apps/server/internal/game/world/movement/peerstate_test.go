package movement

import (
	"testing"

	"opensro.online/server/internal/domain"
)

func TestPeerStateCapturePreservesActiveZeroTeamWithoutAliasing(t *testing.T) {
	c := &domain.Character{Name: "Peer", PK: &domain.PKRecord{Penalty: 1200}, EventMembership: &domain.EventMembership{ID: 9, Team: 0}}
	captured := capturePeerAppearance(c, 1907)
	c.PK.Penalty = 0
	c.EventMembership.Team = 1
	appearance := peerAppearance(nil, nil, "test", captured)
	if appearance.PVPState != 2 || appearance.EventTeam == nil || *appearance.EventTeam != 0 {
		t.Fatalf("captured state changed outside read door: %+v", appearance)
	}
	c.EventMembership = nil
	appearance = peerAppearance(nil, nil, "test", capturePeerAppearance(c, 1907))
	if appearance.PVPState != 0 || appearance.EventTeam != nil {
		t.Fatalf("inactive state retained: %+v", appearance)
	}
	appearance = peerAppearance(nil, nil, "test", peerAppearanceCapture{hasModel: true, modelRef: 1907})
	if appearance.EventTeam != nil {
		t.Fatal("zero-value capture enrolled a character in team zero")
	}
}

/*
================
TestPeerSeesTheJobAliasOnlyInJobMode

v1.188's spawn tail (4E5B55) writes the job alias as a peer's name while a
job suit is worn and the name otherwise. The lookup key stays the real name.
================
*/
func TestPeerSeesTheJobAliasOnlyInJobMode(t *testing.T) {
	// 3/1/7/3: a hunter's job suit, worn in the job suit socket.
	const hunterSuit uint16 = 3<<2 | 1<<5 | 7<<7 | 3<<11
	c := &domain.Character{Name: "RealName", Job: domain.CharacterJob{Type: 3, Grade: 2, Alias: "Shadow"}}
	if appearance := peerAppearance(nil, nil, "test", capturePeerAppearance(c, 1907)); appearance.Name != "RealName" || appearance.JobType != 0 {
		t.Fatalf("out of job mode: name %q job %d", appearance.Name, appearance.JobType)
	}
	c.MissionInventory = []domain.InventoryRow{{Slot: 8, RefObjID: 9001, TypeFlags: hunterSuit, StackCount: 1}}
	captured := capturePeerAppearance(c, 1907)
	appearance := peerAppearance(nil, nil, "test", captured)
	if appearance.Name != "Shadow" || appearance.JobType != 3 || appearance.JobGrade != 2 {
		t.Fatalf("in job mode: name %q job %d grade %d", appearance.Name, appearance.JobType, appearance.JobGrade)
	}
	if captured.name != "RealName" {
		t.Fatalf("the lookup key became %q", captured.name)
	}
}
