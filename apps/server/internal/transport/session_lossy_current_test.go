/*
===========================================================================

session_lossy_current_test.go - the coalesced lane keeps the latest frame and
drops a superseded one (issue #155)

===========================================================================
*/

package transport

import "testing"

// TestSceneUnreliableKeyedCoalescesAndDropsStaleCurrent guards issue #155: a
// movement frame on the coalesced lane is replaced by the latest frame per key,
// and a superseded Current predicate drops the frame before it leaves.
func TestSceneUnreliableKeyedCoalescesAndDropsStaleCurrent(t *testing.T) {
	hub := newHub(testCfg())
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.closeSession(s, nil)
	revision, _ := s.SceneRevision()

	// Two frames for the same key: only the latest is queued.
	current := true
	_ = s.SendSceneUnreliableKeyed(revision, OpObjectSourceMove, 7, []byte{1}, func() bool { return current })
	_ = s.SendSceneUnreliableKeyed(revision, OpObjectSourceMove, 7, []byte{2}, func() bool { return current })
	s.mu.Lock()
	slots := len(s.lossyKeys)
	frame := s.lossy[lossySlot{Opcode: OpObjectSourceMove, Key: 7}]
	s.mu.Unlock()
	if slots != 1 {
		t.Fatalf("coalesce kept %d slots, want 1", slots)
	}
	if len(frame.Payload) != 1 || frame.Payload[0] != 2 {
		t.Fatalf("coalesce kept payload %v, want the latest [2]", frame.Payload)
	}
	if frame.Current == nil {
		t.Fatal("the Current predicate was dropped")
	}

	// A superseded frame is dropped before it leaves.
	stale := false
	_ = s.SendSceneUnreliableKeyed(revision, OpObjectSourceCorrection, 9, []byte{3}, func() bool { return stale })
	s.mu.Lock()
	_, pending := s.lossy[lossySlot{Opcode: OpObjectSourceCorrection, Key: 9}]
	s.mu.Unlock()
	if pending {
		t.Fatal("a superseded frame was queued")
	}
}
