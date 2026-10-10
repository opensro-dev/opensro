/*
===========================================================================

hostiledebuff_test.go - Vital Spot through the live action owner

The cast deals no damage. A monster keeps the word in a target-effect slot
until expiry; a player keeps a registry instance whose parameter write
lowers the same stat.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
vitalSpotFixture
================
*/
func vitalSpotFixture(t *testing.T, code string) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow, uint32) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, code)
	muscle := shippedOffense(t, "SKILL_CH_WATER_CANCEL_A_01")
	// Spirit needs the Muscle rank (column 40) and its mastery at 50.
	c.Skills = []uint32{muscle.ID, skill.ID}
	c.Masteries = []enterworld.CharacterMastery{{ID: 276, Level: 50}}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	rt.deps.SkillData().(staticSkillSource)[muscle.ID] = muscle
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	return rt, clock, c, skill, target.Gid
}

/*
================
monsterStatWord

The stat Vital Spot lowers: evasion for terd, hit rate for thrd.
================
*/
func monsterStatWord(t *testing.T, rt *Runtime, gid uint32, skill enterworld.SkillRow) float64 {
	t.Helper()
	live, _ := rt.Monsters.Get(testDivision, gid)
	s, err := combat.MonsterInstanceStats(live)
	if err != nil {
		t.Fatal(err)
	}
	if skill.HostileDebuff.Evasion != 0 {
		return float64(s.EvasionRate)
	}
	return float64(s.HitRate)
}

/*
================
TestVitalSpotLowersMonsterStatUntilExpiry

Muscle lowers evasion, Spirit hit rate, by the authored word, with tant
aggression and no damage. The spawn row carries the instance, and expiry
restores the stat and publishes the retirement once.
================
*/
func TestVitalSpotLowersMonsterStatUntilExpiry(t *testing.T) {
	for _, code := range []string{"SKILL_CH_WATER_CANCEL_A_01", "SKILL_CH_WATER_CANCEL_B_01"} {
		t.Run(code, func(t *testing.T) {
			rt, clock, c, skill, gid := vitalSpotFixture(t, code)
			before, _ := rt.Monsters.Get(testDivision, gid)
			stat := monsterStatWord(t, rt, gid, skill)
			mp := enterworld.CurrentMP(c)
			out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: gid}.Encode())
			if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
				t.Fatalf("Vital Spot refused: %+v (%s)", out, out.DiagnosticRefusal)
			}
			attached, ok := findFrame(out.Frames, wire.OpAttachedEffect)
			if !ok {
				t.Fatal("no recipient instance frame", out.Frames)
			}
			after, _ := rt.Monsters.Get(testDivision, gid)
			e := after.TargetEffects[0]
			tag, value := skill.HostileDebuff.Word()
			if e.Token == 0 || e.SkillID != skill.ID || e.Tag != tag || e.First != value || e.UntilMs != clock.NowMs()+int64(skill.EffectDurationMs) {
				t.Fatalf("target effect %+v", after.TargetEffects)
			}
			if binary.LittleEndian.Uint32(attached.Payload[4:]) != skill.ID || binary.LittleEndian.Uint32(attached.Payload[8:]) != e.Token {
				t.Fatalf("recipient frame %x", attached.Payload)
			}
			if after.CurrentHP != before.CurrentHP || after.Opponents[0].Damage != 0 ||
				after.Opponents[0].Aggression < int32(skill.HostileDebuff.ThreatFlat) || after.Opponents[0].GID != enterworld.ObjectIDForCharacter(c) {
				t.Fatalf("outcome HP %d->%d, opponents %+v", before.CurrentHP, after.CurrentHP, after.Opponents)
			}
			if enterworld.CurrentMP(c) != mp-int64(skill.Consumption.MP) {
				t.Fatal("MP debit", enterworld.CurrentMP(c))
			}
			// The parameter's native lower bound is zero: the fixture's 27
			// against a word of 43 or more floors there.
			if got := monsterStatWord(t, rt, gid, skill); got != max(stat-float64(value), 0) || got >= stat {
				t.Fatalf("stat %v -> %v, want -%d", stat, got, value)
			}
			def := simulation.MonsterWireDefFromInstance(after, clock.NowMs())
			row := simulation.BuildMonsterCreateRow(def, gid, simulation.Spawn{RegionID: after.Spawn.RegionID, X: after.Spawn.X, Y: after.Spawn.Y, Z: after.Spawn.Z})
			if row[44] != 1 || binary.LittleEndian.Uint32(row[45:]) != skill.ID || binary.LittleEndian.Uint32(row[49:]) != e.Token {
				t.Fatalf("spawn effect bytes %x", row)
			}
			rt.drainSkillFinalizes(clock.NowMs() + int64(skill.ActionDurationMs))
			if rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatal("cast retained action ownership")
			}
			if got := rt.retireMonsterSelfEffects(e.UntilMs); len(got) != 0 {
				t.Fatal("expired at equality")
			}
			got := rt.retireMonsterSelfEffects(e.UntilMs + 1)
			if len(got) != 1 || got[0].SourceGID != gid {
				t.Fatal("missing expiry", got)
			}
			ended, err := wire.DecodeEndedEffectInstances(got[0].Frames[0].Payload)
			if err != nil || len(ended.InstanceTokens) != 1 || ended.InstanceTokens[0] != e.Token {
				t.Fatal("expiry identity", ended, err)
			}
			if got := monsterStatWord(t, rt, gid, skill); got != stat {
				t.Fatalf("stat %v after expiry, want %v", got, stat)
			}
			if got := rt.retireMonsterSelfEffects(e.UntilMs + 2); len(got) != 0 {
				t.Fatal("duplicate retirement")
			}
		})
	}
}

/*
================
TestMonsterTargetEffectReplacesSameWord

A second instance of the same word takes the first one's slot and returns
its token; a different word takes a free slot; a full block refuses.
================
*/
func TestMonsterTargetEffectReplacesSameWord(t *testing.T) {
	rt, clock, _, _, gid := vitalSpotFixture(t, "SKILL_CH_WATER_CANCEL_A_01")
	now := clock.NowMs()
	effect := func(token, tag uint32) monster.SelfEffect {
		return monster.SelfEffect{SkillID: 1, Token: token, Tag: tag, First: 5, StartedAtMs: now, UntilMs: now + 1000}
	}
	if replaced, ok := rt.Monsters.InstallMonsterTargetEffect(testDivision, gid, effect(10, 1), now); !ok || replaced != 0 {
		t.Fatal("first install", replaced, ok)
	}
	if replaced, ok := rt.Monsters.InstallMonsterTargetEffect(testDivision, gid, effect(11, 1), now); !ok || replaced != 10 {
		t.Fatal("same word did not replace", replaced, ok)
	}
	for tag := uint32(2); tag <= 8; tag++ {
		if replaced, ok := rt.Monsters.InstallMonsterTargetEffect(testDivision, gid, effect(10+tag, tag), now); !ok || replaced != 0 {
			t.Fatal("free slot", tag, replaced, ok)
		}
	}
	if _, ok := rt.Monsters.InstallMonsterTargetEffect(testDivision, gid, effect(30, 9), now); ok {
		t.Fatal("ninth instance admitted")
	}
	live, _ := rt.Monsters.Get(testDivision, gid)
	if live.TargetEffects[0].Token != 11 {
		t.Fatalf("slots %+v", live.TargetEffects)
	}
}

/*
================
TestVitalSpotLowersPlayerStat

On a hostile player the registry holds the instance and its write lowers
the recipient's evasion; the cast deals no damage.
================
*/
func TestVitalSpotLowersPlayerStat(t *testing.T) {
	skill := shippedOffense(t, "SKILL_CH_WATER_CANCEL_A_01")
	p := scornOpponentPair(t, skill)
	p.c.Skills = append(p.c.Skills, skill.ID)
	base, _, err := p.rt.playerCombatStats(testDivision, p.m)
	if err != nil {
		t.Fatal(err)
	}
	hp := enterworld.CurrentHP(p.m)
	out := p.cast(skill.ID)
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("Vital Spot refused: %+v (%s)", out, out.DiagnosticRefusal)
	}
	effects := p.rt.effects.Snapshot(testDivision, p.m.Name)
	if len(effects) != 1 || effects[0].SkillID != skill.ID {
		t.Fatalf("recipient effects %+v", effects)
	}
	if enterworld.CurrentHP(p.m) != hp {
		t.Fatal("Vital Spot dealt damage")
	}
	after, _, err := p.rt.playerCombatStats(testDivision, p.m)
	if err != nil {
		t.Fatal(err)
	}
	if after.EvasionRate >= base.EvasionRate {
		t.Fatalf("evasion %v -> %v", base.EvasionRate, after.EvasionRate)
	}
}
