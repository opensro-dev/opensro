/*
===========================================================================

scheduler_commands_test.go - the players' caravan chat commands are rate-limited

Main's slice 4 answered one caravan command per player per five seconds
and consumed the excess silently; the fork's command set keeps that rule.

===========================================================================
*/
package caravanai

import (
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
==================
TestPlayerCommandsAreRateLimited

Unrecognized text stays chat; a player's second command inside the
cooldown is consumed with no reply; the limit is per player; after the
cooldown the player is answered again.
==================
*/
func TestPlayerCommandsAreRateLimited(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true, BrainMode: BrainHardcoded}, nil, nil, WorldHooks{})
	t.Cleanup(func() { _ = rt.Close() })
	const div = "Shard_Silkroad"
	rt.EnsureDivisionRoster(div)
	rt.manualRuns = true

	if reply, handled := rt.HandlePlayerCommand(div, "Scout", domain.JobHunter, "!dance [AI]Trader_Jin", 10_000); handled || reply != "" {
		t.Fatalf("unrecognized text is not a command, got handled=%v reply=%q", handled, reply)
	}
	if reply, handled := rt.HandlePlayerCommand(div, "Scout", domain.JobHunter, "!attack [AI]Trader_Jin", 10_100); !handled || reply == "" {
		t.Fatalf("the first command is answered, got handled=%v reply=%q", handled, reply)
	}
	if reply, handled := rt.HandlePlayerCommand(div, "SCOUT", domain.JobHunter, "!memory [AI]Trader_Jin", 12_000); !handled || reply != "" {
		t.Fatalf("inside the cooldown the command is consumed silently, whatever the name's case; got handled=%v reply=%q", handled, reply)
	}
	if reply, handled := rt.HandlePlayerCommand(div, "Other", domain.JobThief, "!memory [AI]Trader_Jin", 12_500); !handled || reply == "" {
		t.Fatalf("the limit is per player, got handled=%v reply=%q", handled, reply)
	}
	if reply, handled := rt.HandlePlayerCommand(div, "Scout", domain.JobHunter, "!memory [AI]Trader_Jin", 10_100+playerCommandCooldownMs); !handled || !strings.Contains(reply, "Trust:") {
		t.Fatalf("after the cooldown the player is answered again, got handled=%v reply=%q", handled, reply)
	}
}
