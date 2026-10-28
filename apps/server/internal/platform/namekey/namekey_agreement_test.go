/*
===========================================================================

namekey_agreement_test.go - both key owners compose the same key

social.BindKey (the hub's session bindings) and simulation.WorldKey
(the live-world store) delegate to this package; if either ever grows
its own composition again, the two owners stop finding each other's
entries and characters silently lose their sessions or world state.
Pin them to each other, not just to the helper.

===========================================================================
*/

package namekey_test

import (
	"testing"

	"opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/platform/namekey"
)

/*
================
TestBothOwnersComposeTheSameKey
================
*/
func TestBothOwnersComposeTheSameKey(t *testing.T) {
	for _, name := range []string{"already-lower", "MixedCase", "ALL_CAPS", "Hong길동A"} {
		bind, world := social.BindKey("division", name), simulation.WorldKey("division", name)
		if bind != world || bind != namekey.Key("division", name) {
			t.Errorf("name %q: BindKey %q, WorldKey %q, Key %q - all three must agree", name, bind, world, namekey.Key("division", name))
		}
	}
}
