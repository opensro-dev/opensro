/*
===========================================================================

sookarena.go - So-ok survival admission and leased instance lifecycle

Port-only, not native. The pure programme lives in game/sookarena; this
adapter owns character commits, existing relocation and population cleanup.
Run fields are accessed only while holding their division operation lock.

===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/sookarena"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
sookArenaRuntime
================
*/
type sookArenaRuntime struct {
	draining atomic.Bool
	runs     sync.Map
	byWorld  sync.Map
	pool     sookarena.Pool
	resolve  func(*enterworld.Character, simulation.Spawn) (simulation.Spawn, bool)
}

/*
================
sookArenaRun
================
*/
type sookArenaRun struct {
	clearing       bool
	pause          *sookPause
	lastChestPush  int64
	lastRemaining  int
	division, name string
	ownerID        int64
	lease          instance.Lease
	center         simulation.Spawn
	program        sookarena.Run
	refs           []monster.MonsterRef
	sequence       int
	bossPowerups   map[uint32]bool
	paid           bool
	powerupsOwed   int
	props          map[uint32]int64
	nextTick       int64
	firePitAt      simulation.Spawn
	firePitUntil   int64
	firePitNext    int64
}

/*
================
ConfigureSookArena - called once at startup, after monster/navigation wiring.
================
*/
func (rt *Runtime) ConfigureSookArena(value string) {
	if !sookarena.Enabled(value) || rt.Monsters == nil {
		return
	}
	authority, ok := rt.deps.(interface {
		ResolvePublicEventSpawn(*enterworld.Character, simulation.Spawn) (simulation.Spawn, bool)
	})
	if !ok {
		return
	}
	rt.sookArena = &sookArenaRuntime{pool: sookarena.BuildPool(rt.Monsters.CreatableRefs()), resolve: authority.ResolvePublicEventSpawn}
}

/*
================
sookTalkOptions
================
*/
func (rt *Runtime) sookTalkOptions(npc string) []NpcQuestOption {
	if rt.sookArena == nil || npc != sookarena.NPC {
		return nil
	}
	return []NpcQuestOption{{Codename: sookarena.Service, TitleSymbol: "UIIT_PORT_SOOK_ARENA", PromptSymbol: "UIIT_CTL_WK_EVENT", Immediate: true}}
}

/*
================
sookRun - a stale packed ID never grants a replacement world's rewards.
================
*/
func (rt *Runtime) sookRun(division string, c *enterworld.Character) *sookArenaRun {
	if rt.sookArena == nil || c == nil {
		return nil
	}
	value, ok := rt.sookArena.runs.Load(simulation.WorldKey(division, c.Name))
	if !ok {
		return nil
	}
	run := value.(*sookArenaRun)
	if uint32(run.lease.ID) != domain.CharacterWorldInstance(c) {
		return nil
	}
	lease, live := rt.Monsters.PopulationLease(division, run.lease.ID)
	if !live || lease != run.lease {
		return nil
	}
	return run
}

/*
================
enterSookArena - the dialog has already verified the selected So-ok.
================
*/
func (rt *Runtime) enterSookArena(division string, c *enterworld.Character) []wire.Frame {
	unlock := rt.lockDivision(division)
	defer unlock()
	return rt.enterSookArenaInsideDoor(division, c)
}

/*
================
enterSookArenaInsideDoor - entry and checkpoint resume share admission checks.
================
*/
func (rt *Runtime) enterSookArenaInsideDoor(division string, c *enterworld.Character) []wire.Frame {
	refuse := func(text string) []wire.Frame {
		return []wire.Frame{sookBanner(text), {Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer("SN_TALK_COMMON_DENY")}}
	}
	selected, selectedOK := rt.Selected.Get(division, c.Name)
	npc, liveNPC := rt.npcForCurrentViewer(division, c, selected)
	if !selectedOK || !liveNPC || npc.Codename != sookarena.NPC || !rt.npcWithinHitRange(division, c, npc) {
		return refuse("Select Event So-ok nearby to enter the Survival Arena.")
	}
	resume := c.SookChest != nil && c.SookChest.Checkpoint && !c.SookChest.Pending
	if c.SookChest != nil && c.SookChest.Pending {
		return rt.arenaChestFrames(c, nil, "")
	}
	if rt.sookArena == nil || !enterworld.CharacterAlive(c) || c.DeletePending || domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance || rt.jobDressed(c) || c.World != nil && c.World.InstanceReturn != nil {
		return refuse("Survival Arena: enter alive, outside a job or another event.")
	}
	if rt.Stalls.Keeping(division, c.Name) || rt.Exchanges.Trading(division, c.Name) {
		return refuse("Close your stall or exchange first.")
	}
	if _, open := rt.Stalls.Visiting(division, c.Name); open {
		return refuse("Close the stall first.")
	}
	if _, open := rt.storageSession(division, c.Name); open {
		return refuse("Close storage first.")
	}
	if _, _, ok := sookarena.Admit(c.SookArenaDay, c.SookArenaEntries, rt.Now()); !ok && !resume {
		return refuse("Survival Arena: all three entries for today have been used (UTC).")
	}
	refs := rt.sookArena.pool.StageNormals(int(rewardLevel(c)), 0)
	if len(refs) == 0 {
		return refuse("Survival Arena: no suitable encounters are installed for your level.")
	}
	var center simulation.Spawn
	valid := false
	// Port-only venue: the owned Colosseum safezone table locates its centre at 960/960.
	for _, candidate := range []simulation.Spawn{{RegionID: 25580, X: 960, Y: 0, Z: 960}} {
		occupied := false
		for _, npc := range rt.NpcRoster {
			if npc.Teleport == nil && npc.Spawn.RegionID == candidate.RegionID {
				occupied = true
				break
			}
		}
		if !occupied {
			center, valid = rt.sookArena.resolve(c, candidate)
		}
		if valid {
			break
		}
	}
	if !valid {
		return refuse("Survival Arena: the public arena navigation is unavailable.")
	}
	var lease instance.Lease
	for _, def := range []instance.DefinitionID{30, 31} {
		var status instance.Status
		lease, status = rt.OpenInstanceWorld(InstanceOpenRequest{Division: division, Definition: def})
		if status == instance.Success {
			break
		}
	}
	if lease.Generation == 0 {
		return refuse("Survival Arena: all solo arenas are occupied. Try again shortly.")
	}
	day, entries, active := c.SookArenaDay, c.SookArenaEntries, c.SookArenaActive
	// The instance lane commits its anchor before relocation; mark that crash window too.
	if !rt.deps.Update(c, "sook-entry-pending", func() bool {
		if c.DeletePending {
			return false
		}
		c.SookArenaActive = true
		return true
	}) {
		rt.closeInstanceWorld(division, lease)
		return refuse("Survival Arena: entry could not be saved. Try again.")
	}
	status := rt.transferCharacterToInstanceAdmitted(division, c, lease, center, func() bool {
		if resume {
			return true
		}
		nextDay, nextEntries, ok := sookarena.Admit(c.SookArenaDay, c.SookArenaEntries, rt.Now())
		if !ok {
			return false
		}
		c.SookArenaDay, c.SookArenaEntries, c.SookArenaActive = nextDay, nextEntries, true
		return true
	})
	if status != instance.Success {
		rt.deps.Update(c, "sook-entry-rollback", func() bool { c.SookArenaDay, c.SookArenaEntries, c.SookArenaActive = day, entries, active; return true })
		rt.closeInstanceWorld(division, lease)
		return refuse("Survival Arena: entry failed; your daily entry was not spent.")
	}
	run := &sookArenaRun{division: division, name: c.Name, ownerID: c.ID, lease: lease, center: center, refs: refs, program: sookarena.New(rt.Now().UnixMilli()), bossPowerups: map[uint32]bool{}}
	run.program.CheckpointInterval = int64(arenaSetting("SRO_SOOK_CHECKPOINT_SECONDS", 300, 30, 300)) * 1000
	if resume {
		run.program.Started = rt.Now().UnixMilli() - c.SookChest.ElapsedMs
		run.program.Completed, run.program.Kills = c.SookChest.Stage, c.SookChest.Kills
		run.program.CheckpointKills = c.SookChest.Kills
		run.program.Unique = run.program.Completed
		run.program.PausedAt, run.program.DecisionUntil = rt.Now().UnixMilli(), rt.Now().UnixMilli()+sookarena.DecisionMs
		run.pause = pauseSookSkills(c)
	} else if !rt.deps.Update(c, "sook-chest-open", func() bool {
		c.SookChest = &domain.ArenaChest{ID: fmt.Sprintf("%d:%d:%d", c.ID, rt.Now().UnixMilli(), lease.Generation)}
		return true
	}) {
		rt.endSookArena(run, c, false)
		return refuse("Survival Arena: rewards could not be saved.")
	}
	rt.sookArena.runs.Store(simulation.WorldKey(division, c.Name), run)
	rt.sookArena.byWorld.Store(instanceWorldKey{division, lease.ID}, run)
	return []wire.Frame{sookBanner(fmt.Sprintf("Survival: five stages, EXP/SP x5, chest rewards, no floor loot. Entry %d/3. First wave in 20s.", c.SookArenaEntries)), rt.arenaChestFrame(c, run, "")}
}

/*
================
advanceSookArena - bounded by 200 solo layers; no catch-up spawn backlog.
================
*/
func (rt *Runtime) advanceSookArena(now int64) {
	if rt.sookArena == nil {
		return
	}
	rt.sookArena.runs.Range(func(_, value any) bool {
		run := value.(*sookArenaRun)
		unlock := rt.lockDivision(run.division)
		defer unlock()
		if now < run.nextTick {
			return true
		}
		run.nextTick = now + 100
		c := rt.findCharacter(run.division, run.name)
		if c == nil || rt.sookRun(run.division, c) != run {
			if c != nil {
				rt.forgetSookArena(run.division, run.name)
				return true
			}
			rt.endSookArena(run, c, false)
			return true
		}
		if _, present := rt.EntryPopulationLease(run.division, run.name); !present {
			rt.forgetSookArena(run.division, run.name)
			return true
		}
		actors := rt.Monsters.PopulationInstances(run.division, run.lease, []uint16{run.center.RegionID})
		alive := 0
		for _, actor := range actors {
			if actor.CurrentHP == 0 {
				continue
			}
			alive++
			if actor.Ref.MonsterType == 3 && actor.CurrentHP <= actor.EffectiveMaxHP()/2 && !run.bossPowerups[actor.Gid] {
				run.bossPowerups[actor.Gid] = true
				if run.program.PausedAt == 0 {
					rt.spawnSookPowerup(run, c, now)
				}
			}
		}
		paused := run.program.PausedAt

		step := run.program.Advance(now, alive, !enterworld.CharacterAlive(c))
		if step.Countdown > 0 {
			rt.PushCharacterFrames(run.division, c.Name, []wire.Frame{sookBanner(fmt.Sprintf("Stage %d starts in %ds", run.program.Completed+1, step.Countdown))})
		}
		if step.StageEnd {
			rt.expireSookProps(run, math.MaxInt64)
			run.powerupsOwed = 0
			run.lastRemaining = -1
			rt.PushCharacterFrames(run.division, c.Name, []wire.Frame{sookBanner(fmt.Sprintf("End of stage %d - kill all %d remaining monsters!", run.program.Completed+1, alive))})
		}
		if run.program.ClearingAt != 0 && (run.program.PausedAt == 0 || step.Checkpoint) && alive <= 10 && alive != run.lastRemaining {
			run.lastRemaining = alive
			rt.PushCharacterFrames(run.division, c.Name, []wire.Frame{sookBanner(fmt.Sprintf("%d monsters remaining", alive))})
		}
		if step.Hunt {
			for _, actor := range actors {
				if actor.CurrentHP > 0 {
					rt.commitAggression(run.division, actor.Gid, simulation.HostilityEvent{Attacker: enterworld.ObjectIDForCharacter(c), Aggression: 1}, now)
				}
			}
			if step.HuntNotice {
				rt.PushCharacterFrames(run.division, c.Name, []wire.Frame{sookBanner("One minute has passed. The remaining monsters are hunting you!")})
			}
		}
		if step.Return {
			rt.endSookArena(run, c, step.Win)
			return true
		}
		if !enterworld.CharacterAlive(c) {
			return true
		}
		if step.Checkpoint || run.program.PausedAt != 0 && c.SookChest != nil && c.SookChest.Stage < run.program.Completed {
			rt.expireSookProps(run, math.MaxInt64)
			run.powerupsOwed = 0
			if !rt.saveSookCheckpoint(run, c, run.program.Completed) {
				return true
			}
			run.pause = pauseSookSkills(c)
			run.lastChestPush = 0
		}
		if run.program.PausedAt != 0 {
			if run.lastChestPush == 0 {
				run.lastChestPush = now
				rt.PushCharacterFrames(run.division, c.Name, rt.arenaChestFrames(c, run, ""))
			}
			return true
		}
		if step.Resume {
			run.clearing = false
			delta := now - paused
			rt.shiftSookAbnormals(run.division, c, delta)
			rt.deps.Update(c, "sook-checkpoint-resume", func() bool {
				resumeSookSkills(c, run.pause, paused, delta)
				c.SookChest.Checkpoint = false
				return true
			})
			for _, at := range []*int64{&run.firePitUntil, &run.firePitNext} {
				if *at != 0 {
					*at += delta
				}
			}
			rt.expireSookProps(run, math.MaxInt64)
			frames := append(sookPowerResume(run, now), rt.refreshMovementEffects(run.division, c, now)...)
			if rt.AttackPetExpansion {
				for _, pet := range c.Companions() {
					frames = append(frames, rt.petTrainingStateFrames(pet, now)...)
				}
			}
			rt.PushCharacterFrames(run.division, c.Name, append(frames, rt.arenaChestFrames(c, run, "")...))
		}
		if step.Powerup {
			rt.spawnSookPowerup(run, c, now)
		}
		rt.tickSookFirePit(run, c, now)
		if run.powerupsOwed > 0 {
			rt.spawnSookPowerup(run, c, now)
			run.powerupsOwed--
		}
		if frames := sookPowerEnds(run, now, false); len(frames) > 0 {
			frames = append(frames, rt.refreshMovementEffects(run.division, c, now)...)
			rt.PushCharacterFrames(run.division, c.Name, frames)
		}

		if step.Box {
			rt.spawnSookBox(run, c, now)
		}
		if step.Wave {
			if refs := rt.sookArena.pool.StageNormals(int(rewardLevel(c)), run.program.Completed); len(refs) > 0 {
				run.refs = refs
			}
			for i := 0; i < step.Count; i++ {
				ref := run.refs[run.sequence%len(run.refs)]
				rt.spawnSookMonster(run, ref, sookarena.WaveRarity(run.sequence), now)
				run.sequence++
			}
		}
		if step.Unique {
			if boss, ok := rt.sookArena.pool.StageBoss(int(rewardLevel(c)), run.program.Completed); ok {
				if !rt.spawnSookMonster(run, boss, 3, now) {
					run.program.Unique--
					return true
				}
				rt.PushCharacterFrames(run.division, c.Name, []wire.Frame{{Opcode: 0x3058, Payload: wire.NewWriter(5).U8(5).U32(boss.RefObjID).Payload()}})
			}
		}

		rt.expireSookProps(run, now)
		return true
	})
}

/*
================
spawnSookMonster - existing actor factory validates each ground point.
================
*/
func (rt *Runtime) spawnSookMonster(run *sookArenaRun, ref monster.MonsterRef, rarity uint8, now int64) bool {
	c := rt.findCharacter(run.division, run.name)
	if rt.sookRun(run.division, c) != run {
		return false
	}
	center := rt.liveSpawn(simulation.WorldKey(run.division, run.name), c, now)
	for attempt := 0; attempt < 8; attempt++ {
		angle := float64(run.sequence*7+attempt) * 2.399963
		at := center
		radius := float64(80 + (run.sequence+attempt)%4*50)
		at.X += math.Cos(angle) * radius
		at.Z += math.Sin(angle) * radius
		at, valid := rt.sookArena.resolve(c, at)
		if !valid || at.RegionID != run.center.RegionID {
			continue
		}
		// Three evenly spaced aggressive spawns per ten; the opening three include one.
		aggressive := (run.sequence*3)%10 < 3
		created := rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{Division: run.division, Population: run.lease, RefObjID: ref.RefObjID, Count: 1, Type: rarity, Position: at, NowMs: now, Arena: true, ArenaPassive: !aggressive})
		if len(created) > 0 {
			for _, gid := range created {
				if !aggressive {
					continue
				}
				// Port-only: the solo entrant is the encounter's opponent from spawn, without damage credit.
				rt.commitAggression(run.division, gid, simulation.HostilityEvent{Attacker: enterworld.ObjectIDForCharacter(c), Aggression: 1}, now)
			}
			return true
		}
	}
	return false
}

/*
================
endSookArena - return first, release only once residents are evacuated.
================
*/
func (rt *Runtime) endSookArena(run *sookArenaRun, c *enterworld.Character, win bool) {
	if value, ok := rt.sookArena.runs.Load(simulation.WorldKey(run.division, run.name)); ok && value != run {
		c = nil
		win = false
	}
	if c != nil && domain.CharacterWorldInstance(c) == uint32(run.lease.ID) {

		if !enterworld.CharacterAlive(c) {
			rt.deps.Update(c, "sook-revive", func() bool {
				rt.reviveWhereDead(run.division, c, resurrectionOffer{hp: math.MaxInt32}, rt.Now().UnixMilli())
				return true
			})
		}
		if (win || run.program.DeadAt != 0) && !run.paid {
			reward := sookarena.KillReward(run.program.Kills)
			run.paid = rt.deps.Update(c, "sook-payout", func() bool {
				if c.SookChest != nil && c.SookChest.GoldPaid {
					return true
				}
				if goldOf(c) > math.MaxInt64-reward {
					return false
				}
				setGold(c, goldOf(c)+reward)
				if c.SookChest != nil {
					c.SookChest.GoldPaid = true
					c.SookChest.Kills = run.program.Kills
					c.SookChest.Pending = win
					c.SookChest.Checkpoint = false
					if !win {
						c.SookChest = nil
					}
				}
				return true
			})
			if run.paid {
				rt.PushCharacterFrames(run.division, c.Name, []wire.Frame{{Opcode: wire.OpPointsUpdate, Payload: wire.GoldRefresh{Balance: goldOf(c), Notify: true}.Encode()}, sookBanner(fmt.Sprintf("Arena finished: %d kills. Reward: %d gold.", run.program.Kills, reward))})
			}
		}
		if (win || run.program.DeadAt != 0) && !run.paid {
			return
		}
		fault := ""
		if win {
			_, fault = rt.claimSookChest(c)
		}
		if rt.returnCharacterFromInstance(run.division, c) != instance.Success {
			return
		}
		if win {
			// The relocation bootstrap already carries the newly granted bag.
			rt.PushCharacterFrames(run.division, c.Name, rt.arenaChestFrames(c, nil, fault))
		}
	} else if c != nil && c.SookArenaActive && c.World != nil && c.World.InstanceReturn != nil {
		if rt.returnCharacterFromInstance(run.division, c) != instance.Success {
			return
		}
	}
	if c != nil {
		if rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(run.division, c.Name, sookPowerEnds(run, rt.Now().UnixMilli(), true))
		}
		if !win && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(run.division, c.Name, []wire.Frame{sookBanner("Survival Arena ended. Return to So-ok to try again.")})
		}
		rt.deps.Update(c, "sook-end", func() bool {
			c.SookArenaActive = false
			if !win {
				c.SookChest = nil
			}
			return true
		})
	}
	rt.sookArena.runs.CompareAndDelete(simulation.WorldKey(run.division, run.name), run)
	log.WithFields(log.Fields{"character": run.name, "win": win, "kills": run.program.Kills, "stage": run.program.Completed, "death": run.program.DeadAt != 0}).Info("arena finished")
	rt.sookArena.byWorld.CompareAndDelete(instanceWorldKey{run.division, run.lease.ID}, run)
	rt.closeInstanceWorld(run.division, run.lease)
	rt.Ground.RemovePopulation(run.division, grounditem.Population{World: uint32(run.lease.ID), Generation: run.lease.Generation})
}

/*
================
recoverSookArena - persisted arena markers survive a disabled flag/restart.
================
*/
func (rt *Runtime) recoverSookArena(division string, c *enterworld.Character) {
	if c == nil || !c.SookArenaActive {
		return
	}
	stats, _, _ := rt.playerCombatStats(division, c)
	maxHP, _, _, _ := keeperVitalsFrom(c, stats, nil)
	if rt.deps.Update(c, "sook-recover", func() bool {
		if c.World != nil && c.World.InstanceReturn != nil {
			a := *c.World.InstanceReturn
			state := simulation.SeedWorldState(c)
			state.Spawn = simulation.Spawn{RegionID: a.RegionID, X: float64(a.X), Y: float64(a.Y), Z: float64(a.Z), Angle: a.Angle}
			state.MoveSegment = nil
			writeBackWorld(c, state)
			setCharacterWorld(c, instance.ID(a.PackedInstance))
			c.World.InstanceReturn = nil
		}
		if !enterworld.CharacterAlive(c) {
			hp := max(int64(1), maxHP)
			c.CurrentHP = &hp
			c.DeathKind = 0
			c.LastExpLoss = 0
		}
		c.SookArenaActive = false
		if c.SookChest != nil && !c.SookChest.Checkpoint {
			c.SookChest.Pending = true
		}
		return true
	}) {
		// Admission must seed from the restored town, not the previous socket's arena pose.
		rt.Worlds.Forget(simulation.WorldKey(division, c.Name))
	}
}

/*
================
forgetSookArena - disconnect forfeits, restoring a safe persistent town pose.
================
*/
func (rt *Runtime) forgetSookArena(division, name string) {
	c := rt.findCharacter(division, name)
	if rt.sookArena != nil {
		if value, ok := rt.sookArena.runs.LoadAndDelete(simulation.WorldKey(division, name)); ok {
			run := value.(*sookArenaRun)
			if c != nil && (run.program.PausedAt == 0 || run.program.ResumeAt != 0) && !rt.sookArena.draining.Load() {
				rt.deps.Update(c, "sook-abort-forfeit", func() bool { c.SookChest = nil; return true })
			}
			rt.sookArena.byWorld.CompareAndDelete(instanceWorldKey{division, run.lease.ID}, run)
			rt.recoverSookArena(division, c)
			rt.closeInstanceWorld(division, run.lease)
			rt.Ground.RemovePopulation(division, grounditem.Population{World: uint32(run.lease.ID), Generation: run.lease.Generation})
		}
	}
}
