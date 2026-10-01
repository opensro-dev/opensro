/*
===========================================================================

peercos_abnormal_test.go - first-sight summon status and life publication

An arriving observer must receive the same native status and life carriers
as existing observers, after object creation and without private pet data.

===========================================================================
*/
package simulation

import (
	"bytes"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

/*
================
TestPeerCosFirstSightPublishesAbnormalAndDeadState
================
*/
func TestPeerCosFirstSightPublishesAbnormalAndDeadState(t *testing.T) {
	payload := []byte{7, 0, 0, 0, 2, 0, 1, 1, 0, 0, 0}
	pet := PeerCOS{Row: wire.CosSpawnBand2{Gid: 7, RefObjID: 9}, AbnormalVitals: payload, LifeState: wire.LifeStateDead}
	frames := pet.frames(1000, true)
	if len(frames) != 4 || frames[0].Opcode != wire.OpSingleObjectSpawn || frames[1].Opcode != OpVitalsUpdate || !bytes.Equal(frames[1].Payload, payload) {
		t.Fatalf("first-sight packets %+v", frames)
	}
	if frames[2].Opcode != OpVitalsUpdate || frames[3].Opcode != wire.OpObjectStateRefresh {
		t.Fatal("LIFE preceded death baseline")
	}
	payload[7] = 0
	if frames[1].Payload[7] != 1 {
		t.Fatal("published snapshot aliases retained status")
	}
	for _, frame := range pet.frames(1000, false) {
		if frame.Opcode == OpVitalsUpdate || frame.Opcode == wire.OpObjectStateRefresh {
			t.Fatal("ordinary movement replayed first-sight status")
		}
	}
}
