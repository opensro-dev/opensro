/*
===========================================================================

peer_visibility_test.go - the kept shown sets equal the full relation

The visibility pass recomputes only a crosser's pairs when nothing but
blocks changed (peer_visibility.go). Over a randomized run of crossings,
in-block moves, joins, leaves, instance changes and a member without an
appearance, after every tick each viewer's shown set must equal the full
relation computed from scratch, the tick must have sent exactly the
spawns and despawns that move it there, and each player's viewers must
be exactly the viewers that show it.

===========================================================================
*/
package simulation

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
)

/*
================
fullPeerRelation

What every viewer must be shown, from scratch: each other session of its
division and instance, with an appearance, in a block adjacent to its own.
================
*/
func fullPeerRelation(sessions []SessionSnapshot, nowMs int64) map[string]map[uint32]bool {
	out := make(map[string]map[uint32]bool, len(sessions))
	for i := range sessions {
		viewer := &sessions[i]
		shown := map[uint32]bool{}
		for j := range sessions {
			peer := &sessions[j]
			if i == j || peer.Appearance == nil || peer.DivisionID != viewer.DivisionID || peer.WorldInstance != viewer.WorldInstance {
				continue
			}
			if peerInterestBlock(viewer.World.LiveSpawnAt(nowMs)).Adjacent(peerInterestBlock(peer.World.LiveSpawnAt(nowMs))) {
				shown[PlayerObjectID(peer.CharacterID)] = true
			}
		}
		out[viewer.SessionID] = shown
	}
	return out
}

/*
================
scopeGIDsTo

The gids pushed to viewer with opcode, from frames naming their scope.
================
*/
func scopeGIDsTo(push *fakePusher, viewer string, opcode uint16) []uint32 {
	var out []uint32
	for _, pushed := range push.toSession {
		if pushed.sessionID != viewer {
			continue
		}
		for _, f := range pushed.frames {
			if f.Opcode == opcode {
				out = append(out, f.ScopeGID)
			}
		}
	}
	slices.Sort(out)
	return out
}

/*
================
TestPeerVisibilityKeepsTheFullRelation
================
*/
func TestPeerVisibilityKeepsTheFullRelation(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	// Four by four blocks of one region: most moves stay in a block, some
	// cross one border, some jump across the area.
	place := func() Spawn {
		return Spawn{RegionID: 0x6060, X: rng.Float64() * 4 * worldgeom.InterestBlockSize, Z: rng.Float64() * 4 * worldgeom.InterestBlockSize}
	}
	var sessions []SessionSnapshot
	next := 0
	join := func() {
		next++
		s := peerSession(fmt.Sprintf("s%03d", next), "world", int64(next), fmt.Sprintf("peer%d", next))
		s.World.Spawn = place()
		if next%17 == 0 {
			s.Appearance = nil // a viewer the block index does not hold
		}
		sessions = append(sessions, s)
	}
	for len(sessions) < 60 {
		join()
	}
	state := &divisionTickState{shownPeers: make(map[string]map[uint32]bool)}
	push := &fakePusher{}
	ticker := &Ticker{Push: push}
	previous := map[string]map[uint32]bool{}
	incremental := 0
	for tick := 0; tick < 400; tick++ {
		switch op := rng.Intn(20); {
		case op == 0 && len(sessions) > 40:
			i := rng.Intn(len(sessions))
			sessions = append(sessions[:i], sessions[i+1:]...)
		case op == 1:
			join()
		case op == 2:
			sessions[rng.Intn(len(sessions))].WorldInstance ^= 1
		default:
			for n := 0; n < 1+rng.Intn(6); n++ {
				s := &sessions[rng.Intn(len(sessions))]
				if rng.Intn(4) == 0 {
					s.World.Spawn = place()
				} else {
					s.World.Spawn.X = min(max(s.World.Spawn.X+rng.Float64()*80-40, 0), 4*worldgeom.InterestBlockSize-1)
					s.World.Spawn.Z = min(max(s.World.Spawn.Z+rng.Float64()*80-40, 0), 4*worldgeom.InterestBlockSize-1)
				}
			}
		}
		live := make(map[string]bool, len(sessions))
		for i := range sessions {
			live[sessions[i].SessionID] = true
		}
		nowMs := int64(tick * 100)
		if full, crossers := state.clone().peerVisibilityChanges(sessions, nowMs, live); !full && len(crossers) > 0 {
			incremental++
		}
		push.toSession = nil
		ticker.runPeerVisibility(state, nowMs, sessions, live)

		want := fullPeerRelation(sessions, nowMs)
		for _, viewer := range sessions {
			id := viewer.SessionID
			got := state.shownPeers[id]
			var spawns, despawns []uint32
			for gid := range want[id] {
				if !previous[id][gid] {
					spawns = append(spawns, gid)
				}
			}
			for gid := range previous[id] {
				if !want[id][gid] {
					despawns = append(despawns, gid)
				}
			}
			slices.Sort(spawns)
			slices.Sort(despawns)
			if len(got) != len(want[id]) {
				t.Fatalf("tick %d: %s shows %d players, the full relation %d", tick, id, len(got), len(want[id]))
			}
			for gid := range want[id] {
				if !got[gid] {
					t.Fatalf("tick %d: %s does not show %d", tick, id, gid)
				}
			}
			if sent := scopeGIDsTo(push, id, wire.OpSingleObjectSpawn); !slices.Equal(sent, spawns) {
				t.Fatalf("tick %d: %s was spawned %v, want %v", tick, id, sent, spawns)
			}
			if sent := scopeGIDsTo(push, id, wire.OpObjectDespawn); !slices.Equal(sent, despawns) {
				t.Fatalf("tick %d: %s was despawned %v, want %v", tick, id, sent, despawns)
			}
		}
		for i := range sessions {
			gid := PlayerObjectID(sessions[i].CharacterID)
			if watchers, scanned := watchersOf(state, gid), fullScanWatchers(state, gid); !slices.Equal(watchers, scanned) {
				t.Fatalf("tick %d: viewers of %d are %v, the shown sets say %v", tick, gid, watchers, scanned)
			}
		}
		previous = want
	}
	if incremental < 100 {
		t.Fatalf("only %d of 400 ticks took the crossing path", incremental)
	}
}

/*
================
divisionTickState.clone

A copy of the visibility keys alone, to ask peerVisibilityChanges which
path a tick takes without changing the real state.
================
*/
func (state *divisionTickState) clone() *divisionTickState {
	return &divisionTickState{
		peerVisibilityKeys:  append([]peerVisibilityKey(nil), state.peerVisibilityKeys...),
		peerVisibilityValid: state.peerVisibilityValid,
	}
}

/*
================
watchersOf
================
*/
func watchersOf(state *divisionTickState, gid uint32) []string {
	var out []string
	state.forEachPeerWatcher(gid, func(viewer string) { out = append(out, viewer) })
	slices.Sort(out)
	return out
}

/*
================
fullScanWatchers
================
*/
func fullScanWatchers(state *divisionTickState, gid uint32) []string {
	var out []string
	for viewer, shown := range state.shownPeers {
		if shown[gid] {
			out = append(out, viewer)
		}
	}
	slices.Sort(out)
	return out
}
