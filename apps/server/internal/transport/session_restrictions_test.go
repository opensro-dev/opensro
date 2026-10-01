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
