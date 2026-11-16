/*
===========================================================================

nasrun_test.go - the Spirit is classified by TID3, the avatars by TID4

===========================================================================
*/
package nasrun

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestSocketClassifiesAvatarsAndTheSpirit
================
*/
func TestSocketClassifiesAvatarsAndTheSpirit(t *testing.T) {
	cases := []struct {
		name   string
		flags  uint16
		socket int
		ok     bool
	}{
		{"hat", wire.PackTypeFlags(3, 1, 13, 1), 1, true},
		{"dress", wire.PackTypeFlags(3, 1, 13, 2), 2, true},
		{"attachment", wire.PackTypeFlags(3, 1, 13, 3), 3, true},
		{"flag", wire.PackTypeFlags(3, 1, 13, 4), 4, true},
		// The Spirit's TID4 is 1, the hat's: TID3 decides.
		{"spirit", wire.PackTypeFlags(3, 1, 14, 1), SpiritSocket, true},
		{"avatar with no socket", wire.PackTypeFlags(3, 1, 13, 0), 0, false},
		{"armor", wire.PackTypeFlags(3, 1, 1, 1), 0, false},
		{"potion", wire.PackTypeFlags(3, 3, 1, 1), 0, false},
	}
	for _, c := range cases {
		socket, ok := Socket(c.flags)
		if socket != c.socket || ok != c.ok {
			t.Errorf("%s: Socket(%#x) = %d, %v; want %d, %v", c.name, c.flags, socket, ok, c.socket, c.ok)
		}
	}
}

/*
================
TestEnabledIsOffUnlessNamed
================
*/
func TestEnabledIsOffUnlessNamed(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "0": false, "maybe": false, "1": true, " ON ": true, "true": true} {
		if Enabled(value) != want {
			t.Errorf("Enabled(%q) = %v", value, !want)
		}
	}
}
