/*
===========================================================================

peervis_invisible_test.go - a GM-invisible peer spawns for its party only

===========================================================================
*/
package simulation

import (
	"slices"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestInvisiblePeerSpawnsOnlyForPartyMembers

A character under the GM invisible body state (4) is spawn-hidden from
everyone but its current party: a party member is shown the GM, a
stranger is never shown it, the end of the party withdraws the spawn,
and dropping the state shows the GM to everyone again (#4122). The
v1.150 client never hides this body byte itself, so the audience is
decided by who receives the spawn.
================
*/
func TestInvisiblePeerSpawnsOnlyForPartyMembers(t *testing.T) {
	gm := peerSession("gm", "DIV_A", 1, "GM")
	gm.NativeBodyStatus = gmInvisibleBodyStatus
	mate := peerSession("mate", "DIV_A", 2, "Mate")
	solo := peerSession("solo", "DIV_A", 3, "Solo")
	sessions := []SessionSnapshot{gm, mate, solo}
	live := map[string]bool{"gm": true, "mate": true, "solo": true}
	gmGid := PlayerObjectID(1)
	mateGid := PlayerObjectID(2)
	soloGid := PlayerObjectID(3)

	parties := []PeerParty{{Order: 7, Members: []uint32{gmGid, mateGid}}}
	push := &fakePusher{}
	ticker := &Ticker{Push: push, Parties: func(string) []PeerParty { return parties }}
	state := &divisionTickState{shownPeers: make(map[string]map[uint32]bool)}

	// While the party lasts, the party member is shown the invisible GM
	// and the stranger is not; the two visible players see each other.
	ticker.runPeerVisibility(state, 1000, sessions, live)
	if got := scopeGIDsTo(push, "mate", wire.OpSingleObjectSpawn); !slices.Equal(got, []uint32{gmGid, soloGid}) {
		t.Fatalf("party member spawns = %v, want the invisible GM and the stranger %v", got, []uint32{gmGid, soloGid})
	}
	if got := scopeGIDsTo(push, "solo", wire.OpSingleObjectSpawn); !slices.Equal(got, []uint32{mateGid}) {
		t.Fatalf("stranger spawns = %v, want only the visible player %v", got, []uint32{mateGid})
	}

	// The party ends: the former member's spawn of the GM is withdrawn,
	// and nobody is shown anything new.
	parties = nil
	ticker.runPeerVisibility(state, 1100, sessions, live)
	if got := scopeGIDsTo(push, "mate", wire.OpObjectDespawn); !slices.Equal(got, []uint32{gmGid}) {
		t.Fatalf("former member despawns = %v, want the GM %v", got, []uint32{gmGid})
	}
	if got := scopeGIDsTo(push, "mate", wire.OpSingleObjectSpawn); !slices.Equal(got, []uint32{gmGid, soloGid}) {
		t.Fatalf("party member spawns after the party ended = %v, want no new spawn", got)
	}
	if got := scopeGIDsTo(push, "solo", wire.OpSingleObjectSpawn); !slices.Equal(got, []uint32{mateGid}) {
		t.Fatalf("stranger spawns after the party ended = %v, want no new spawn", got)
	}

	// The GM drops the invisible state: everyone in interest is shown it.
	sessions[0].NativeBodyStatus = 0
	ticker.runPeerVisibility(state, 1200, sessions, live)
	if got := scopeGIDsTo(push, "solo", wire.OpSingleObjectSpawn); !slices.Equal(got, []uint32{gmGid, mateGid}) {
		t.Fatalf("stranger spawns after the state dropped = %v, want the GM shown %v", got, []uint32{gmGid, mateGid})
	}
	if got := scopeGIDsTo(push, "mate", wire.OpObjectDespawn); !slices.Equal(got, []uint32{gmGid}) {
		t.Fatalf("former member despawns after the state dropped = %v, want no new despawn", got)
	}
}
