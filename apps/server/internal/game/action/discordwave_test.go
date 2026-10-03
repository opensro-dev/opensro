/*
===========================================================================

discordwave_test.go - the Bard's Discord Wave clears monsters' hostility

The shipped Discord Wave row is cast through HandleTargetInteract on the
Bard itself; the monsters chasing the Bard then run the real monster leg.
Native sends each victim one negative hate event sourced at the cast's
target (593D62..593E7B) through the ordinary ledger update (5473C0).

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// discordCode is Discord Wave's last tier, dtnt(21194,0) mwdt(850).
	discordCode = "SKILL_EU_BARD_FORGETA_AGGRO_A_11"
	// discordFlat is that tier's dtnt flat word.
	discordFlat = 21194
	// discordMaxTargets and discordRadius are its efr words.
	discordMaxTargets = 4
	discordRadius     = 100
	// discordFarOffset puts a monster beyond the radius plus both body
	// radii from the Bard.
	discordFarOffset = 200
	// discordSmallHostility is less than any cut; discordLargeHostility
	// is more than this tier's cut.
	discordSmallHostility = 100
	discordLargeHostility = 1000000
)

/*
================
TestDiscordWaveReleasesUpToFourMonstersAroundItsTarget

Six monsters chase the Bard: five within 100 of it, one 200 away. The
first holds more hostility than the cut. Discord Wave on the Bard cuts
exactly four of the five near ones: the first keeps less hostility, the next
three are left with none. 5473C0 clamps at zero and, when the primary record
reaches it, refuses the callback, so each keeps the target it chases until
another opponent's hate takes over. The fifth near one and the far one are
untouched.
================
*/
func TestDiscordWaveReleasesUpToFourMonstersAroundItsTarget(t *testing.T) {
	rt, clock, c, original := newCombatTestRuntime(t, 1000)
	skill := shippedOffense(t, discordCode)
	if !skill.Threat.Decrease || skill.Threat.DecreaseFlat != discordFlat || skill.Threat.Area.MaxTargets != discordMaxTargets ||
		skill.Threat.Area.Radius != discordRadius || skill.Threat.Area.Shape != 2 || skill.Threat.Area.Select != 16 || !skill.Targets.Self {
		t.Fatalf("catalog shape: %+v refusal %q", skill.Threat, skill.OffenseRefusal)
	}
	skills := rt.deps.SkillData().(staticSkillSource)
	attack := skills[temptationAttackSkill]
	attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent = 1, 1, 100
	skills[temptationAttackSkill] = attack
	ref := original.Ref
	ref.DefaultSkillIDs[0], ref.RunSpeed, ref.WalkSpeed, ref.ScaleDenom = temptationAttackSkill, 22, 8, 100
	state := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nil))
	state.SetTimeSource(clock.Now)
	state.SetRandomSource(func() float64 { return 0 })
	state.SetAbnormalContext(monsterAbnormalContext{rt})
	rt.Monsters = state
	gid := enterworld.ObjectIDForCharacter(c)
	at := monster.Pose{RegionID: original.Spawn.RegionID, X: original.Spawn.X, Y: original.Spawn.Y, Z: original.Spawn.Z}
	var mobs []monster.Instance
	for i, offset := range []float64{10, 20, 30, 40, 50, discordFarOffset} {
		pose := at
		pose.X += offset
		mob, err := state.DevelopmentCreateLeader(testDivision, ref.RefObjID, pose, clock.NowMs()+100000)
		if err != nil {
			t.Fatal(err)
		}
		if !state.ArmRetaliation(testDivision, mob.Gid, gid) {
			t.Fatal("no retaliation")
		}
		hostility := int32(discordSmallHostility)
		if i == 0 {
			hostility = discordLargeHostility
		}
		rt.commitAggression(testDivision, mob.Gid, simulation.HostilityEvent{Attacker: gid, Aggression: hostility}, clock.NowMs())
		mobs = append(mobs, mob)
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}
			w.SpawnSet = true
		})
	*c.World.Spawn.X = at.X
	hostility := func(mob monster.Instance) int32 {
		live, _ := rt.Monsters.Get(testDivision, mob.Gid)
		for _, record := range live.Opponents {
			if record.GID == gid {
				return record.Aggression
			}
		}
		return 0
	}
	before := make([]int32, len(mobs))
	for i, mob := range mobs {
		before[i] = hostility(mob)
		if mover, _ := rt.Monsters.Mover(testDivision, mob.Gid); mover.TargetGID() != gid || before[i] <= 0 {
			t.Fatalf("monster %d does not fight the Bard: hostility %d, mover %+v", i, before[i], mover)
		}
	}

	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = bardHarpKind
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	mp := enterworld.CurrentMP(c)
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 {
		t.Fatalf("Discord Wave was refused: %q %+v", out.DiagnosticRefusal, out.Frames)
	}
	if enterworld.CurrentMP(c) >= mp {
		t.Fatalf("MP not charged: %d -> %d", mp, enterworld.CurrentMP(c))
	}

	// One planner step consumes the release.
	clock.Advance(temptationTick)
	lease, _ := rt.Monsters.ObjectPopulation(testDivision, mobs[0].Gid)
	viewer := simulation.SessionSnapshot{SessionID: "discord", DivisionID: testDivision, CharacterID: c.ID,
		Population: lease, WorldInstance: uint32(lease.ID), BodyRadius: 4, CombatEligible: true,
		World: simulation.SeedWorldState(c)}
	ops := &simulation.MonsterMoverOps{Monsters: state, Rand: func() float64 { return 0 }, AttackPlan: rt.MonsterAttackPlan, RunAction: rt.RunMonsterAction}
	ops.RunMonsterLeg(clock.NowMs(), []simulation.SessionSnapshot{viewer}, &summonTickPusher{})

	cut := 0
	for i, mob := range mobs {
		after := hostility(mob)
		mover, _ := rt.Monsters.Mover(testDivision, mob.Gid)
		keeps := mover.TargetGID() == gid
		if after < before[i] {
			cut++
		}
		switch {
		case i == 0:
			if after >= before[i] || after <= 0 || !keeps {
				t.Fatalf("the hostile monster: hostility %d -> %d, keeps the Bard %v", before[i], after, keeps)
			}
		case i < discordMaxTargets:
			if after != 0 || !keeps {
				t.Fatalf("monster %d: hostility %d -> %d, keeps the Bard %v", i, before[i], after, keeps)
			}
		default:
			if after != before[i] || !keeps {
				t.Fatalf("monster %d beyond the cap or the radius was touched: hostility %d -> %d, keeps the Bard %v", i, before[i], after, keeps)
			}
		}
	}
	if cut != discordMaxTargets {
		t.Fatalf("%d monsters lost hostility, want %d", cut, discordMaxTargets)
	}
}
