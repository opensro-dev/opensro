package transport

import "testing"

func TestSceneResetRetiresQueuedMotionAndRejectsStaleVisibility(t *testing.T) {
	hub := newHub(testCfg())
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.closeSession(s, nil)
	old, _ := s.SceneRevision()
	if err := s.SendSceneUnreliableKeyed(old, OpObjectSourceCorrection, 7, make([]byte, 20), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SendSceneReset([]Frame{{Opcode: 0x3369, Payload: []byte{1, 2}}, {Opcode: 0x330a}}); err != nil {
		t.Fatal(err)
	}
	current, active := s.SceneRevision()
	if current == old || active {
		t.Fatal("reset did not open new admission")
	}
	_ = s.SendSceneBatch(old, []Frame{{Opcode: 0x30d7}})
	_ = s.SendSceneUnreliableKeyed(old, OpObjectSourceCorrection, 7, make([]byte, 20), nil)
	_ = s.SendSceneBatch(current, []Frame{{Opcode: 0x30d7}})
	s.mu.Lock()
	count, lossy := len(s.queue), len(s.lossyKeys)
	s.mu.Unlock()
	if count != 2 || lossy != 0 {
		t.Fatalf("loading admits stale work: reliable=%d lossy=%d", count, lossy)
	}
	if !s.FinishSceneReentry() {
		t.Fatal("ready not accepted")
	}
	_ = s.SendSceneBatch(old, []Frame{{Opcode: 0x30d7}})
	_ = s.SendSceneBatch(current, []Frame{{Opcode: 0x30d7}})
	s.mu.Lock()
	count = len(s.queue)
	s.mu.Unlock()
	if count != 3 {
		t.Fatalf("ready did not admit exactly the current scene: %d", count)
	}
}
