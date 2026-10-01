package transport

import "testing"

func TestWorldReadyIsOneShotPerCharacterBinding(t *testing.T) {
	hub := newHub(testCfg())
	session := &Session{ID: 1, hub: hub}

	if session.TryMarkWorldReady() {
		t.Fatal("unbound session crossed world-ready")
	}
	session.BindCharacter("global-official", "asd2", 0)
	if session.WorldReady() {
		t.Fatal("BindCharacter published world-ready before scene admission")
	}
	if !session.TryMarkWorldReady() {
		t.Fatal("first game-ready transition was refused")
	}
	if session.TryMarkWorldReady() {
		t.Fatal("duplicate game-ready transition was accepted")
	}

	session.BindCharacter("global-official", "another", 0)
	if session.WorldReady() {
		t.Fatal("new character binding inherited prior world-ready state")
	}
	if !session.TryMarkWorldReady() {
		t.Fatal("new character binding could not cross world-ready")
	}
	session.ClearGameplayContext()
	if session.WorldReady() {
		t.Fatal("cleared gameplay context retained world-ready state")
	}
}
