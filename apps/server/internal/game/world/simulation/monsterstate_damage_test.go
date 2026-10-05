package simulation

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

func damageTestState() *MonsterState {
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			1933: {RefObjID: 1933, MaxHP: 54, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100},
		},
		[]monster.NestRow{{
			SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: 0x62AA, X: 100, Y: 20, Z: 100},
		}},
	)
	return NewMonsterState(template)
}

func firstDamageTestMonster(t *testing.T, state *MonsterState, divisionID string) monster.Instance {
	t.Helper()
	state.StartDivision(divisionID)
	state.AdvancePopulation(state.CurrentTimeMillis())
	instances := state.InstancesInRegions(divisionID, []uint16{0x62AA})
	if len(instances) == 0 {
		t.Fatal("test template materialized no monsters")
	}
	return instances[0]
}

func TestApplyDamageOwnsCurrentHPAndLeavesRemovalExplicit(t *testing.T) {
	registry := damageTestState()
	instance := firstDamageTestMonster(t, registry, "damage")

	hit, ok := registry.ApplyDamage("damage", instance.Gid, 7)
	if !ok {
		t.Fatal("ApplyDamage rejected a live monster")
	}
	if hit.BeforeHP != 54 || hit.Applied != 7 || hit.CurrentHP != 47 || hit.Fatal {
		t.Fatalf("first hit = %+v, want 54 -> 47 with 7 applied", hit)
	}
	if hit.Instance.CurrentHP != hit.CurrentHP {
		t.Fatalf("result snapshot HP = %d, committed HP = %d",
			hit.Instance.CurrentHP, hit.CurrentHP)
	}

	// Result snapshots are values; mutating one cannot become a second HP
	// owner outside the registry door.
	hit.Instance.CurrentHP = 999
	stored, found := registry.Get("damage", instance.Gid)
	if !found || stored.CurrentHP != 47 {
		t.Fatalf("stored monster after snapshot mutation = %+v, found %v", stored, found)
	}

	fatal, ok := registry.ApplyDamage("damage", instance.Gid, 1000)
	if !ok {
		t.Fatal("overkill rejected a live monster")
	}
	if fatal.BeforeHP != 47 || fatal.Applied != 47 ||
		fatal.CurrentHP != 0 || !fatal.Fatal {
		t.Fatalf("fatal hit = %+v, want 47 -> 0 with overkill clamped", fatal)
	}
	if _, found := registry.Get("damage", instance.Gid); !found {
		t.Fatal("fatal HP transition removed the entity before death publication")
	}

	afterDeath, ok := registry.ApplyDamage("damage", instance.Gid, 1)
	if !ok || afterDeath.Applied != 0 || afterDeath.Fatal ||
		afterDeath.CurrentHP != 0 {
		t.Fatalf("post-fatal hit = %+v, found %v; want stable zero HP", afterDeath, ok)
	}

	if !registry.Defeat("damage", instance.Gid, time.UnixMilli(10_000)) {
		t.Fatal("explicit lifecycle defeat rejected the zero-HP population monster")
	}
	if _, found := registry.Get("damage", instance.Gid); found {
		t.Fatal("explicit Defeat did not remove the population identity")
	}
}

func TestApplyDamageSerializesConcurrentHits(t *testing.T) {
	registry := damageTestState()
	instance := firstDamageTestMonster(t, registry, "concurrent-damage")

	var appliedTotal atomic.Uint32
	var fatalTransitions atomic.Uint32
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, ok := registry.ApplyDamage("concurrent-damage", instance.Gid, 10)
			if !ok {
				return
			}
			appliedTotal.Add(result.Applied)
			if result.Fatal {
				fatalTransitions.Add(1)
			}
		}()
	}
	wait.Wait()

	stored, ok := registry.Get("concurrent-damage", instance.Gid)
	if !ok {
		t.Fatal("damage door unexpectedly removed the instance")
	}
	if stored.CurrentHP != 0 {
		t.Fatalf("current HP = %d, want 0", stored.CurrentHP)
	}
	if appliedTotal.Load() != 54 {
		t.Fatalf("total applied damage = %d, want exactly the original 54 HP",
			appliedTotal.Load())
	}
	if fatalTransitions.Load() != 1 {
		t.Fatalf("fatal transitions = %d, want exactly one", fatalTransitions.Load())
	}
}

func TestApplyDamageMissingIdentityDoesNotInventState(t *testing.T) {
	registry := damageTestState()
	if result, ok := registry.ApplyDamage("missing", monster.GidBase+999, 1); ok {
		t.Fatalf("missing gid produced %+v", result)
	}
	if len(registry.MaterializedInstances("missing")) != 0 {
		t.Fatal("missing damage materialized or invented a monster")
	}
}

func TestNativeSignedDamageDoesNotTurnNegativeIntoFatalHit(t *testing.T) {
	for _, damage := range []uint32{0, 0x80000000, 0xffffffff} {
		s := damageTestState()
		m := firstDamageTestMonster(t, s, "signed-damage")
		rows := s.ApplyDamageSequence("signed-damage", m.Gid, m.CurrentHP,
			[]MonsterDamagePlan{{GID: m.Gid, Damage: damage, CreditGID: 77}})
		if len(rows) != 1 || rows[0].Applied != 0 || rows[0].Fatal || rows[0].CurrentHP != m.CurrentHP {
			t.Fatalf("damage %#x changed HP: %+v", damage, rows)
		}
		// NPC credit is accumulated before the common signed HP gate.
		if damage != 0 {
			credit := s.divs["signed-damage"].contributionSnapshot(m.Gid)
			if len(credit) != 1 || credit[0].Damage != damage {
				t.Fatalf("credit lost: %+v", credit)
			}
		}
	}
}

/*
================
TestAreaCandidatesSkipStructures

52BF90 admits only an aimed basic attack on a fortress structure, so the
area, chain and secondary candidate scans never return one.
================
*/
func TestAreaCandidatesSkipStructures(t *testing.T) {
	s := NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			1933:  {RefObjID: 1933, MaxHP: 54, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100},
			19536: {RefObjID: 19536, MaxHP: 500, ScaleDenom: 100, Structure: true},
		},
		[]monster.NestRow{
			{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: 0x62AA, X: 100, Y: 20, Z: 100}},
			{SpawnPoint: monster.SpawnPoint{RefObjID: 19536, RegionID: 0x62AA, X: 102, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1},
		},
	))
	s.StartDivision("area")
	s.AdvancePopulation(s.CurrentTimeMillis())
	if n := len(s.InstancesInRegions("area", []uint16{0x62AA})); n != 2 {
		t.Fatalf("fixture materialized %d instances, want the monster and the structure", n)
	}
	center := Spawn{RegionID: 0x62AA, X: 101, Y: 20, Z: 100}
	got := s.CombatCandidatesInSphere("area", center, 20, s.CurrentTimeMillis())
	if len(got) != 1 || got[0].Ref.Structure {
		t.Fatalf("area candidates %+v, want only the monster", got)
	}
}
