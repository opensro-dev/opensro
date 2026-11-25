/*
===========================================================================

playermap_test.go - the beta world map roster leaves an invisible GM out

A GM in /INVISIBLE (body status 4) is hidden from every other client's
world, so the port-only roster must not draw them on the world map (M)
either; turning invisibility off brings the marker back.

===========================================================================
*/
package simulation

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
playerMapGids

The gids in each session's last roster frame, decoded from the wire body
(u16 count, then gid-first rows with a u8-length name).
================
*/
func playerMapGids(t *testing.T, push *fakePusher) map[string]map[uint32]bool {
	t.Helper()
	out := map[string]map[uint32]bool{}
	for _, sent := range push.toSession {
		for _, frame := range sent.frames {
			if frame.Opcode != wire.OpBetaPlayerMap {
				continue
			}
			body := frame.Payload
			count := int(binary.LittleEndian.Uint16(body))
			body = body[2:]
			gids := map[uint32]bool{}
			for range count {
				gids[binary.LittleEndian.Uint32(body)] = true
				nameLength := int(body[14])
				body = body[15+nameLength:]
			}
			out[sent.sessionID] = gids
		}
	}
	return out
}

/*
================
TestPlayerMapLeavesInvisibleGMOut
================
*/
func TestPlayerMapLeavesInvisibleGMOut(t *testing.T) {
	sessions := []SessionSnapshot{
		{SessionID: "gm", CharacterID: 1, Appearance: &PeerAppearance{Name: "GM"}},
		{SessionID: "player", CharacterID: 2, Appearance: &PeerAppearance{Name: "Ann"}},
	}
	gm, player := PlayerObjectID(1), PlayerObjectID(2)
	run := func(status uint8, nowMs int64) map[string]map[uint32]bool {
		sessions[0].NativeBodyStatus = status
		push := &fakePusher{}
		ticker := newTestTicker(nil, push)
		ticker.PlayerMap = true
		ticker.runPlayerMap(&divisionTickState{}, nowMs, sessions)
		return playerMapGids(t, push)
	}

	hidden := run(gmInvisibleBodyStatus, 0)
	if hidden["player"][gm] {
		t.Fatal("invisible GM is on another player's world map")
	}
	if !hidden["player"][player] || !hidden["gm"][player] {
		t.Fatalf("roster lost the visible player: %v", hidden)
	}

	shown := run(0, 0)
	if !shown["player"][gm] {
		t.Fatal("GM is still missing after invisibility was turned off")
	}

	// /INVINCIBLE (3) hides nothing.
	if !run(3, 0)["player"][gm] {
		t.Fatal("invincible GM dropped off the world map")
	}
}
