/*
===========================================================================

flying_stone_test.go - the flying stone chain and its bleeding lifecycle

===========================================================================
*/

package action

import (
	"encoding/binary"
	"fmt"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
================
TestFlyingStoneShippedChainAndBleedingLifecycle
================
*/
func TestFlyingStoneShippedChainAndBleedingLifecycle(t *testing.T) {
	dir := os.Getenv("SRO_SKILL_INVENTORY_DATA")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "..", "..", ".generated", "game-data", "1.150", "server", "textdata")
	}
	if _, err := os.Stat(filepath.Join(dir, "skilldata.txt")); err != nil {
		t.Skip("v1.150 textdata unavailable")
	}
	source := enterworld.NewTextdataSkills(dir)
	// Every authored rank must pass the production compiler, including its tail.
	for level := 1; level <= 10; level++ {
		row, ok := source.SkillByCodename(fmt.Sprintf("SKILL_CH_SWORD_SMASH_D_%02d", level))
		if !ok {
			t.Fatal("missing rank", level)
		}
		stages, ok := enterworld.OffensiveSequence(source, row.ID)
		if !ok || len(stages) != 2 {
			t.Fatalf("rank %d not admitted: %s", level, row.OffenseRefusal)
		}
		for _, s := range stages {
			if bleeding, ok := s.Abnormal.Param(abnormal.Bleeding); !ok || bleeding.Args[0] != 30000 {
				t.Fatalf("lost descriptor %+v", s.Abnormal)
			}
		}
	}
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	root, _ := source.SkillByID(18628)
	tail, _ := source.SkillByID(18640)
	skills := rt.deps.SkillData().(staticSkillSource)
	skills[root.ID] = root
	skills[tail.ID] = tail
	c.Skills = append(c.Skills, root.ID)
	c.Intellect = testInt64(200)
	c.CurrentMP = testInt64(1000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	at := clock.NowMs()
	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
		t.Fatalf("refused %+v", start)
	}
	if enterworld.CurrentMP(c) != 1000 {
		t.Fatal("charged during preparation")
	}
	clock.now = time.UnixMilli(at + int64(root.ActionCastingTimeMs) + 1)
	released := releasePreparedSkillForTest(t, rt, clock.NowMs())
	first, _ := rt.Monsters.Get(testDivision, target.Gid)
	firstBleed := bleedingSlot(first)
	if first.CurrentHP >= target.CurrentHP || firstBleed.Grade != 8 || firstBleed.Param38 != 156 || firstBleed.Param40 != 20 {
		t.Fatalf("first strike %+v", firstBleed)
	}
	if enterworld.CurrentMP(c) != 450 {
		t.Fatal("root MP", enterworld.CurrentMP(c))
	}
	maskFound := false
	for _, f := range released.Frames {
		if f.Opcode == simulation.OpVitalsUpdate && len(f.Payload) == 16 && f.Payload[6] == 5 && binary.LittleEndian.Uint32(f.Payload[11:])&0x800 != 0 && f.Payload[15] == 8 {
			maskFound = true
		}
	}
	if !maskFound {
		t.Fatal("missing bleeding grade on HP/status snapshot")
	}
	tailSeen := false
	for n := 1; n <= 15; n++ {
		clock.now = time.UnixMilli(at + int64(root.ActionCastingTimeMs) + 1 + int64(n)*100)
		for _, batch := range rt.TickHook()(clock.NowMs()) {
			for _, f := range batch.Frames {
				if f.Opcode == wire.OpSkillCastResult && len(f.Payload) >= 6 && f.Payload[0] == 1 && binary.LittleEndian.Uint32(f.Payload[2:]) == tail.ID {
					tailSeen = true
				}
			}
		}
		if tailSeen {
			break
		}
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	// An equal-grade re-roll never refreshes the slot (4A4270).
	if !tailSeen || after.CurrentHP >= first.CurrentHP || enterworld.CurrentMP(c) != 450 || bleedingSlot(after).StartedAt != firstBleed.StartedAt {
		t.Fatalf("tail/cost/replacement: seen=%v mp=%d bleed=%+v", tailSeen, enterworld.CurrentMP(c), bleedingSlot(after))
	}
	if !rt.criticals.actors[criticalActor{division: testDivision, character: strings.ToLower(c.Name)}][0x0c000000].Initialized {
		t.Fatal("bleeding probability key was not shared across stages")
	}
	if c.OffensiveSkillCooldowns[root.Group] != at+3000 {
		t.Fatal("tail reset cooldown")
	}
	rt.ClearCombatIntent(testDivision, c.Name)
	// A due fixed-damage tick does not depend on magical parry or repeat damage.
	now := bleedingSlot(after).LastTickAt + 2001
	if now < clock.NowMs() {
		now = clock.NowMs() + 2001
	}
	rt.advanceMonsterAbnormals(now)
	ticked, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP-ticked.CurrentHP != 156 {
		t.Fatal("tick damage", after.CurrentHP, ticked.CurrentHP)
	}
	rt.advanceMonsterAbnormals(now)
	repeated, _ := rt.Monsters.Get(testDivision, target.Gid)
	if repeated.CurrentHP != ticked.CurrentHP {
		t.Fatal("duplicate tick")
	}
	rt.advanceMonsterAbnormals(firstBleed.StartedAt + 30001)
	expired, _ := rt.Monsters.Get(testDivision, target.Gid)
	if expired.Abnormal != nil {
		t.Fatal("expiry retained modifier")
	}
}

/*
================
bleedingSlot
================
*/
func bleedingSlot(instance monster.Instance) abnormal.Slot {
	if instance.Abnormal == nil {
		return abnormal.Slot{}
	}
	return instance.Abnormal.Slots[abnormal.Bleeding]
}

// seedDepartedAbnormal installs a status from c through the impact door and
// then detaches c as a disconnect does: ticks continue uncredited.
/*
================
seedDepartedAbnormal
================
*/
func seedDepartedAbnormal(t *testing.T, rt *Runtime, c *enterworld.Character, gid uint32, record abnormal.Record) {
	t.Helper()
	record.SourceGID, record.SourceName = enterworld.ObjectIDForCharacter(c), c.Name
	current, _ := rt.Monsters.Get(testDivision, gid)
	if r := rt.Monsters.ApplyDamageSequence(testDivision, gid, current.CurrentHP, []simulation.MonsterDamagePlan{{GID: gid, CreditGID: record.SourceGID, Abnormal: []abnormal.Record{record}, AbnormalSources: rt.Monsters.PrepareAbnormalSources(testDivision, []abnormal.Record{record})}}); len(r) != 1 || r[0].Instance.Abnormal == nil {
		t.Fatal("status admission")
	}
	rt.Monsters.ForgetAbnormalSource(testDivision, 0, c.Name)
}

/*
================
actorOf
================
*/
func actorOf(t *testing.T, rt *Runtime) *enterworld.Character {
	t.Helper()
	for _, c := range rt.deps.CharactersForDivision(testDivision) {
		if c != nil {
			return c
		}
	}
	t.Fatal("no character fixture")
	return nil
}
