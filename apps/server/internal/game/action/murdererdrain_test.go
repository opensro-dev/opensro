/*
===========================================================================

murdererdrain_test.go - the murderer status HP drain on the recovery pulse

The drain is the natural recovery pulse's murderer branch, so the tests
drive advanceNaturalRecovery: 2% of the maximum on the pulse, truncated,
outside a safe region, replacing recovery; motion and an open cast do not
stop it; a town, an offline record, a corpse and a clean record lose
nothing; a lethal pulse settles through the shared fatal transition.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	murdererDrainTestPenalty = 5000
	drainTestFirstPulseMs    = 4000
)

/*
================
vitalsHPRefresh

The HP carried by a 0x33A6 HP-only refresh, and whether a frame is one.
================
*/
func vitalsHPRefresh(frame simulation.Frame) (uint32, bool) {
	if frame.Opcode != simulation.OpVitalsUpdate || len(frame.Payload) != 11 || frame.Payload[6] != 0x01 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(frame.Payload[7:11]), true
}

/*
================
drainTestMurderer

A wounded murderer outside a town: wounded so that a recovery pulse would
show as a gain, and with a resident session bound.
================
*/
func drainTestMurderer(t *testing.T) (*Runtime, *enterworld.Character, int64, int64) {
	t.Helper()
	c := testCharacter()
	maximum := enterworld.DerivedMaxHP(c)
	hp := maximum / 2
	c.CurrentHP = &hp
	c.PK = &domain.PKRecord{Penalty: murdererDrainTestPenalty}
	rt, clock := newTestRuntime(c, testItems())
	rt.BindRecoverySession(testDivision, c, 1)
	_, field := regionsByCombat(t)
	drainTestWorld(rt, c, func(state *simulation.WorldState) { state.Spawn.RegionID = field })
	return rt, c, maximum, clock.NowMs()
}

/*
================
drainTestWorld

Edits the character's live world state.
================
*/
func drainTestWorld(rt *Runtime, c *enterworld.Character, edit func(state *simulation.WorldState)) {
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState {
		return simulation.SeedWorldState(c)
	}, edit)
}

/*
================
TestMurdererDrainsOnTheRecoveryPulse

The pulse before the period is silent; the pulse on it takes the truncated
2% and gives nothing back, publishing to the victim and observers.
================
*/
func TestMurdererDrainsOnTheRecoveryPulse(t *testing.T) {
	rt, c, maximum, start := drainTestMurderer(t)
	hp := enterworld.CurrentHP(c)
	if frames := rt.advanceNaturalRecovery(start + drainTestFirstPulseMs - 1); len(frames) != 0 {
		t.Fatalf("drained before the pulse: %+v", frames)
	}
	frames := rt.advanceNaturalRecovery(start + drainTestFirstPulseMs)
	want := hp - pk.DrainAmount(maximum)
	if got := enterworld.CurrentHP(c); got != want || want == hp {
		t.Fatalf("HP %d on the pulse, want %d from %d", got, want, hp)
	}
	if len(frames) != 2 {
		t.Fatalf("drain published %+v, want the victim refresh and the observer refresh", frames)
	}
	gid := enterworld.ObjectIDForCharacter(c)
	var private, public []simulation.Frame
	for _, routed := range frames {
		if routed.OnlyCharacterID == c.ID {
			private = append(private, routed.Frames...)
			continue
		}
		if routed.SourceGID != gid {
			t.Fatalf("public drain batch names source %d, want %d", routed.SourceGID, gid)
		}
		public = append(public, routed.Frames...)
	}
	if !hasSimOpcode(private, simulation.OpVitalsUpdate) {
		t.Fatalf("victim frames %+v lack the vitals refresh", private)
	}
	observed, ok := uint32(0), false
	for _, frame := range public {
		if value, isVitals := vitalsHPRefresh(frame); isVitals {
			observed, ok = value, true
		}
	}
	if !ok || observed != uint32(want) {
		t.Fatalf("observers saw HP %d (refresh %v), want %d", observed, ok, want)
	}
}

/*
================
TestMurdererDrainIgnoresTheRecoveryGates

Motion and posture gate recovery, not the drain, which comes first on the
pulse.
================
*/
func TestMurdererDrainIgnoresTheRecoveryGates(t *testing.T) {
	rt, c, maximum, start := drainTestMurderer(t)
	hp := enterworld.CurrentHP(c)
	now := start + drainTestFirstPulseMs
	drainTestWorld(rt, c, func(state *simulation.WorldState) { state.PostureTransitionUntilMs = now + 1 })
	rt.advanceNaturalRecovery(now)
	if got, want := enterworld.CurrentHP(c), hp-pk.DrainAmount(maximum); got != want {
		t.Fatalf("HP %d under a posture transition, want %d", got, want)
	}
}

/*
================
TestMurdererRecoversInASafeRegion

A town is the one place a murderer's pulse is still recovery.
================
*/
func TestMurdererRecoversInASafeRegion(t *testing.T) {
	town, _ := regionsByCombat(t)
	rt, c, _, start := drainTestMurderer(t)
	drainTestWorld(rt, c, func(state *simulation.WorldState) { state.Spawn.RegionID = town })
	hp := enterworld.CurrentHP(c)
	rt.advanceNaturalRecovery(start + drainTestFirstPulseMs)
	if got := enterworld.CurrentHP(c); got <= hp {
		t.Fatalf("HP %d in a town, want a gain from %d", got, hp)
	}
}

/*
================
TestRecoveryStaysForCleanAndOfflineRecords

A record with PK history but no penalty recovers as anyone does, and an
unbound murderer has no pulse at all.
================
*/
func TestRecoveryStaysForCleanAndOfflineRecords(t *testing.T) {
	rt, c, _, start := drainTestMurderer(t)
	c.PK = &domain.PKRecord{TotalCount: 3}
	hp := enterworld.CurrentHP(c)
	rt.advanceNaturalRecovery(start + drainTestFirstPulseMs)
	if got := enterworld.CurrentHP(c); got <= hp {
		t.Fatalf("clean record HP %d, want a gain from %d", got, hp)
	}

	offline := testCharacter()
	full := enterworld.DerivedMaxHP(offline)
	offline.CurrentHP = &full
	offline.PK = &domain.PKRecord{Penalty: murdererDrainTestPenalty}
	rt, clock := newTestRuntime(offline, testItems())
	if frames := rt.advanceNaturalRecovery(clock.NowMs() + drainTestFirstPulseMs); len(frames) != 0 {
		t.Fatalf("an offline murderer was drained: %+v", frames)
	}
}

/*
================
TestMurdererDrainDeathIsAMurdererDeath

A lethal pulse reuses the shared fatal transition: the death is classified
as pk.DeathPlayer, the penalty drops by 200, the daily count by one, and
the murderer drop roll runs (penalty 30000 always drops). The fixture's
rand() is 100, so the equipped roll lands on an empty slot and the bag
potion goes.
================
*/
func TestMurdererDrainDeathIsAMurdererDeath(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	rt.BindRecoverySession(testDivision, c, 1)
	_, field := regionsByCombat(t)
	drainTestWorld(rt, c, func(state *simulation.WorldState) { state.Spawn.RegionID = field })
	c.Level = testInt64(11)
	c.MaxLevel = testInt64(11)
	hp := int64(1)
	c.CurrentHP = &hp
	day := clock.Now().Local()
	c.PK = &domain.PKRecord{Penalty: 30000, DailyCount: 1, DailyDay: int32(day.Year()*10000 + int(day.Month())*100 + day.Day())}
	potion := &enterworld.ItemRef{RefObjID: 9, Codename: "ITEM_ETC_HP_POTION_01", TypeIDs: [4]int64{3, 3, 1, 1}, CanDropOnDeath: true}
	rt.deps.ItemReferences().(staticItemSource)[potion.Codename] = potion
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 13, RefObjID: potion.RefObjID,
		Codename: potion.Codename, TypeFlags: potion.TypeFlags(), VarianceBits: "0", StackCount: 5})
	var loss pk.LossRule
	rt.ApplyDeathPenalty = func(_ *enterworld.Character, penalty pk.DeathPenalty) ([]wire.Frame, bool) {
		loss = penalty.Rule
		return nil, true
	}

	frames := rt.advanceNaturalRecovery(clock.NowMs() + drainTestFirstPulseMs)

	if got := enterworld.CurrentHP(c); got != 0 {
		t.Fatalf("HP %d after the lethal pulse, want the corpse", got)
	}
	if c.DeathKind != uint8(pk.DeathPlayer) {
		t.Fatalf("death kind %d, want a player death", c.DeathKind)
	}
	// No killer object: DeathLoss reads the killer-less murderer row, the
	// same row a monster's blow takes.
	if loss != (pk.LossRule{Percent: 0.06, CapFactor: 300, SP: 60}) {
		t.Fatalf("death loss %+v, want the killer-less murderer row", loss)
	}
	if c.PK.Penalty != 30000-200 || c.PK.DailyCount != 0 {
		t.Fatalf("record %+v, want penalty 29800 and daily 0", c.PK)
	}
	for _, row := range c.MissionInventory {
		if row.Slot == 13 {
			t.Fatal("the murderer drop roll did not run")
		}
	}
	var private, public []simulation.Frame
	for _, routed := range frames {
		if routed.OnlyCharacterID == c.ID {
			private = append(private, routed.Frames...)
		} else {
			public = append(public, routed.Frames...)
		}
	}
	if !hasSimOpcode(private, wire.OpDeathNotice) || !hasSimOpcode(private, wire.OpItemMoveResponse) ||
		!hasSimOpcode(private, wire.OpPKPenalty) || !hasSimOpcode(private, wire.OpPKDaily) {
		t.Fatalf("victim frames %+v lack the death notice, drop result or PK updates", private)
	}
	if !hasSimOpcode(public, wire.OpSingleObjectSpawn) {
		t.Fatalf("observer frames %+v lack the drop spawn", public)
	}
	zeroHP := 0
	for _, frame := range public {
		if value, isVitals := vitalsHPRefresh(frame); isVitals && value == 0 {
			zeroHP++
		}
	}
	if zeroHP != 1 {
		t.Fatalf("observers saw %d zero-HP vitals frames, want the death baseline's one", zeroHP)
	}

	// A corpse is not drained, even though it is still a murderer.
	if frames := rt.advanceNaturalRecovery(clock.NowMs() + 2*drainTestFirstPulseMs); len(frames) != 0 || enterworld.CurrentHP(c) != 0 {
		t.Fatalf("a corpse was drained: HP %d, frames %+v", enterworld.CurrentHP(c), frames)
	}
}
