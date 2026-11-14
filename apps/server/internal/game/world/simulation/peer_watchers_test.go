/*
===========================================================================

peer_watchers_test.go - the session legs' shownPeers inversion

forEachPeerWatcher must visit exactly the viewers the full shownPeers walk
visits, indexed or not, and a stale inversion may only skip a viewer that
no longer shows the gid, never invent one.

===========================================================================
*/

package simulation

import (
	"math/rand"
	"sort"
	"testing"
)

/*
================
watchersOf
================
*/
func watchersOf(state *divisionTickState, gid uint32) []string {
	var out []string
	state.forEachPeerWatcher(gid, func(viewer string) { out = append(out, viewer) })
	sort.Strings(out)
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
	sort.Strings(out)
	return out
}

/*
================
TestPeerWatchersMatchTheFullScan
================
*/
func TestPeerWatchersMatchTheFullScan(t *testing.T) {
	rng := rand.New(rand.NewSource(3510))
	for round := 0; round < 50; round++ {
		state := &divisionTickState{shownPeers: map[string]map[uint32]bool{}}
		for v := 0; v < 1+rng.Intn(40); v++ {
			shown := map[uint32]bool{}
			for g := 0; g < rng.Intn(30); g++ {
				shown[uint32(100000+rng.Intn(60))] = rng.Intn(8) != 0
			}
			state.shownPeers[string(rune('a'+v%26))+string(rune('A'+v/26))] = shown
		}
		for _, indexed := range []bool{false, true} {
			if indexed {
				state.indexPeerWatchers()
			} else {
				state.dropPeerWatchers()
			}
			for gid := uint32(100000); gid < 100060; gid++ {
				got, want := watchersOf(state, gid), fullScanWatchers(state, gid)
				if len(got) != len(want) {
					t.Fatalf("round %d indexed=%v gid %d: %v, full scan %v", round, indexed, gid, got, want)
				}
				for i := range got {
					if got[i] != want[i] {
						t.Fatalf("round %d indexed=%v gid %d: %v, full scan %v", round, indexed, gid, got, want)
					}
				}
			}
		}
	}
}

/*
================
TestStalePeerWatchersNeverInventAViewer

A viewer that stops showing the gid after the inversion is skipped; one
that starts showing it is missed until the next inversion, which the legs
rebuild every tick (shownPeers changes only before them).
================
*/
func TestStalePeerWatchersNeverInventAViewer(t *testing.T) {
	state := &divisionTickState{shownPeers: map[string]map[uint32]bool{
		"a": {100001: true},
		"b": {100001: true},
	}}
	state.indexPeerWatchers()
	delete(state.shownPeers["b"], 100001)
	if got := watchersOf(state, 100001); len(got) != 1 || got[0] != "a" {
		t.Fatalf("stale inversion visited %v, want only a", got)
	}
	state.dropPeerWatchers()
	state.shownPeers["c"] = map[uint32]bool{100001: true}
	if got := watchersOf(state, 100001); len(got) != 2 {
		t.Fatalf("outside the legs the full scan should see a and c, got %v", got)
	}
}

/*
================
TestPeerWatchersReindexListsEachViewerOnce

The legs index every tick on the same state: a second inversion must
truncate the first's lists, or every viewer would be listed (and pushed
to) once more per tick.
================
*/
func TestPeerWatchersReindexListsEachViewerOnce(t *testing.T) {
	state := &divisionTickState{shownPeers: map[string]map[uint32]bool{"a": {100001: true}, "b": {100001: true}}}
	state.indexPeerWatchers()
	state.indexPeerWatchers()
	if got := watchersOf(state, 100001); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("second tick's inversion visited %v, want [a b] once each", got)
	}
}

/*
================
TestPeerWatchersPruneGidsNobodyShows

A gid that leaves every shown set keeps an empty list for one tick and is
pruned at the next inversion; one shown again is listed afresh.
================
*/
func TestPeerWatchersPruneGidsNobodyShows(t *testing.T) {
	state := &divisionTickState{shownPeers: map[string]map[uint32]bool{"a": {100001: true}}}
	state.indexPeerWatchers()
	delete(state.shownPeers["a"], 100001)
	state.indexPeerWatchers()
	state.indexPeerWatchers()
	if _, kept := state.peerWatchers[100001]; kept {
		t.Fatal("a gid nobody shows stayed in the inversion")
	}
	state.shownPeers["a"][100001] = true
	state.indexPeerWatchers()
	if got := watchersOf(state, 100001); len(got) != 1 || got[0] != "a" {
		t.Fatalf("a gid shown again was listed as %v, want [a]", got)
	}
}
