/*
===========================================================================

session_published_test.go - the kept published order is the map's order

PublishedObjects keeps its sorted set between changes instead of sorting
the observed map on every read. A random walk of scope batches and scene
resets compares every read with the set a mirror map sorts afresh, the way
the read worked before.

===========================================================================
*/
package transport

import (
	"math/rand"
	"slices"
	"testing"
)

/*
================
publishedMirror

The reference read: the mirror's members, freshly sorted.
================
*/
func publishedMirror(mirror map[uint32]bool) []uint32 {
	gids := make([]uint32, 0, len(mirror))
	for gid := range mirror {
		gids = append(gids, gid)
	}
	slices.Sort(gids)
	return gids
}

/*
================
TestPublishedObjectsMatchesSortedObservedSet
================
*/
func TestPublishedObjectsMatchesSortedObservedSet(t *testing.T) {
	hub := newHub(testCfg())
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.closeSession(s, nil)
	r := rand.New(rand.NewSource(7))
	mirror := map[uint32]bool{}
	if err := s.SendSceneReset([]Frame{{Opcode: 0x330a}}); err != nil {
		t.Fatal(err)
	}
	s.FinishSceneReentry()
	resets := 0
	for step := 0; step < 5000; step++ {
		revision, _ := s.SceneRevision()
		if r.Intn(200) == 0 {
			// A reset forgets the old scene and publishes its bootstrap.
			clear(mirror)
			var scope []ObjectScopeChange
			for range r.Intn(4) {
				gid := uint32(1 + r.Intn(300))
				scope = append(scope, ObjectScopeChange{GID: gid, Visible: true})
				mirror[gid] = true
			}
			if err := s.SendSceneReset([]Frame{{Opcode: 0x3417, Scope: scope}}); err != nil {
				t.Fatal(err)
			}
			if _, ok := s.PublishedObjects(revision); ok {
				t.Fatalf("step %d: the replaced scene still reads current", step)
			}
			s.FinishSceneReentry()
			resets++
			continue
		}
		var scope []ObjectScopeChange
		for range 1 + r.Intn(6) {
			change := ObjectScopeChange{GID: uint32(1 + r.Intn(300)), Visible: r.Intn(3) != 0}
			scope = append(scope, change)
			if change.Visible {
				mirror[change.GID] = true
			} else {
				delete(mirror, change.GID)
			}
		}
		if err := s.SendSceneBatch(revision, []Frame{{Opcode: 0x330a, Scope: scope}}); err != nil {
			t.Fatal(err)
		}
		// Read some steps twice, so reads of an unchanged set are covered.
		for range 1 + r.Intn(2) {
			got, ok := s.PublishedObjects(revision)
			if want := publishedMirror(mirror); !ok || got == nil || !slices.Equal(got, want) {
				t.Fatalf("step %d: published = %v %v, want %v", step, got, ok, want)
			}
		}
		// Draining keeps the queue under the slow-consumer limit.
		s.mu.Lock()
		s.queue, s.queueBytes = nil, 0
		s.mu.Unlock()
	}
	if resets < 10 {
		t.Fatalf("only %d scene resets in the walk", resets)
	}
}

/*
================
BenchmarkPublishedObjects

One session's read of 200 published objects, unchanged between reads (the
tick's common case).
================
*/
func BenchmarkPublishedObjects(b *testing.B) {
	hub := newHub(testCfg())
	s, err := hub.createSession()
	if err != nil {
		b.Fatal(err)
	}
	defer hub.closeSession(s, nil)
	var scope []ObjectScopeChange
	for gid := uint32(1); gid <= 200; gid++ {
		scope = append(scope, ObjectScopeChange{GID: gid * 7919 % 100003, Visible: true})
	}
	if err := s.SendSceneReset([]Frame{{Opcode: 0x3417, Scope: scope}}); err != nil {
		b.Fatal(err)
	}
	s.FinishSceneReentry()
	revision, _ := s.SceneRevision()
	b.ReportAllocs()
	for b.Loop() {
		s.PublishedObjects(revision)
	}
}
