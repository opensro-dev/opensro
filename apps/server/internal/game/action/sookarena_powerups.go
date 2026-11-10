/*
===========================================================================

sookarena_powerups.go - arena-only floor props and damage settlement

Port-only, not native. Tagged props are lease-scoped, expire after 40 seconds,
and are consumed directly; no character or pickup-pet inventory can keep one.

===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"opensro.online/server/internal/domain"
	"unicode/utf16"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/sookarena"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	sookDamageScroll = "ITEM_ETC_GNGWC_DAMAGE_UP_20"
	sookShockBomb    = "ITEM_FORT_SHOCK_BOMB"
	sookBox          = "ITEM_ETC_E060517_MON_GENERATION_BOX"
	sookEgg          = "ITEM_ETC_E060411_EGG"
	sookDamage       = uint8(1)
	sookBomb         = uint8(2)
	sookUnstoppable  = uint8(3)
	sookBoxKind      = uint8(4)
	sookInvincible   = uint8(5)
	sookMana         = uint8(6)
	sookFirePit      = uint8(7)
	sookReflect      = uint8(8)
	sookAlarm        = uint8(9)
	sookAmbush       = uint8(10)
	sookTransfer     = uint8(11)
	sookSpeed        = uint8(12)
	// Port-only arena positions on the existing cosmetic cue lane.
	sookBlastVisual   = uint8(9)
	sookFirePitVisual = uint8(10)
	// Port-only virtual board references, not inventory items or persisted param jobs.
	sookPowerRefBase = uint32(0x7f530000)
)

/*
================
sookBanner - retain the native subtype-7 UTF-16 length contract.
================
*/
func sookBanner(text string) wire.Frame {
	units := utf16.Encode([]rune(text))
	if len(units) > 96 {
		units = units[:96]
		if units[95] >= 0xd800 && units[95] <= 0xdbff {
			units = units[:95]
		}
	}
	return wire.NotificationFrame(string(utf16.Decode(units)))
}

/*
================
sookPopulation
================
*/
func (rt *Runtime) sookPopulation(division string, lease instance.Lease) *sookArenaRun {
	if rt.sookArena == nil {
		return nil
	}
	value, ok := rt.sookArena.byWorld.Load(instanceWorldKey{division, lease.ID})
	if !ok {
		return nil
	}
	run := value.(*sookArenaRun)
	if run.lease != lease {
		return nil
	}
	return run
}

/*
================
spawnSookProp
================
*/
func (rt *Runtime) spawnSookProp(run *sookArenaRun, c *enterworld.Character, codename string, kind uint8, now int64) {
	for attempt := 0; attempt < 8; attempt++ {
		angle := float64(run.sequence+int(now/1000)+attempt) * 2.399963
		at := rt.liveSpawn(simulation.WorldKey(run.division, c.Name), c, now)
		radius := float64(80 + (attempt%4)*40)
		at.X += math.Cos(angle) * radius
		at.Z += math.Sin(angle) * radius
		if at.RegionID != run.center.RegionID {
			continue
		}
		at, ok := rt.sookArena.resolve(c, at)
		if !ok {
			continue
		}
		item, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: codename, Count: 1, Assigned: true}, at, c.Name, rt.Now())
		if !ok {
			return
		}
		item.ArenaPowerup = kind
		item.OwnerJID = enterworld.ObjectIDForCharacter(c)
		item = rt.addCharacterGround(run.division, c, item)
		if item.Gid == 0 {
			return
		}
		if run.props == nil {
			run.props = make(map[uint32]int64)
		}
		lifetime := int64(40000)
		if kind == sookBoxKind {
			lifetime = 1200
		}
		run.props[item.Gid] = item.DroppedAt.UnixMilli() + lifetime
		frames := append(rt.groundReferences([]grounditem.Item{item}), wire.DropBroadcastFrames(item.SpawnRow(true))...)
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: run.division, SourceGID: enterworld.ObjectIDForCharacter(c), Frames: simFrames(frames)}})
		return
	}
}

/*
================
spawnSookBox - a marked, unlootable prop precedes the monster wave.
================
*/
func (rt *Runtime) spawnSookBox(run *sookArenaRun, c *enterworld.Character, now int64) {
	rt.spawnSookProp(run, c, sookBox, sookBoxKind, now)
}

/*
================
spawnSookPowerup
================
*/
func (rt *Runtime) spawnSookPowerup(run *sookArenaRun, c *enterworld.Character, now int64) {
	kinds := [...]uint8{sookDamage, sookBomb, sookUnstoppable, sookInvincible, sookMana, sookFirePit, sookReflect, sookAlarm, sookAmbush, sookTransfer, sookSpeed}
	roll, err := rt.CombatRoll()
	if err != nil {
		return
	}
	rt.spawnSookProp(run, c, sookEgg, kinds[int(roll)%len(kinds)], now)
}

/*
================
expireSookProps
================
*/
func (rt *Runtime) expireSookProps(run *sookArenaRun, now int64) {
	population := grounditem.Population{World: uint32(run.lease.ID), Generation: run.lease.Generation}
	for gid, deadline := range run.props {
		if now < deadline {
			continue
		}
		delete(run.props, gid)
		item, exists := rt.Ground.GetInPopulation(run.division, population, gid)
		if exists {
			rt.Ground.Remove(run.division, item.Gid)
		}
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: run.division, OnlyCharacterID: run.ownerID, Frames: simFrames([]wire.Frame{groundDespawn(gid)})}})
	}
}

/*
================
groundDespawn
================
*/
func groundDespawn(gid uint32) wire.Frame {
	return wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode(), Scope: []domain.ObjectScopeChange{{GID: gid}}}
}

/*
================
pickupSookPowerup - caller already owns the division pickup operation.
================
*/
func (rt *Runtime) pickupSookPowerup(division, key string, c *enterworld.Character, item grounditem.Item) OpResult {
	run := rt.sookRun(division, c)
	if run == nil || run.program.PausedAt != 0 || item.ArenaPowerup == 0 || item.ArenaPowerup == sookBoxKind || item.ArenaPowerup > sookSpeed || item.Population != (grounditem.Population{World: uint32(run.lease.ID), Generation: run.lease.Generation}) || !enterworld.CharacterAlive(c) {
		return pickupRefusal(wire.ErrCodeInvalidRequest)
	}
	live, ok := rt.characterGround(division, c, item.Gid)
	if !ok || live.ArenaPowerup != item.ArenaPowerup || rt.Now().UnixMilli() >= live.DroppedAt.UnixMilli()+40000 {
		return pickupRefusal(wire.ErrCodeTargetGone)
	}
	multiplier := uint32(2)
	if item.ArenaPowerup == sookDamage {
		roll, err := rt.CombatRoll()
		if err != nil {
			return pickupRefusal(wire.ErrCodeInvalidRequest)
		}
		multiplier += roll % 9
	}
	if _, ok := rt.Ground.Remove(division, item.Gid); !ok {
		return pickupRefusal(wire.ErrCodeTargetGone)
	}
	delete(run.props, item.Gid)
	at := rt.liveSpawn(key, c, rt.Now().UnixMilli())
	anim := wire.PickupAnim{Gid: enterworld.ObjectIDForCharacter(c), Heading: wire.HeadingByteFromAngle(at.Angle)}
	frames := wire.PickupBroadcastFrames(anim, item.Gid, 0)
	result := OpResult{Frames: frames, Broadcast: frames}
	now := rt.Now().UnixMilli()
	switch item.ArenaPowerup {
	case sookDamage:
		run.program.DamageUntil = rt.Now().UnixMilli() + 30000
		run.program.DamageMultiplier = multiplier
		result.Frames = append(result.Frames, sookBanner(fmt.Sprintf("Damage Surge: %dx outgoing damage for 30 seconds!", multiplier)))
	case sookBomb:
		rt.detonateSookBomb(run, c, simulation.Spawn{RegionID: item.Position.RegionID, X: float64(item.Position.X), Y: float64(item.Y), Z: float64(item.Position.Z)}, rt.Now().UnixMilli())
		result.Frames = append(result.Frames, crowVisualFrame(crowVisual{gid: enterworld.ObjectIDForCharacter(c), kind: sookBlastVisual, book: 1, at: simulation.Spawn{RegionID: item.Position.RegionID, X: float64(item.Position.X), Y: float64(item.Y), Z: float64(item.Position.Z)}}), sookBanner("KABUMMMMMM"))
	case sookFirePit:
		run.firePitAt = simulation.Spawn{RegionID: item.Position.RegionID, X: float64(item.Position.X), Y: float64(item.Y), Z: float64(item.Position.Z)}
		run.firePitUntil, run.firePitNext = now+60000, now+1000
		result.Frames = append(result.Frames, crowVisualFrame(crowVisual{gid: enterworld.ObjectIDForCharacter(c), kind: sookFirePitVisual, book: 1, at: run.firePitAt}), sookBanner("Fire Pit: 30m burns for 60s. Non-uniques lose 3% maximum HP each second."))
	case sookAmbush:
		alive := 0
		for _, actor := range rt.Monsters.PopulationInstances(division, run.lease, []uint16{run.center.RegionID}) {
			if actor.CurrentHP > 0 {
				alive++
			}
		}
		spawned := 0
		refs := rt.sookArena.pool.StageNormals(int(rewardLevel(c)), run.program.Completed)
		if len(refs) > 0 {
			for i := 0; i < min(10, max(0, sookarena.MaxAlive-alive)); i++ {
				if rt.spawnSookMonster(run, refs[run.sequence%len(refs)], sookarena.WaveRarity(run.sequence), now) {
					spawned++
				}
				run.sequence++
			}
		}
		result.Frames = append(result.Frames, sookBanner(fmt.Sprintf("Bad Egg: ambush! %d extra monsters appeared.", spawned)))
	case sookAlarm:
		for _, actor := range rt.Monsters.PopulationInstances(division, run.lease, []uint16{run.center.RegionID}) {
			if actor.CurrentHP > 0 {
				rt.commitAggression(division, actor.Gid, simulation.HostilityEvent{Attacker: enterworld.ObjectIDForCharacter(c), Aggression: 1}, now)
			}
		}
		result.Frames = append(result.Frames, sookBanner("Bad Egg: every living monster in your arena is hunting you!"))
	case sookSpeed:
		run.program.SpeedUntil = now + 30000
		speed := rt.refreshMovementEffects(division, c, now)
		result.Frames = append(result.Frames, speed...)
		result.Broadcast = append(result.Broadcast, speed...)
		result.Frames = append(result.Frames, sookBanner("Fleetfoot: +200% movement speed for 30 seconds."))
	case sookTransfer:
		run.program.TransferUntil = now + 30000
		result.Frames = append(result.Frames, sookBanner("Chain Reaction: attacks and skills copy damage to up to 10 enemies within 10m for 30s."))
	case sookReflect:
		run.program.ReflectUntil = now + 30000
		result.Frames = append(result.Frames, sookBanner("Mirror Shell: return 500% of HP damage taken to the attacker for 30s. You still take damage."))
	case sookUnstoppable:
		run.program.UnstoppableUntil = now + 60000
		rt.playerDisplacements.Delete(key)
		var cleared *playerAbnormalOwner
		rt.deps.UpdateDeferred(c, "sook-unstoppable", func() bool {
			cleared = rt.clearPlayerAbnormalInDoor(division, c, now)
			return true
		})
		if cleared != nil {
			publication := rt.playerAbnormalPublication(division, c, cleared)
			result.Frames = append(result.Frames, publication.actor...)
			result.Broadcast = append(result.Broadcast, publication.public...)
		}
		result.Frames = append(result.Frames, sookBanner("Unstoppable: abnormal-state and knockback immunity for 60 seconds."))
	case sookInvincible:
		run.program.InvincibleUntil = now + 30000
		result.Frames = append(result.Frames, sookBanner("Unkillable: incoming damage cannot take your last HP for 30 seconds."))
	case sookMana:
		run.program.ManaUntil = now + 60000
		_, maxMP, _, _ := rt.playerKeeperVitals(division, c)
		rt.deps.UpdateDeferred(c, "sook-mana", func() bool { c.CurrentMP = &maxMP; return true })
		result.Frames = append(result.Frames, rt.gaugeDropFrames(division, c, false, true, false)...)
		result.Frames = append(result.Frames, sookBanner("Arcane Spring: skills consume no MP for 60 seconds."))
	}
	if duration := sookPowerDuration(item.ArenaPowerup); duration > 0 {
		result.Frames = append(result.Frames, wire.Frame{Opcode: wire.OpParamJobStart, Payload: wire.EncodeParamJobRow(enterworld.ObjectIDForCharacter(c), duration, sookPowerRefBase|uint32(item.ArenaPowerup))})
	}
	return result
}

/*
================
sookPowerDuration - native param-job messages carry arena-only HUD timers.
================
*/
func sookPowerDuration(kind uint8) uint32 {
	switch kind {
	case sookDamage, sookReflect, sookInvincible, sookTransfer, sookSpeed:
		return 30
	case sookUnstoppable, sookMana, sookFirePit:
		return 60
	}
	return 0
}

/*
================
sookPowerEnds - retire only this run's virtual HUD rows on expiry or exit.
================
*/
func sookPowerEnds(run *sookArenaRun, now int64, all bool) []wire.Frame {
	var frames []wire.Frame
	for _, row := range []struct {
		kind  uint8
		until *int64
	}{
		{sookDamage, &run.program.DamageUntil}, {sookUnstoppable, &run.program.UnstoppableUntil},
		{sookInvincible, &run.program.InvincibleUntil}, {sookMana, &run.program.ManaUntil},
		{sookFirePit, &run.firePitUntil},
		{sookReflect, &run.program.ReflectUntil}, {sookTransfer, &run.program.TransferUntil}, {sookSpeed, &run.program.SpeedUntil},
	} {
		if *row.until != 0 && (all || now >= *row.until) {
			*row.until = 0
			frames = append(frames, wire.Frame{Opcode: wire.OpParamJobEnd, Payload: wire.EncodeParamJobEnd(simulation.PlayerObjectID(run.ownerID), sookPowerRefBase|uint32(row.kind))})
		}
	}
	return frames
}

/*
================
sookPowerActive - private run deadlines never survive leaving or lease reuse.
================
*/
func (rt *Runtime) sookPowerActive(division string, c *enterworld.Character, kind uint8, now int64) bool {
	run := rt.sookRun(division, c)
	if run == nil {
		return false
	}
	switch kind {
	case sookSpeed:
		return now < run.program.SpeedUntil
	case sookTransfer:
		return run.program.PausedAt == 0 && now < run.program.TransferUntil
	case sookReflect:
		return now < run.program.ReflectUntil
	case sookUnstoppable:
		return now < run.program.UnstoppableUntil
	case sookInvincible:
		return now < run.program.InvincibleUntil
	case sookMana:
		return now < run.program.ManaUntil
	}
	return false
}

/*
================
sookManaConsumption - waive MP only, preserving data, HP and cooldown checks.
================
*/
func (rt *Runtime) sookManaConsumption(division string, c *enterworld.Character, cost enterworld.SkillConsumption, now int64) enterworld.SkillConsumption {
	if rt.sookPowerActive(division, c, sookMana, now) {
		cost.MP, cost.MPPercent = 0, 0
	}
	return cost
}

/*
================
detonateSookBomb - ordinary credited hits settle loot, EXP and kill feedback.
================
*/
func (rt *Runtime) detonateSookBomb(run *sookArenaRun, c *enterworld.Character, at simulation.Spawn, now int64) {
	snapshot := rt.characterSnapshotCombat(run.division, c)
	if snapshot == nil {
		return
	}
	for _, target := range rt.Monsters.CombatCandidatesInPopulation(run.division, run.lease, at, 150, now, true) {
		if target.CurrentHP == 0 {
			continue
		}
		damage := target.CurrentHP
		if target.Ref.MonsterType == 3 {
			damage = max(uint32(1), target.EffectiveMaxHP()/4)
		}
		hit, ok := rt.commitCreditedMonsterHit(run.division, c, snapshot, enterworld.SkillRow{}, target, combat.Result{Damage: damage, ResultFlags: 1}, "sook-shockwave", now)
		if !ok {
			continue
		}
		impact := hit.impacts[0]
		out := rt.creditedHitResult(run.division, target, hit, []wire.Frame{wire.SkillDamageTickFrame(enterworld.ObjectIDForCharacter(c), impact.Fatal, target.Gid, []uint32{impact.Damage})}, now)
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: run.division, SourceGID: target.Gid, Frames: simFrames(out.Broadcast)}, {DivisionID: run.division, OnlyCharacterID: c.ID, Frames: simFrames(out.ActorPrivate)}})
		rt.queueMonsterLegFrames(recipientDivisionFrames(run.division, out.Recipients))
	}
}

/*
================
tickSookFirePit - port-only fixed-origin field; refresh replaces this run's pit.
================
*/
func (rt *Runtime) tickSookFirePit(run *sookArenaRun, c *enterworld.Character, now int64) {
	if run.firePitNext == 0 || now < run.firePitNext {
		return
	}
	// Settle the final second at exactly 60s, never damage after the field expired.
	if now > run.firePitUntil {
		run.firePitNext = 0
		return
	}
	run.firePitNext = now + 1000
	snapshot := rt.characterSnapshotCombat(run.division, c)
	if snapshot == nil {
		return
	}
	for _, target := range rt.Monsters.CombatCandidatesInPopulation(run.division, run.lease, run.firePitAt, 300, now, true) {
		if target.CurrentHP == 0 || target.Ref.MonsterType == 3 {
			continue
		}
		damage := max(uint32(1), uint32(uint64(target.EffectiveMaxHP())*3/100))
		hit, ok := rt.commitCreditedMonsterHit(run.division, c, snapshot, enterworld.SkillRow{}, target, combat.Result{Damage: damage, ResultFlags: 1}, "sook-fire-pit", now)
		if !ok {
			continue
		}
		impact := hit.impacts[0]
		out := rt.creditedHitResult(run.division, target, hit, []wire.Frame{wire.SkillDamageTickFrame(enterworld.ObjectIDForCharacter(c), impact.Fatal, target.Gid, []uint32{impact.Damage})}, now)
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: run.division, OnlyCharacterID: c.ID, Frames: simFrames(out.Broadcast)}, {DivisionID: run.division, OnlyCharacterID: c.ID, Frames: simFrames(out.ActorPrivate)}})
		rt.queueMonsterLegFrames(recipientDivisionFrames(run.division, out.Recipients))
	}
}

/*
================
sookDamageBoost - saturate the wire's native damage field.
================
*/
func (rt *Runtime) sookDamageBoost(division, name string, result combat.Result, now int64) combat.Result {
	if rt.sookArena == nil {
		return result
	}
	value, ok := rt.sookArena.runs.Load(simulation.WorldKey(division, name))
	if !ok {
		return result
	}
	if run := value.(*sookArenaRun); now < run.program.DamageUntil && rt.sookRun(division, rt.findCharacter(division, name)) == run {
		factor := uint64(max(uint32(2), min(uint32(10), run.program.DamageMultiplier)))
		result.Damage = uint32(min(uint64(wire.MaxSkillActionDamage), uint64(result.Damage)*factor))
		result.PhysicalDamage = uint32(min(uint64(math.MaxUint32), uint64(result.PhysicalDamage)*factor))
		result.MagicalDamage = uint32(min(uint64(math.MaxUint32), uint64(result.MagicalDamage)*factor))
	}
	return result
}

/*
================
sookReward - the caller has already checked the fatal impact's exact lease.
================
*/
func sookReward(value int64, run *sookArenaRun) int64 {
	if run != nil {
		return sookarena.Fivefold(value)
	}
	return value
}

/*
================
sookPowerResume - resynchronize the native HUD with paused arena deadlines.
================
*/
func sookPowerResume(run *sookArenaRun, now int64) []wire.Frame {
	var frames []wire.Frame
	for _, row := range []struct {
		kind  uint8
		until int64
	}{
		{sookDamage, run.program.DamageUntil}, {sookUnstoppable, run.program.UnstoppableUntil},
		{sookInvincible, run.program.InvincibleUntil}, {sookMana, run.program.ManaUntil},
		{sookFirePit, run.firePitUntil}, {sookReflect, run.program.ReflectUntil}, {sookTransfer, run.program.TransferUntil}, {sookSpeed, run.program.SpeedUntil},
	} {
		if row.until > now {
			frames = append(frames, wire.Frame{Opcode: wire.OpParamJobStart, Payload: wire.EncodeParamJobRow(simulation.PlayerObjectID(run.ownerID), uint32((row.until-now+999)/1000), sookPowerRefBase|uint32(row.kind))})
		}
	}
	return frames
}
