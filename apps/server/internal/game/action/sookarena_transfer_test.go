/*
===========================================================================
sookarena_transfer_test.go - bounded secondary damage, timers and stage cleanup.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
	"time"
)

/*
================
TestSookTransferLimitsExpiryAndIsolation
================
*/
func TestSookTransferLimitsExpiryAndIsolation(t *testing.T) {
	a, b := instanceWorldTestCharacter(11, "ChainOwner"), instanceWorldTestCharacter(12, "ChainNeighbour")
	rt, clock := sookFixture(t, a, b)
	rt.enterSookArena(testDivision, a)
	rt.enterSookArena(testDivision, b)
	run, other := rt.sookRun(testDivision, a), rt.sookRun(testDivision, b)
	now := clock.Now().UnixMilli()
	spawn := func(r *sookArenaRun, distance float64) uint32 {
		at := r.center
		at.X += distance
		ids := rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{Division: testDivision, Population: r.lease, RefObjID: 100, Count: 1, Position: at, NowMs: now, Arena: true})
		if len(ids) != 1 {
			t.Fatal("spawn")
		}
		return ids[0]
	}
	primary := spawn(run, 0)
	var nearby []uint32
	for i := 0; i < 12; i++ {
		nearby = append(nearby, spawn(run, float64(i+1)))
	}
	outside, foreign := spawn(run, 101), spawn(other, 0)
	source, _ := rt.Monsters.Get(testDivision, primary)
	impacts := []simulation.MonsterDamageResult{{Population: run.lease, Instance: source, Damage: 99999, Applied: 123}}
	in := sookTransferHit{division: testDivision, character: a, at: run.center, impacts: impacts, now: now}
	rt.transferSookDamage(in)
	for _, id := range nearby {
		m, _ := rt.Monsters.Get(testDivision, id)
		if m.CurrentHP != 100000 {
			t.Fatal("inactive transfer")
		}
	}
	item := rt.addCharacterGround(testDivision, a, grounditem.Item{ArenaPowerup: sookTransfer, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
	rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, a.Name), a, item)
	if run.program.TransferUntil != now+30000 {
		t.Fatal("pickup duration")
	}
	rt.transferSookDamage(in)
	hit := 0
	for _, id := range nearby {
		m, _ := rt.Monsters.Get(testDivision, id)
		if m.CurrentHP == 100000-123 {
			hit++
		} else if m.CurrentHP != 100000 {
			t.Fatalf("recursive/overkill damage: %d", m.CurrentHP)
		}
	}
	if hit != 10 {
		t.Fatalf("secondary count %d", hit)
	}
	for _, id := range []uint32{primary, outside, foreign} {
		m, _ := rt.Monsters.Get(testDivision, id)
		if m.CurrentHP != 100000 {
			t.Fatal("source, range or population leak")
		}
	}
	in.now = now + 30000
	rt.transferSookDamage(in)
	for _, id := range nearby {
		m, _ := rt.Monsters.Get(testDivision, id)
		if m.CurrentHP < 100000-123 {
			t.Fatal("expired transfer")
		}
	}
	// A foreign primary cannot inject copied damage into this owner's lease.
	run.program.TransferUntil = now + 60000
	in.impacts[0].Population = other.lease
	rt.transferSookDamage(in)
	for _, id := range nearby {
		m, _ := rt.Monsters.Get(testDivision, id)
		if m.CurrentHP < 100000-123 {
			t.Fatal("foreign primary accepted")
		}
	}
}

/*
================
TestSookCheckpointClearsEggsSpawnedWhileClearing
================
*/
func TestSookCheckpointClearsEggsSpawnedWhileClearing(t *testing.T) {
	c := instanceWorldTestCharacter(11, "OldEggs")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	item := rt.addCharacterGround(testDivision, c, grounditem.Item{ArenaPowerup: sookMana, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
	run.props = map[uint32]int64{item.Gid: clock.Now().UnixMilli() + 40000}
	now := clock.Now().UnixMilli()
	run.program.StageStarted = now - 300000
	run.program.ClearingAt = now - 1000
	run.powerupsOwed = 1
	rt.advanceSookArena(now)
	if c.SookChest == nil || !c.SookChest.Checkpoint || run.program.PausedAt == 0 {
		t.Fatal("checkpoint")
	}
	if len(run.props) != 0 || run.powerupsOwed != 0 {
		t.Fatal("checkpoint retained ground props or deferred eggs")
	}
	if _, ok := rt.characterGround(testDivision, c, item.Gid); ok {
		t.Fatal("egg not removed at stage END")
	}
	rt.HandleArenaDecision(testDivision, c, append([]byte{2}, []byte(c.SookChest.ID)...))
	clock.now = clock.now.Add(15 * time.Second)
	rt.advanceSookArena(clock.Now().UnixMilli())
	if _, ok := rt.characterGround(testDivision, c, item.Gid); ok {
		t.Fatal("old egg survived stage change")
	}
	if _, ok := run.props[item.Gid]; ok {
		t.Fatal("old egg index survived")
	}
}

/*
================
TestSookUnkillableThirtySecondBoundary
================
*/
func TestSookUnkillableThirtySecondBoundary(t *testing.T) {
	c := instanceWorldTestCharacter(11, "ThirtySeconds")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	item := rt.addCharacterGround(testDivision, c, grounditem.Item{ArenaPowerup: sookInvincible, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
	rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, c.Name), c, item)
	now := clock.Now().UnixMilli()
	if !rt.sookPowerActive(testDivision, c, sookInvincible, now+29999) || rt.sookPowerActive(testDivision, c, sookInvincible, now+30000) || sookPowerDuration(sookInvincible) != 30 {
		t.Fatal("Unkillable timer mismatch")
	}
}

/*
================
TestSookFleetfootSpeedAndExpiry
================
*/
func TestSookFleetfootSpeedAndExpiry(t *testing.T) {
	a, b := instanceWorldTestCharacter(11, "Fast"), instanceWorldTestCharacter(12, "Normal")
	rt, clock := sookFixture(t, a, b)
	rt.enterSookArena(testDivision, a)
	now := clock.Now().UnixMilli()
	item := rt.addCharacterGround(testDivision, a, grounditem.Item{ArenaPowerup: sookSpeed, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
	out := rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, a.Name), a, item)
	found := false
	for _, f := range out.Frames {
		found = found || f.Opcode == movementSpeedOpcode
	}
	walk, run := rt.EntryMovementSpeeds(testDivision, a.Name)
	if !found || walk != simulation.WalkSpeed*3 || run != simulation.RunSpeed*3 {
		t.Fatalf("boost %v %v %v", found, walk, run)
	}
	rt.refreshMovementEffects(testDivision, b, now)
	_, other := rt.EntryMovementSpeeds(testDivision, b.Name)
	if other != simulation.RunSpeed {
		t.Fatal("speed leaked")
	}
	rt.refreshMovementEffects(testDivision, a, now+29999)
	_, run = rt.EntryMovementSpeeds(testDivision, a.Name)
	if run != simulation.RunSpeed*3 {
		t.Fatal("early expiry")
	}
	rt.refreshMovementEffects(testDivision, a, now+30000)
	_, run = rt.EntryMovementSpeeds(testDivision, a.Name)
	if run != simulation.RunSpeed {
		t.Fatal("speed not restored")
	}
}

/*
================
TestSookRemainingMonsterNotices
================
*/
func TestSookRemainingMonsterNotices(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Countdown")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	now := clock.Now().UnixMilli()
	ids := rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{Division: testDivision, Population: run.lease, RefObjID: 100, Count: 11, Position: run.center, NowMs: now, Arena: true})
	if len(ids) != 11 {
		t.Fatal("spawn")
	}
	run.program.StageStarted = now - 300000
	run.program.Started = now - 320000
	var delivered []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { delivered = append(delivered, frames...) }
	rt.advanceSookArena(now)
	for remaining := 10; remaining >= 0; remaining-- {
		target, _ := rt.Monsters.Get(testDivision, ids[remaining])
		rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP, []simulation.MonsterDamagePlan{{GID: target.Gid, ExpectedHP: target.CurrentHP, Damage: target.CurrentHP}})
		now += 100
		rt.advanceSookArena(now)
		if run.lastRemaining != remaining {
			t.Fatalf("remaining %d, got %d", remaining, run.lastRemaining)
		}
	}
	notices := 0
	for _, f := range delivered {
		if f.Opcode == sookBanner("").Opcode {
			notices++
		}
	}
	if notices != 12 {
		t.Fatalf("expected stage-end plus eleven notices, got %d", notices)
	}
}

/*
================
TestSookMissingGroundRowStillDespawnsEgg
================
*/
func TestSookMissingGroundRowStillDespawnsEgg(t *testing.T) {
	c := instanceWorldTestCharacter(11, "StaleEgg")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	item := rt.addCharacterGround(testDivision, c, grounditem.Item{ArenaPowerup: sookMana, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
	run.props = map[uint32]int64{item.Gid: clock.Now().UnixMilli()}
	rt.Ground.Remove(testDivision, item.Gid)
	rt.drainMonsterLegRecipients()
	rt.expireSookProps(run, clock.Now().UnixMilli())
	found := false
	for _, batch := range rt.drainMonsterLegRecipients() {
		if batch.OnlyCharacterID != c.ID {
			continue
		}
		for _, f := range batch.Frames {
			if f.Opcode == wire.OpObjectDespawn && string(f.Payload) == string(groundDespawn(item.Gid).Payload) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("stale client egg was not despawned")
	}
}

/*
================
TestSookBoostedEggShares
================
*/
func TestSookBoostedEggShares(t *testing.T) {
	counts := map[uint8]int{}
	for roll := uint32(0); roll < 32768; roll++ {
		counts[sookPowerupKind(roll)]++
	}
	if len(counts) != 11 {
		t.Fatalf("egg kinds=%d", len(counts))
	}
	for kind, count := range counts {
		weight := 280
		if kind == sookDamage || kind == sookTransfer {
			weight = 390
		} else if kind == sookBomb {
			weight = 364
		} else if kind == sookAmbush {
			weight = 196
		}
		delta := count*3300 - weight*32768
		if delta < -3300 || delta > 3300 {
			t.Fatalf("kind %d count %d exceeds one-sample rounding", kind, count)
		}
	}
}
