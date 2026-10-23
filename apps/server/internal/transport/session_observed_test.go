package transport

import (
	"sync"
	"testing"
)

func TestObservedPublicationOrdersSpawnActionDespawn(t *testing.T) {
	hub := newHub(testCfg())
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.closeSession(s, nil)
	revision, _ := s.SceneRevision()
	const gid = 81
	action := []Frame{{Opcode: 0x324b}}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.SendObservedBatch(revision, gid, action))
	check(s.PublishSceneObjects(revision, []ObjectScopeChange{{GID: gid, Visible: true}}, []Frame{{Opcode: 0x30d7}, {Opcode: 0x3122}}))
	check(s.SendObservedBatch(revision, gid, action))
	check(s.SendSceneUnreliableKeyed(revision, OpObjectSourceMove, gid, make([]byte, 20)))
	check(s.SendSceneUnreliableKeyed(revision, OpObjectSourceCorrection, gid+1, make([]byte, 20)))
	check(s.PublishSceneObjects(revision, []ObjectScopeChange{{GID: gid, Visible: false}}, []Frame{{Opcode: 0x36ab}}))
	check(s.SendObservedBatch(revision, gid, action))
	s.mu.Lock()
	defer s.mu.Unlock()
	want := []uint16{0x30d7, 0x3122, 0x324b, 0x36ab}
	if len(s.lossyKeys) != 1 || s.lossyKeys[0].Key != gid+1 || s.lossyBytes != 22 {
		t.Fatalf("departure retained retired motion or removed another object: %v / %d", s.lossyKeys, s.lossyBytes)
	}
	if len(s.queue) != len(want) {
		t.Fatalf("unexpected publication: %+v", s.queue)
	}
	for i, op := range want {
		if s.queue[i].Opcode != op {
			t.Fatalf("frame %d: %x != %x", i, s.queue[i].Opcode, op)
		}
	}
}

func TestObservedPublicationSceneReplacement(t *testing.T) {
	hub := newHub(testCfg())
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.closeSession(s, nil)
	old, _ := s.SceneRevision()
	spawn := []Frame{{Opcode: 0x30d7}}
	change := []ObjectScopeChange{{GID: 81, Visible: true}}
	_ = s.PublishSceneObjects(old, change, spawn)
	s.BeginSceneAdmission()
	_ = s.PublishSceneObjects(old, change, spawn)
	if !s.FinishSceneReentry() {
		t.Fatal("scene did not resume")
	}
	current, _ := s.SceneRevision()
	_ = s.SendObservedBatch(current, 81, []Frame{{Opcode: 0x324b}})
	_ = s.PublishSceneObjects(old, change, spawn)
	_ = s.PublishSceneObjects(current, change, spawn)
	_ = s.SendObservedBatch(old, 81, []Frame{{Opcode: 0x324b}})
	_ = s.SendObservedBatch(current, 81, []Frame{{Opcode: 0x324b}})
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) != 3 || s.queue[2].Opcode != 0x324b {
		t.Fatalf("stale scene leaked: %+v", s.queue)
	}
}

func TestObservedConcurrentDeparture(t *testing.T) {
	cfg := testCfg()
	cfg.OutboundQueue = 1024
	hub := newHub(cfg)
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.closeSession(s, nil)
	revision, _ := s.SceneRevision()
	_ = s.PublishSceneObjects(revision, []ObjectScopeChange{{GID: 81, Visible: true}}, []Frame{{Opcode: 0x30d7}})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.SendObservedBatch(revision, 81, []Frame{{Opcode: 0x324b}}) }()
	}
	_ = s.PublishSceneObjects(revision, []ObjectScopeChange{{GID: 81, Visible: false}}, []Frame{{Opcode: 0x36ab}})
	wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queue[0].Opcode != 0x30d7 || s.queue[len(s.queue)-1].Opcode != 0x36ab {
		t.Fatalf("action escaped lifecycle: %+v", s.queue)
	}
}

/*
================
TestUnobservedPublicationIntroducesOnce

SendUnobservedBatch introduces an object only to a live scene that lacks it:
never twice, never while loading, never after its removal is forgotten by a
stale revision.
================
*/
func TestUnobservedPublicationIntroducesOnce(t *testing.T) {
	hub := newHub(testCfg())
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.closeSession(s, nil)
	const gid = 81
	spawn := []Frame{{Opcode: 0x30d7, Scope: []ObjectScopeChange{{GID: gid, Visible: true}}}}
	s.BeginSceneAdmission()
	loading, _ := s.SceneRevision()
	_ = s.SendUnobservedBatch(loading, gid, spawn)
	if !s.FinishSceneReentry() {
		t.Fatal("scene did not resume")
	}
	revision, _ := s.SceneRevision()
	_ = s.SendUnobservedBatch(revision, gid, spawn)
	_ = s.SendUnobservedBatch(revision, gid, spawn)
	_ = s.PublishSceneObjects(revision, []ObjectScopeChange{{GID: gid, Visible: false}}, []Frame{{Opcode: 0x36ab}})
	_ = s.SendUnobservedBatch(revision, gid, spawn)
	s.mu.Lock()
	defer s.mu.Unlock()
	want := []uint16{0x30d7, 0x36ab, 0x30d7}
	if len(s.queue) != len(want) {
		t.Fatalf("unexpected publication: %+v", s.queue)
	}
	for i, op := range want {
		if s.queue[i].Opcode != op {
			t.Fatalf("frame %d: %x != %x", i, s.queue[i].Opcode, op)
		}
	}
	if _, observed := s.observedObjects[gid]; !observed {
		t.Fatal("the reintroduced object is not in scope")
	}
}
