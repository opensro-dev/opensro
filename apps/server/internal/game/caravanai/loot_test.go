package caravanai

import (
	"context"
	"strings"
	"testing"
)

/*
================
TestAgentsWalkToLootAndPickItUp

A Thief on the road walks to the item it was shown and picks it up when
it stands there; a Trader under a parley truce leaves its goods be.
================
*/
func TestAgentsWalkToLootAndPickItUp(t *testing.T) {
	paid := map[string]int64{}
	rt := hireRuntime(t, map[string]*hirePlayer{}, &paid)
	var picked []uint32
	rt.hooks.PickUp = func(_, _ string, gid uint32) bool { picked = append(picked, gid); return true }
	field := Waypoint{RegionID: 25239, X: 1000, Z: 1000}
	loot := offsetPoint(field, 60, 0)
	rt.mu.Lock()
	crow := rt.findAgentLocked("division", "[AI]Thief_Crow")
	crow.place(field, 0)
	crow.State, crow.RunSpeed = StateTraveling, 50
	rt.mu.Unlock()
	if !rt.LootSought("division", "[AI]Thief_Crow", 1000) {
		t.Fatal("a Thief on the road does not look for loot")
	}
	rt.NoteLoot("division", "[AI]Thief_Crow", 77, loot, 1000)
	now := int64(1000)
	for i := 0; i < 40 && len(picked) == 0; i++ {
		rt.StepAgent(context.Background(), "division", "[AI]Thief_Crow", nil, now)
		now += 100
	}
	if len(picked) != 1 || picked[0] != 77 {
		t.Fatalf("the Thief did not pick the loot up: %v", picked)
	}

	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	jin.place(field, 0)
	jin.State = StateTraveling
	rt.mu.Unlock()
	rt.parleys.StartParley("division", "[AI]Trader_Jin", "Bandit", field.RegionID, 3, 300, 1000)
	if rt.LootSought("division", "[AI]Trader_Jin", 2000) {
		t.Fatal("a Trader recovers its goods during the truce it paid for")
	}
	if !rt.LootSought("division", "[AI]Trader_Jin", 1000+60_000) {
		t.Fatal("a Trader does not look for its goods after the truce")
	}
}

/*
================
TestHunterCallsHuntersAndAvengesItself

A Hunter below half HP calls the Hunters around (chat and a call only
Hunters answer); a Hunter killed by a Thief rallies idle Hunters of its
town and goes back after that Thief.
================
*/
func TestHunterCallsHuntersAndAvengesItself(t *testing.T) {
	paid := map[string]int64{}
	rt := hireRuntime(t, map[string]*hirePlayer{}, &paid)
	var said []string
	rt.hooks.OnAgentChat = func(_, agent string, _ uint16, text string) { said = append(said, agent+": "+text) }
	field := Waypoint{RegionID: 25239, X: 1000, Z: 1000}
	rt.mu.Lock()
	lei := rt.findAgentLocked("division", "[AI]Hunter_Lei")
	lei.place(field, 0)
	lei.State = StateTraveling
	rt.findAgentLocked("division", "[AI]Thief_Crow").place(offsetPoint(field, 40, 0), 0)
	rt.mu.Unlock()

	rt.RecordPlayerAttack("division", "[AI]Hunter_Lei", "[AI]Thief_Crow", lei.MaxHP*6/10, 1000)
	rt.mu.Lock()
	call, ok := rt.distress["division"]
	rt.mu.Unlock()
	if !ok || !call.Hunter || call.TraderName != "[AI]Hunter_Lei" {
		t.Fatalf("no Hunter call for help: %+v", call)
	}
	rt.flushOutbox("division")
	if len(said) == 0 || !strings.Contains(said[0], "Hunters, to me") {
		t.Fatalf("the Hunter said nothing: %v", said)
	}

	rt.RecordPlayerAttack("division", "[AI]Hunter_Lei", "[AI]Thief_Crow", lei.MaxHP, 2000)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if lei.Vengeance != "[AI]Thief_Crow" || lei.RunKind != RunVengeance {
		t.Fatalf("the killed Hunter does not hunt its killer: %q %q", lei.Vengeance, lei.RunKind)
	}
	gang := 0
	for _, u := range rt.agents["division"] {
		if u.RunKind == RunVengeance && u.EscortingTrader == lei.Name {
			if u.Role != RoleHunter {
				t.Fatalf("%s joined a Hunter's hunt", u.Name)
			}
			gang++
		}
	}
	if gang == 0 {
		t.Fatal("no idle Hunter answered")
	}
}
