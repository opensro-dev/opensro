/*
===========================================================================

wall_test.go - the Chinese Force walls against monster hits

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

const (
	crystalWallA1 = 99  // SKILL_CH_COLD_BINGBYEOK_A_01: onff 5000 41, pw 7 824 0 11
	fireWallA1    = 136 // SKILL_CH_FIRE_HWABYEOK_A_01: onff 5000 58, pw 11 1159 0 17
)

// wallFixture raises the wall and arms the monster's basic attack with the
// given lane flags.
func wallFixture(t *testing.T, id uint32, flags uint32) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	rt, clock, c, mob := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0
	learnWall(t, rt, c, id)
	mob.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	attack := skills[2]
	attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent, attack.Attack.Flags = 60, 60, 100, flags
	skills[2] = attack
	if r := castSelf(rt, c, id); r.DiagnosticRefusal != "" || !hasSkillEffect(rt, c.Name, id) {
		t.Fatalf("wall cast: %+v", r)
	}
	if _, ok := rt.standingWallOf(testDivision, c.Name); !ok {
		t.Fatal("no wall in the slot")
	}
	clock.Advance(2 * time.Second)
	rt.drainSkillFinalizes(clock.NowMs())
	return rt, clock, c, mob
}

// learnWall learns the shipped wall at a cost the level-1 keeper can pay.
func learnWall(t *testing.T, rt *Runtime, c *enterworld.Character, id uint32) {
	t.Helper()
	row := learnShipped(t, rt, c, id)
	row.Consumption.MP = 50
	rt.deps.SkillData().(staticSkillSource)[id] = row
	c.CurrentMP = testInt64(150)
}

// wallHit is the monster's B245 against the player: its impact count, the
// player's records and the wall entry's records.
type wallRecord struct {
	tag             byte
	damage          uint32
	remaining, pool uint16
}

func wallHit(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character, mob monster.Instance) (player []uint32, wall []wallRecord) {
	t.Helper()
	result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !result.Accepted {
		t.Fatalf("monster hit refused: %+v", result)
	}
	p := result.Frames[0].Payload
	gid := enterworld.ObjectIDForCharacter(c)
	impacts := int(p[19])
	if p[20] != 2 || binary.LittleEndian.Uint32(p[21:]) != gid {
		t.Fatalf("result shape % X", p)
	}
	at := 25
	for range impacts {
		if p[at]&0x7f != 0 {
			t.Fatalf("player record % X", p)
		}
		player = append(player, binary.LittleEndian.Uint32(p[at+1:])>>8)
		at += 9
	}
	if binary.LittleEndian.Uint32(p[at:]) != gid {
		t.Fatalf("wall entry gid % X", p)
	}
	at += 4
	for range impacts {
		r := wallRecord{tag: p[at]}
		at++
		if r.tag&0x7f == 7 {
			r.damage = binary.LittleEndian.Uint32(p[at:]) >> 8
			r.remaining, r.pool = binary.LittleEndian.Uint16(p[at+4:]), binary.LittleEndian.Uint16(p[at+6:])
			at += 8
		}
		wall = append(wall, r)
	}
	if at != len(p) {
		t.Fatalf("trailing bytes % X", p)
	}
	return player, wall
}

/*
==================
TestCrystalWallAbsorbsThePhysicalHit

pw 7 covers the physical lane: the player's record carries no damage and a
second target entry for the same gid carries the type-7 record - absorbed
damage, the pool it leaves and the authored 824 - and the pool drains.
==================
*/
func TestCrystalWallAbsorbsThePhysicalHit(t *testing.T) {
	rt, clock, c, mob := wallFixture(t, crystalWallA1, 5)
	hp := enterworld.CurrentHP(c)
	player, wall := wallHit(t, rt, clock, c, mob)
	for _, damage := range player {
		if damage != 0 {
			t.Fatalf("the player took %v", player)
		}
	}
	if enterworld.CurrentHP(c) != hp {
		t.Fatalf("HP %d -> %d", hp, enterworld.CurrentHP(c))
	}
	// Each impact adds to the group total; its record reports the pool that
	// total leaves.
	total := uint32(0)
	for _, r := range wall {
		total += r.damage
		if r.tag != 7 || r.damage == 0 || r.pool != 824 || uint32(r.remaining) != 824-total {
			t.Fatalf("absorb records %+v", wall)
		}
	}
	if w, _ := rt.standingWallOf(testDivision, c.Name); w.pool != 824-total {
		t.Fatalf("pool %d, want %d", w.pool, 824-total)
	}
}

/*
==================
TestFireWallLetsThePhysicalHitThrough

pw 11 covers only the magical lane: the physical hit lands on the player,
the absorb entry is a bare type 8 and the pool stays full.
==================
*/
func TestFireWallLetsThePhysicalHitThrough(t *testing.T) {
	rt, clock, c, mob := wallFixture(t, fireWallA1, 5)
	// Survive the hit: death retires every unprotected effect, the wall too.
	c.Strength = testInt64(500)
	c.CurrentHP = nil
	hp := enterworld.CurrentHP(c)
	_, wall := wallHit(t, rt, clock, c, mob)
	if !enterworld.CharacterAlive(c) {
		t.Fatal("fixture player died; the wall would retire with it")
	}
	for _, r := range wall {
		if r.tag != 8 {
			t.Fatalf("absorb records %+v", wall)
		}
	}
	if enterworld.CurrentHP(c) >= hp {
		t.Fatalf("physical through a fire wall: HP %d -> %d", hp, enterworld.CurrentHP(c))
	}
	if w, _ := rt.standingWallOf(testDivision, c.Name); w.pool != 1159 {
		t.Fatalf("pool %d, want untouched 1159", w.pool)
	}
}

/*
==================
TestWallBreaksRetiresAndBlocksASecondWall

A second ao / pw cast is refused while the wall stands (motion 0x11). The
hit that empties the pool is flagged (0x87, remaining 0); the next update
retires the wall and its instance. The onff pulse charges 41 MP per 5 s.
==================
*/
func TestWallBreaksRetiresAndBlocksASecondWall(t *testing.T) {
	rt, clock, c, mob := wallFixture(t, crystalWallA1, 5)
	learnWall(t, rt, c, fireWallA1)
	if r := castSelf(rt, c, fireWallA1); hasSkillEffect(rt, c.Name, fireWallA1) || r.DiagnosticRefusal == "" && (len(r.Frames) == 0 || r.Frames[0].Payload[0] != 2) {
		t.Fatalf("second wall while one stands: %+v", r)
	}

	c.CurrentMP = testInt64(150)
	mp := enterworld.CurrentMP(c)
	clock.Advance(5 * time.Second)
	rt.TickHook()(clock.NowMs())
	if got := enterworld.CurrentMP(c); got != mp-41 {
		t.Fatalf("MP %d after one pulse, want %d", got, mp-41)
	}

	rt.wallMu.Lock()
	rt.walls[wallKey(testDivision, c.Name)].pool = 1
	rt.wallMu.Unlock()
	_, wall := wallHit(t, rt, clock, c, mob)
	if wall[0].tag != 0x87 || wall[0].remaining != 0 {
		t.Fatalf("breaking record %+v", wall)
	}
	rt.TickHook()(clock.NowMs() + 1)
	if _, ok := rt.standingWallOf(testDivision, c.Name); ok || hasSkillEffect(rt, c.Name, crystalWallA1) {
		t.Fatal("the empty wall still stands")
	}
	hp := enterworld.CurrentHP(c)
	clock.Advance(3 * time.Second)
	if r := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs()); !r.Accepted || r.Frames[0].Payload[20] != 1 {
		t.Fatalf("hit after the wall broke: %+v", r)
	}
	if enterworld.CurrentHP(c) >= hp {
		t.Fatal("hits still absorbed after the wall broke")
	}
}
