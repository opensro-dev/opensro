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

/*
================
TestReboundNamesOnlyTheSameCharacterAgain

A resumed transport's repeated EnterWorld rebinds the character the session
already carried; a first entry, another character or an entry after the
context was cleared is a new admission.
================
*/
func TestReboundNamesOnlyTheSameCharacterAgain(t *testing.T) {
	hub := newHub(testCfg())
	session := &Session{ID: 1, hub: hub}

	session.BindCharacter("global-official", "asd2", 0)
	if session.Rebound() {
		t.Fatal("a first entry counted as a rebind")
	}
	session.BindCharacter("global-official", "asd2", 0)
	if !session.Rebound() {
		t.Fatal("the same character on the same session was not a rebind")
	}
	session.BindCharacter("global-official", "another", 0)
	if session.Rebound() {
		t.Fatal("another character counted as a rebind")
	}
	session.BindCharacter("other-division", "another", 0)
	if session.Rebound() {
		t.Fatal("the same name in another division counted as a rebind")
	}
	session.ClearGameplayContext()
	if session.Rebound() {
		t.Fatal("a cleared context kept the rebind")
	}
	session.BindCharacter("other-division", "another", 0)
	if session.Rebound() {
		t.Fatal("an entry after the context was cleared counted as a rebind")
	}
}
