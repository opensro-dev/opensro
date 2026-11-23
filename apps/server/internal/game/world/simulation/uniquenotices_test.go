package simulation

import (
	"bytes"
	"opensro.online/server/internal/game/world/monster"
	"testing"
	"time"
)

func TestUniqueNoticesBroadcastBeyondVisibility(t *testing.T) {
	s := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: {TidWord: 0x00C6, RefObjID: 1, MaxHP: 100, MonsterType: 3}}, []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 0x62aa}, MaxCount: 1}}))
	s.StartDivision("a")
	s.AdvancePopulation(s.CurrentTimeMillis())
	s.InstancesInRegions("a", []uint16{0x62aa})
	ops := &MonsterMoverOps{Monsters: s}
	push := &fakePusher{}
	session := SessionSnapshot{SessionID: "distant", DivisionID: "a", CharacterID: 1, World: WorldState{Spawn: Spawn{RegionID: 0x7090}, SpawnSet: true}}
	ops.RunMonsterLeg(time.Now().UnixMilli(), []SessionSnapshot{session}, push)
	if len(push.toDivision) != 1 || push.toDivision[0].divisionID != "a" || push.toDivision[0].except != "" || push.toDivision[0].frames[0].Opcode != 0x3058 {
		t.Fatalf("not shard-wide: %+v", push.toDivision)
	}
	ops.RunMonsterLeg(time.Now().UnixMilli(), []SessionSnapshot{session}, push)
	if len(push.toDivision) != 1 {
		t.Fatal("tick replayed event")
	}
}

func TestUniqueNoticesFollowLifecycleNotVisibility(t *testing.T) {
	for _, rarity := range []uint8{0, 1, 3, 4, 8} {
		unique := rarity == 3 || rarity == 8
		t.Run(string(rune('0'+rarity)), func(t *testing.T) {
			s := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: {TidWord: 0x00C6, RefObjID: 1, MaxHP: 100, MonsterType: rarity}}, []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 0x62aa}, MaxCount: 1, Respawn: true, RespawnDelayMinSec: 10, RespawnDelayMaxSec: 10}}))
			now := time.Unix(100, 0)
			s.SetTimeSource(func() time.Time { return now })
			s.StartDivision("a")
			s.AdvancePopulation(s.CurrentTimeMillis())
			first := s.InstancesInRegions("a", []uint16{0x62aa})[0]
			spawn := s.DrainUniqueNotices("a")
			if unique {
				if len(spawn) != 1 || spawn[0].Opcode != 0x3058 || !bytes.Equal(spawn[0].Payload, []byte{5, 1, 0, 0, 0}) {
					t.Fatalf("spawn %+v", spawn)
				}
			} else if len(spawn) != 0 {
				t.Fatal("non-unique announced")
			}
			s.StartDivision("a")
			s.AdvancePopulation(s.CurrentTimeMillis())
			s.InstancesInRegions("a", []uint16{0x62aa})
			if len(s.DrainUniqueNotices("a")) != 0 {
				t.Fatal("visibility replay")
			}
			s.RecordUniqueKiller("a", first.Gid, "alive")
			if len(s.DrainUniqueNotices("a")) != 0 {
				t.Fatal("living kill announced")
			}
			hit, ok := s.ApplyDamage("a", first.Gid, first.CurrentHP)
			if !ok || !hit.Fatal {
				t.Fatal("fixture failed to kill unique")
			}
			s.RecordUniqueKiller("a", first.Gid, "asd2")
			s.RecordUniqueKiller("a", first.Gid, "other")
			s.Defeat("a", first.Gid, now)
			s.Defeat("a", first.Gid, now)
			death := s.DrainUniqueNotices("a")
			if unique {
				if len(death) != 1 || !bytes.Equal(death[0].Payload, []byte{6, 1, 0, 0, 0, 4, 0, 'a', 's', 'd', '2'}) {
					t.Fatalf("death %+v", death)
				}
			} else if len(death) != 0 {
				t.Fatal("non-unique kill announced")
			}
			if len(s.DrainUniqueNotices("b")) != 0 {
				t.Fatal("cross-shard leak")
			}
			now = now.Add(10 * time.Second)
			s.StartDivision("a")
			s.AdvancePopulation(s.CurrentTimeMillis())
			second := s.InstancesInRegions("a", []uint16{0x62aa})[0]
			if second.Gid == first.Gid {
				t.Fatal("respawn reused identity")
			}
			respawn := s.DrainUniqueNotices("a")
			if (len(respawn) == 1) != unique {
				t.Fatal("respawn notification")
			}
			s.Defeat("a", second.Gid, now)
			gone := s.DrainUniqueNotices("a")
			if unique && (len(gone) != 1 || !bytes.Equal(gone[0].Payload, []byte{6, 1, 0, 0, 0, 3, 0, '?', '?', '?'})) {
				t.Fatalf("disappearance %+v", gone)
			}
		})
	}
}
