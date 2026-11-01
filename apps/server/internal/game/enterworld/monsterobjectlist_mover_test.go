package enterworld

import (
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

func TestMonsterObjectListRowsSnapshotsAuthoritativeLivePose(t *testing.T) {
	const (
		divisionID = "DIV_A"
		regionID   = uint16(25000)
		t0         = int64(1_784_000_000_000)
	)
	registry := simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG", WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100},
		},
		[]monster.NestRow{{SpawnPoint: monster.SpawnPoint{
			RefObjID: 1933, RegionID: regionID, X: 100, Y: 20, Z: 100,
		}}},
	))
	registry.SetTimeSource(func() time.Time { return time.UnixMilli(t0) })
	registry.StartDivision(divisionID)
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions(divisionID, []uint16{regionID})
	if len(instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(instances))
	}
	instance := instances[0]
	mover, ok := registry.Mover(divisionID, instance.Gid)
	if !ok {
		t.Fatal("materialized monster has no mover")
	}
	if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
		t.Fatalf("spawn -> idle: %v", err)
	}
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatalf("idle -> wander: %v", err)
	}
	mover.BehaviorDeadlineMs = 0
	mover.From = mover.Pose
	mover.To = monster.Pose{RegionID: regionID, X: 200, Y: 30, Z: 240, Heading: 0x3456}
	mover.DepartMs = t0
	mover.ArriveMs = t0 + 10_000
	if !registry.CommitMover(divisionID, instance.Gid, mover) {
		t.Fatal("commit in-flight mover")
	}

	rows := MonsterObjectListRows(registry, divisionID, instances, t0+5_000, nil)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0].Payload
	readU32 := func(values []byte, offset int) uint32 {
		return uint32(values[offset]) |
			uint32(values[offset+1])<<8 |
			uint32(values[offset+2])<<16 |
			uint32(values[offset+3])<<24
	}
	readF32 := func(offset int) float32 {
		return math.Float32frombits(readU32(row, offset))
	}
	if x, y, z := readF32(10), readF32(14), readF32(18); x != 150 || y != 25 || z != 170 {
		t.Fatalf("bootstrap pose = (%v,%v,%v), want interpolated (150,25,170), not anchor (100,20,100)", x, y, z)
	}
	if heading := uint16(row[22]) | uint16(row[23])<<8; heading != 0x3456 {
		t.Fatalf("bootstrap heading = %#04x, want mover heading 0x3456", heading)
	}

	settled := MonsterObjectListRows(registry, divisionID, instances, t0+10_001, nil)[0].Payload
	if x, z := math.Float32frombits(readU32(settled, 10)), math.Float32frombits(readU32(settled, 18)); x != 200 || z != 240 {
		t.Fatalf("settled bootstrap pose = (%v,%v), want destination (200,240)", x, z)
	}
}
