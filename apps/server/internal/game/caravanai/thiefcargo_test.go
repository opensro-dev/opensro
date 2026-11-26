package caravanai

import (
	"context"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestHiredThiefKeepsItsHaulForTheHirer

A hired Thief carrying stolen goods is never teleported to its far-off
hirer; after its scroll lands it holds the haul in the Den until the
hirer stands there, then sells for the hirer and follows again. An
unhired Thief sells as it lands.
================
*/
func TestHiredThiefKeepsItsHaulForTheHirer(t *testing.T) {
	den := TownWaypoint(TownThiefDen)
	field := Waypoint{RegionID: 25239, X: 900, Z: 900}
	players := map[string]*hirePlayer{"Rogue": {at: den, job: domain.JobThief, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	rt.hooks.Move = func(_ string, from, to Waypoint, distance float32) (Waypoint, bool) {
		region, x, z, arrived := StepWaypoint(from.RegionID, from.X, from.Z, to, distance)
		return Waypoint{RegionID: region, X: x, Z: z}, arrived
	}
	if reply := rt.HireAgent("division", "Rogue", domain.JobThief, "[AI]Thief_Wisp", 1000); reply == "" {
		t.Fatal("no hire reply")
	}
	step := func(now int64) *AgentUnit {
		_, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Thief_Wisp", nil, now)
		return unit
	}
	// Out in the field with goods, the hirer far away in the Den.
	rt.mu.Lock()
	wisp := rt.findAgentLocked("division", "[AI]Thief_Wisp")
	wisp.place(field, 1000)
	wisp.pendingTeleport = nil
	rt.mu.Unlock()
	rt.NoteThiefCargo("division", "[AI]Thief_Wisp", true)
	if unit := step(2000); WaypointDistance(position(unit), den) <= EscortTeleportDistance {
		t.Fatal("a Thief carrying goods was teleported to its hirer")
	}

	// The scroll lands it in the Den; the hirer is away: it holds the haul.
	players["Rogue"].at = field
	if sellNow := rt.ThiefLandedInDen("division", "[AI]Thief_Wisp", den, 3000); sellNow {
		t.Fatal("a hired Thief sold without its hirer")
	}
	for now := int64(3100); now < 8000; now += 100 {
		if unit := step(now); WaypointDistance(position(unit), den) > denRadius {
			t.Fatal("a Thief holding its haul left the Den")
		}
	}
	if _, sell := rt.ThiefDenSale("division", "[AI]Thief_Wisp"); sell {
		t.Fatal("the haul sold before the hirer came")
	}
	// The hirer comes: it sells for the hirer, once.
	players["Rogue"].at = den
	if hirer, sell := rt.ThiefDenSale("division", "[AI]Thief_Wisp"); !sell || hirer != "Rogue" {
		t.Fatalf("the haul did not sell for the hirer: %q %v", hirer, sell)
	}
	if _, sell := rt.ThiefDenSale("division", "[AI]Thief_Wisp"); sell {
		t.Fatal("the haul sold twice")
	}

	// An unhired Thief sells as it lands.
	if !rt.ThiefLandedInDen("division", "[AI]Thief_Viper", den, 9000) {
		t.Fatal("an unhired Thief held its haul")
	}
}
