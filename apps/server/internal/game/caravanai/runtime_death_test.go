/*
===========================================================================

runtime_death_test.go - agent death and respawn (port-only, #3704 slice 3b)

A defeated agent dies where it stands, leaves the world (the entity
snapshot omits it, which is what despawns the corpse for viewers) and
reappears at its home town once the respawn delay has passed. Blows on
the corpse land nowhere and do not extend the wait. The thief Viper is
the subject: he waits at a ferry dock, far from his Donwhang home, so
"dies where he stands" and "respawns at home" are distinct poses.

===========================================================================
*/
package caravanai

import (
	"context"
	"testing"
)

/*
==================
agentEntityByName

The named division entity, or false when the agent is not in the world.
==================
*/
func agentEntityByName(entities []AgentEntity, name string) (AgentEntity, bool) {
	for _, entity := range entities {
		if entity.Name == name {
			return entity, true
		}
	}
	return AgentEntity{}, false
}

/*
==================
TestAgentDeathAndRespawn

Drives Thief_Viper: a lethal RecordPlayerAttack kills him at the dock,
further blows on the corpse do nothing, stepping before the deadline
keeps him dead, and stepping at the deadline respawns him at the
Donwhang anchor (height included) at full vitals, back in the world.
==================
*/
func TestAgentDeathAndRespawn(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true, BrainMode: BrainHardcoded}, nil, nil, WorldHooks{})
	const div = "death-test"
	rt.EnsureDivisionRoster(div)

	before, ok := agentEntityByName(rt.AgentEntities(div), "[AI]Thief_Viper")
	if !ok {
		t.Fatal("Viper is not in the world before the fight")
	}

	_, defeated, _ := rt.RecordPlayerAttack(div, "[AI]Thief_Viper", "IronGuard", 99_999, 10_000)
	if !defeated {
		t.Fatal("a 99999 blow on 11000 HP did not defeat Viper")
	}
	if _, ok := agentEntityByName(rt.AgentEntities(div), "[AI]Thief_Viper"); ok {
		t.Fatal("the dead Viper is still among the world entities")
	}

	// The corpse takes no damage and the respawn is not extended.
	if _, defeated, _ := rt.RecordPlayerAttack(div, "[AI]Thief_Viper", "IronGuard", 99_999, 20_000); defeated {
		t.Fatal("a blow on the corpse defeated Viper a second time")
	}
	_, unit, ok := rt.StepAgent(context.Background(), div, "[AI]Thief_Viper", nil, 10_000+agentRespawnDelayMs-1)
	if !ok || unit.State != StateDead || unit.HP != 0 {
		t.Fatalf("before the deadline Viper should stay dead, got ok=%v unit=%+v", ok, unit)
	}
	if unit.RegionID != before.Pose.RegionID || unit.X != before.Pose.X || unit.Z != before.Pose.Z {
		t.Fatalf("the corpse moved: pose %d/%f/%f, fell at %d/%f/%f",
			unit.RegionID, unit.X, unit.Z, before.Pose.RegionID, before.Pose.X, before.Pose.Z)
	}

	_, unit, ok = rt.StepAgent(context.Background(), div, "[AI]Thief_Viper", nil, 10_000+agentRespawnDelayMs)
	if !ok || unit.State == StateDead {
		t.Fatalf("at the deadline Viper should be back, got ok=%v state=%v", ok, unit.State)
	}
	home := TownWaypoint(TownDonwhang)
	if unit.HP != unit.MaxHP || unit.MP != unit.MaxMP {
		t.Fatalf("respawn vitals = %d/%d HP, %d/%d MP, want full", unit.HP, unit.MaxHP, unit.MP, unit.MaxMP)
	}
	if unit.RegionID != home.RegionID || unit.X != home.X || unit.Y != home.Y || unit.Z != home.Z {
		t.Fatalf("respawn pose = %d/%f/%f/%f, want the Donwhang anchor %+v",
			unit.RegionID, unit.X, unit.Y, unit.Z, home)
	}
	back, ok := agentEntityByName(rt.AgentEntities(div), "[AI]Thief_Viper")
	if !ok {
		t.Fatal("the respawned Viper is not among the world entities")
	}
	if back.Pose.RegionID != home.RegionID || back.Pose.X != home.X || back.Pose.Y != home.Y || back.Pose.Z != home.Z {
		t.Fatalf("respawned entity pose = %+v, want the Donwhang anchor %+v", back.Pose, home)
	}
}
