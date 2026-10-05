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
