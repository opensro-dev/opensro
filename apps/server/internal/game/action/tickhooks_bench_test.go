/*
===========================================================================

tickhooks_bench_test.go - the five hooks' idle-tick cost (#1820 D part 2)

D part 1 measured the hooks with long tails in the tick profile; these
five were named next. The benchmarks run each hook the way the ticker
runs it, over the crowd the store holds, in the common case where
nothing is due: the sweep cost is then the check itself, paid every
tick whether or not any player, pet, aura, caravan or monster cast is
active.

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// tickHookBenchNowMs is the fake clock's instant every bench ticks at.
const tickHookBenchNowMs = int64(1_000_000)

/*
================
BenchmarkAdvancePetsIdleCrowd

Every crowd member online with a summoned pet standing by: no target,
no training, nothing due. advancePets still walks every session.
================
*/
func BenchmarkAdvancePetsIdleCrowd(b *testing.B) {
	rt, _, clock, crowd := crowdStoreRuntime(b, true)
	rt.petSessions = make(map[petOwnerKey]*petSession, len(crowd))
	for _, c := range crowd {
		gid, _ := enterworld.CosObjectIDForCharacter(c)
		c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
		rt.petSessions[petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: gid}] =
			&petSession{character: c, ready: true}
	}
	now := clock.NowMs()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rt.advancePets(now)
	}
}

/*
================
BenchmarkAdvancePartyAurasIdle

One open aura per crowd member, its next scan an hour away: no pulse,
no join or leave due. advancePartyAuras retires and installs nothing,
but it looks every aura up.
================
*/
func BenchmarkAdvancePartyAurasIdle(b *testing.B) {
	const auraSkillID = uint32(777001)
	rt, _, clock, crowd := crowdStoreRuntime(b, true)
	rt.deps.(*enterworld.Deps).Skills = staticSkillSource{auraSkillID: {
		ID:       auraSkillID,
		Group:    9,
		Codename: "SKILL_BENCH_AURA",
		Abnormal: abnormal.SkillParams{PulsePresent: true, Pulse: 60000},
	}}
	for _, c := range crowd {
		token := uint32(0x1000 + c.ID)
		if !rt.ApplyCharacterEffect(testDivision, c.Name, auraSkillID, token, statuseffect.StateActive, false) {
			b.Fatalf("aura effect for %s did not apply", c.Name)
		}
		rt.partyAuras = append(rt.partyAuras, partyAura{
			division: testDivision, casterName: c.Name, skillID: auraSkillID, token: token,
			nextScan: tickHookBenchNowMs + 3600000, members: map[string]uint32{},
		})
	}
	now := clock.NowMs()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rt.advancePartyAuras(now)
	}
}

/*
================
BenchmarkAdvanceCaravansIdle

A registry of caravans whose timers are minutes away, ticked at a
fixed instant so no timer ever comes due: advanceCaravans walks every
timer and fires none.
================
*/
func BenchmarkAdvanceCaravansIdle(b *testing.B) {
	rt, _, _, crowd := crowdStoreRuntime(b, true)
	roll := func() (uint32, error) { return 16384, nil }
	for _, c := range crowd {
		if err := rt.caravans.Register(testDivision, c.Name, roll); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rt.advanceCaravans(tickHookBenchNowMs)
	}
}

/*
================
tickHookBenchMonsterRuntime

A runtime over one spawned monster and its target, as the combat
fixture builds it, sized for benchmarks (no store-door guard).
================
*/
func tickHookBenchMonsterRuntime(b *testing.B) (*Runtime, *enterworld.Character, monster.Instance) {
	b.Helper()
	const region = uint16(0x62A8)
	clock := &fakeClock{now: time.UnixMilli(tickHookBenchNowMs)}
	ref := monster.MonsterRef{
		RefObjID: 1933, TidWord: 0x00c6, Codename: "MOB_CH_MANGNYANG",
		Level: 85, MaxHP: 100, BodyRadius: 6, ExpToGive: 24,
		RewardActionPinned: true, CombatPinned: true,
		PhysicalDefense: 7, MagicalDefense: 10, ParryRate: 1, MagicalParry: 1,
		EvasionRate: 27, HitRate: 27, CriticalRate: 2,
	}
	monsters := simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{ref.RefObjID: ref},
		[]monster.NestRow{{
			SpawnPoint:     monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: region, X: 963, Y: 20, Z: 458},
			RetailEvidence: true, MaxCount: 1,
		}},
	))
	monsters.SetTimeSource(clock.Now)
	monsters.StartDivision(testDivision)
	monsters.AdvancePopulation(monsters.CurrentTimeMillis())
	instances := monsters.InstancesInRegions(testDivision, []uint16{region})
	if len(instances) != 1 {
		b.Fatalf("bench fixture materialized %d monsters, want one", len(instances))
	}
	character := testCharacter()
	character.BattleUntilMs = int64(^uint64(0) >> 1)
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Items:      testItems(),
		Skills: staticSkillSource{2: {
			ID: 2, Codename: "SKILL_CH_SWORD_BASE_01", CombatPinned: true,
		}},
	}
	rt := NewRuntime(deps, monsters)
	rt.Now = clock.Now
	return rt, character, instances[0]
}

/*
================
BenchmarkMonsterActionTickHookIdle

No summons retiring, no pending monster cast, no finalize due: the
hook takes the publication gate and walks its three empty queues.
================
*/
func BenchmarkMonsterActionTickHookIdle(b *testing.B) {
	rt, _, _ := tickHookBenchMonsterRuntime(b)
	hook := rt.MonsterActionTickHook()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hook(tickHookBenchNowMs)
	}
}

/*
================
BenchmarkMonsterActionTickHookPendingCasts

Pending monster casts still inside their casting time: each one is
revalidated every tick until its release. The count is the in-flight
casts of a busy fight.
================
*/
func BenchmarkMonsterActionTickHookPendingCasts(b *testing.B) {
	for _, casts := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("casts=%d", casts), func(b *testing.B) {
			rt, killer, mob := tickHookBenchMonsterRuntime(b)
			for i := range casts {
				rt.pendingMonsterCasts = append(rt.pendingMonsterCasts, pendingMonsterCast{
					division:      testDivision,
					instance:      mob,
					characterName: killer.Name,
					characterID:   killer.ID,
					token:         uint32(0x9000 + i),
					releaseAtMs:   tickHookBenchNowMs + 60000,
					skill:         2,
				})
			}
			hook := rt.MonsterActionTickHook()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				hook(tickHookBenchNowMs)
			}
		})
	}
}
