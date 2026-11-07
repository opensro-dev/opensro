package transport

import "testing"

func TestCommandRestrictionsKeepNativeSessionOwnership(t *testing.T) {
	s := &Session{ID: 1, hub: newHub(testCfg())}
	chat := [8]uint16{2001, 2, 3, 4, 5, 6, 7, 8} // old date still enabled until explicitly cleared
	trade := [8]uint16{2026, 9, 2, 22, 13, 45, 30, 0}
	if !s.SetCommandRestriction(CommandRestrictionChat, chat) || !s.SetCommandRestriction(CommandRestrictionTrade, trade) {
		t.Fatal("install refused")
	}
	s.BindCharacter("division", "first", 0)
	s.BindCharacter("division", "second", 0)
	got, ok := s.CommandRestriction(CommandRestrictionChat)
	if !ok || got != chat {
		t.Fatal("binding changed restriction")
	}
	got[0] = 9999
	got, ok = s.CommandRestriction(CommandRestrictionChat)
	if !ok || got != chat {
		t.Fatal("snapshot aliased record")
	}
	s.ClearCommandRestriction(CommandRestrictionChat)
	if _, ok = s.CommandRestriction(CommandRestrictionChat); ok {
		t.Fatal("clear failed")
	}
	if got, ok = s.CommandRestriction(CommandRestrictionTrade); !ok || got != trade {
		t.Fatal("clearing chat changed trade")
	}
	fresh := &Session{}
	if _, ok = fresh.CommandRestriction(CommandRestrictionTrade); ok {
		t.Fatal("restriction leaked across sessions")
	}
	for _, kind := range []uint8{0, 1, 2, 5, 255} {
		if s.SetCommandRestriction(kind, chat) || s.ClearCommandRestriction(kind) {
			t.Fatal("invalid selector accepted")
		}
		if _, ok = s.CommandRestriction(kind); ok {
			t.Fatal("invalid selector active")
		}
	}
}

/*
================
TestCharacterRestrictionOutlivesItsSession

A restriction installed on a character-bound session is recorded against
the character: the by-character read still reports it after the session
is gone (the offline-mute residue), until a later bound session clears
it. Kinds stay independent and the name folds as names compare.
================
*/
func TestCharacterRestrictionOutlivesItsSession(t *testing.T) {
	hub := newHub(testCfg())
	stamp := [8]uint16{2026, 1, 0, 1, 0, 0, 0, 0}

	s, _ := newAttachedSession(t, hub, false)
	s.BindCharacter("DIV_A", "Muted", 0)
	if !s.SetCommandRestriction(CommandRestrictionChat, stamp) {
		t.Fatal("install refused")
	}
	if !hub.CharacterRestricted("DIV_A", "muted", CommandRestrictionChat) {
		t.Fatal("online muted author not restricted by character")
	}
	if hub.CharacterRestricted("DIV_A", "Muted", CommandRestrictionTrade) {
		t.Fatal("chat restriction leaked into the trade kind")
	}

	hub.closeSession(s, nil)
	if got := hub.CharacterSessions("DIV_A", "Muted"); len(got) != 0 {
		t.Fatalf("session survived its close: %v", got)
	}
	if !hub.CharacterRestricted("DIV_A", "Muted", CommandRestrictionChat) {
		t.Fatal("restriction ended at logoff: the by-character record is missing")
	}

	fresh, _ := newAttachedSession(t, hub, false)
	fresh.BindCharacter("DIV_A", "Muted", 0)
	if !fresh.ClearCommandRestriction(CommandRestrictionChat) {
		t.Fatal("clear refused")
	}
	if hub.CharacterRestricted("DIV_A", "Muted", CommandRestrictionChat) {
		t.Fatal("cleared restriction still reported")
	}
	if hub.CharacterRestricted("DIV_A", "Muted", 0) || hub.CharacterRestricted("DIV_A", "Muted", 5) {
		t.Fatal("invalid selector reported restricted")
	}
}

/*
================
TestCharacterRestrictionNeedsABoundInstall

An install on a session with no bound character records nothing against
anybody: there is no character to record against.
================
*/
func TestCharacterRestrictionNeedsABoundInstall(t *testing.T) {
	hub := newHub(testCfg())
	stamp := [8]uint16{2026, 1, 0, 1, 0, 0, 0, 0}

	s, _ := newAttachedSession(t, hub, false)
	if !s.SetCommandRestriction(CommandRestrictionChat, stamp) {
		t.Fatal("install refused")
	}
	hub.closeSession(s, nil)
	if hub.CharacterRestricted("DIV_A", "Muted", CommandRestrictionChat) {
		t.Fatal("unbound install was recorded against a character")
	}
}

/*
================
TestCharacterRestrictionFollowsARelog

#2671: a mute installed by character reaches every live session of the
character, and a session bound to the character later picks it up at
bind, so a relog no longer walks out of it. Clearing reaches both.
================
*/
func TestCharacterRestrictionFollowsARelog(t *testing.T) {
	hub := newHub(testCfg())
	until := [8]uint16{2026, 11, 4, 6, 12, 0, 0, 0}

	live, _ := newAttachedSession(t, hub, false)
	live.BindCharacter("DIV_A", "Loud", 0)
	if !hub.SetCharacterRestriction("DIV_A", "loud", CommandRestrictionChat, until) {
		t.Fatal("install refused")
	}
	if got, ok := live.CommandRestriction(CommandRestrictionChat); !ok || got != until {
		t.Fatalf("live session not muted: %v %v", got, ok)
	}

	relog, _ := newAttachedSession(t, hub, false)
	relog.BindCharacter("DIV_A", "Loud", 0)
	if got, ok := relog.CommandRestriction(CommandRestrictionChat); !ok || got != until {
		t.Fatalf("relog escaped the mute: %v %v", got, ok)
	}
	if _, ok := relog.CommandRestriction(CommandRestrictionTrade); ok {
		t.Fatal("chat mute leaked into trade")
	}

	other, _ := newAttachedSession(t, hub, false)
	other.BindCharacter("DIV_A", "Quiet", 0)
	if _, ok := other.CommandRestriction(CommandRestrictionChat); ok {
		t.Fatal("mute reached another character")
	}

	hub.ClearCharacterRestriction("DIV_A", "Loud", CommandRestrictionChat)
	if _, ok := relog.CommandRestriction(CommandRestrictionChat); ok {
		t.Fatal("clear missed the live session")
	}
	if hub.CharacterRestricted("DIV_A", "Loud", CommandRestrictionChat) {
		t.Fatal("clear left the hub record")
	}
	late, _ := newAttachedSession(t, hub, false)
	late.BindCharacter("DIV_A", "Loud", 0)
	if _, ok := late.CommandRestriction(CommandRestrictionChat); ok {
		t.Fatal("a cleared mute came back at bind")
	}
	if hub.SetCharacterRestriction("DIV_A", "Loud", 2, until) || hub.SetCharacterRestriction("DIV_A", "", CommandRestrictionChat, until) {
		t.Fatal("bad selector or empty name accepted")
	}
}
