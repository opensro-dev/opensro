package simulation

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

func TestOrdinaryAcquisitionRankingAndOrder(t *testing.T) {
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
	player := func(gid uint32, dx, dy, dz float64) playerPose {
		return playerPose{Gid: gid, Pose: Spawn{RegionID: from.RegionID, X: from.X + dx, Y: from.Y + dy, Z: from.Z + dz}}
	}
	cases := []struct {
		name    string
		sight   float64
		players []playerPose
		want    uint32
	}{
		{"height", 115, []playerPose{player(1, 5, 200, 0), player(2, 50, 0, 0)}, 2},
		{"integer accumulator", 20, []playerPose{player(1, 10.5, 0, 0), player(2, 10.25, 0, 0)}, 1},
		{"integer tie", 20, []playerPose{player(1, 10.5, 0, 0), player(2, 10, 0, 0)}, 1},
		{"closer integer band", 20, []playerPose{player(1, 10.5, 0, 0), player(2, 9.5, 0, 0)}, 2},
		{"zero accumulator", 20, []playerPose{player(1, 0.5, 0, 0), player(2, 9.5, 0, 0)}, 2},
		{"GID tree order", 20, []playerPose{player(2, 10, 0, 0), player(1, 10, 0, 0)}, 1},
		{"unsigned GID order", 20, []playerPose{player(0xffff0000, 10, 0, 0), player(1, 10, 0, 0)}, 1},
		{"truncated sight", 10.9, []playerPose{player(1, 10.5, 0, 0)}, 0},
		{"inclusive sight", 10, []playerPose{player(1, 10, 0, 0)}, 1},
		{"zero sight", 0, []playerPose{player(1, 0, 0, 0)}, 1},
		{"outside sampled blocks", 800, []playerPose{player(1, 700, 0, 0)}, 0},
		{"whole sampled block", 800, []playerPose{player(1, 400, 0, 0)}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := append([]playerPose(nil), c.players...)
			got, ok := nearestEligiblePlayer(monster.Instance{}, from, c.players, c.sight)
			if got.Gid != c.want || ok != (c.want != 0) {
				t.Fatalf("target=%d found=%v want=%d", got.Gid, ok, c.want)
			}
			if !reflect.DeepEqual(before, c.players) {
				t.Fatal("changed shared candidate order")
			}
			for left, right := 0, len(c.players)-1; left < right; left, right = left+1, right-1 {
				c.players[left], c.players[right] = c.players[right], c.players[left]
			}
			got, _ = nearestEligiblePlayer(monster.Instance{}, from, c.players, c.sight)
			if got.Gid != c.want {
				t.Fatal("session enumeration order changed result")
			}
		})
	}
}

/*
================
TestAcquisitionOrdinarySelectorScope

The flag rows leave selector 2: flag 4 runs selector 3 and flag 0x80
selector 4, both measuring 53D7A0's 3D distance, so a player 200 above
the monster is out of sight; selector 4 takes thieves only. Flag 0x100
(selector 5) keeps the planar projection.
================
*/
func TestAcquisitionOrdinarySelectorScope(t *testing.T) {
	for _, tc := range []struct {
		flags, want uint32
		thief       bool
	}{{4, 2, false}, {0x80, 0, false}, {0x80, 2, true}, {0x100, 1, false}} {
		actor := monster.Instance{}
		actor.Nest.NativeTacticsFlags = tc.flags
		from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
		players := []playerPose{
			{Gid: 1, Pose: Spawn{RegionID: monsterTestRegion, X: 1005, Y: 200, Z: 1000}},
			{Gid: 2, Pose: Spawn{RegionID: monsterTestRegion, X: 1050, Z: 1000}},
		}
		if tc.thief {
			players[0].JobState, players[1].JobState = jobStateThief, jobStateThief
		}
		got, _ := nearestEligiblePlayer(actor, from, players, 115)
		if got.Gid != tc.want {
			t.Fatalf("flags %x took %d, want %d", tc.flags, got.Gid, tc.want)
		}
	}
}

/*
================
TestJobQueryPrefersJobModeThenNearest

Selector 3 holds a trader or hunter over a nearer jobless player, skips a
thief, and among equals keeps the strictly nearer; a companion stands for
its owner's job.
================
*/
func TestJobQueryPrefersJobModeThenNearest(t *testing.T) {
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
	at := func(gid uint32, x float64, job uint8) playerPose {
		return playerPose{Gid: gid, Pose: Spawn{RegionID: monsterTestRegion, X: x, Z: 1000}, JobState: job}
	}
	players := []playerPose{at(1, 1010, 0), at(2, 1005, jobStateThief), at(3, 1060, jobStateTrader), at(4, 1040, jobStateHunter)}
	got, ok := jobQueryAcquisition(monster.Instance{}, from, players, 100, jobQuery{})
	if !ok || got.Gid != 4 {
		t.Fatalf("selector 3 took %d", got.Gid)
	}
	transport := at(5, 1020, 0)
	transport.OwnerGid, transport.Band = 3, 2
	got, _ = jobQueryAcquisition(monster.Instance{}, from, append(players, transport), 100, jobQuery{})
	if got.Gid != 5 {
		t.Fatalf("the trader's nearer transport was not taken: %d", got.Gid)
	}
}

func TestAcquisitionBlockOrderPrecedesGID(t *testing.T) {
	from := monster.Pose{RegionID: monsterTestRegion, X: 320, Z: 320}
	for _, poses := range [][2]Spawn{
		{{RegionID: monsterTestRegion, X: 319, Z: 320}, {RegionID: monsterTestRegion, X: 321, Z: 320}},
		{{RegionID: monsterTestRegion, X: 321, Z: 319}, {RegionID: monsterTestRegion, X: 319, Z: 321}},
	} {
		players := []playerPose{{Gid: 1, Pose: poses[1]}, {Gid: 9, Pose: poses[0]}}
		got, ok := nearestEligiblePlayer(monster.Instance{}, from, players, 10)
		if !ok || got.Gid != 9 {
			t.Fatalf("block row/column order lost: %+v", got)
		}
	}
}

func TestAcquisitionDrivesOrdinaryWanderAndSummonChase(t *testing.T) {
	for _, mode := range []string{"idle", "wander", "summon"} {
		t.Run(mode, func(t *testing.T) {
			ops, instance := monsterLegFixture(t, aggressiveTactics())
			mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			if mode == "wander" {
				mustMoverTransition(&mover, monster.MoverEventStartWander, 0)
				mover.From, mover.To = mover.Pose, mover.Pose
				mover.To.Z += 100
				mover.DepartMs, mover.ArriveMs = 100000, 110000
				ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
			}
			if mode == "summon" {
				instance.SummonerGID, instance.SummonSightRange = 999, 115
				ops.Monsters.division(monsterTestDivision).instances.set(instance.Gid, instance)
			}
			players := []playerPose{
				{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1005, Y: 220, Z: 1000}, BodyRadius: 4},
				{Gid: PlayerObjectID(2), Pose: Spawn{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000}, BodyRadius: 4},
			}
			frames, _ := ops.advanceInstance(monsterTestDivision, instance, players, 100000)
			got, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			if got.TargetGID() != players[1].Gid || !got.InFlight(100000) {
				t.Fatalf("wrong production chase: %+v", got)
			}
			goal := false
			for _, frame := range frames {
				if frame.Opcode == OpMovementAck {
					gid, _, x, y, _, _ := decodeGoalPayload(t, frame.Payload)
					if gid != instance.Gid || x <= 1005 || y != 20 {
						t.Fatalf("wrong chase goal: gid=%d x=%d y=%d", gid, x, y)
					}
					goal = true
				}
			}
			if !goal {
				t.Fatal("acquisition produced no movement packet")
			}
		})
	}
}

func TestProductionRankingAgainstFrozenNativeSlice(t *testing.T) {
	data, err := os.ReadFile("testdata/acquisition-ranking-native-v1188.json")
	if err != nil {
		t.Fatal(err)
	}
	type rank struct{ Target, Distance uint32 }
	var corpus struct {
		Cases []struct {
			Name     string
			Before   rank
			Target   uint32
			Distance float32
			Sight    uint32
			Expected rank
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 1320 {
		t.Fatal("native corpus incomplete")
	}
	for _, c := range corpus.Cases {
		got := c.Before
		if acquisitionRankAccepts(got.Target, got.Distance, c.Distance, c.Sight) {
			got = rank{c.Target, uint32(c.Distance)}
		}
		if got != c.Expected {
			t.Fatalf("%s: got %+v native %+v", c.Name, got, c.Expected)
		}
	}
}
