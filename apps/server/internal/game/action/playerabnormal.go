/*
===========================================================================

playerabnormal.go - abnormal states on players and their publication

The division owner admits source facts before the authority transaction.
Callbacks mutate the target and collect publications without store lookups;
network publication follows the committed character state.

===========================================================================
*/

package action

import (
	"math"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/internal/vitals"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	abnormalMaxHPParam           = 3
	abnormalMaxMPParam           = 4
	abnormalDiseaseBonusParam    = 0xa9
	abnormalElementResistBase    = 0x1b
	abnormalFlatResistanceBase   = 0x91
	abnormalLevelMask            = 0x203f
	abnormalMaskBits             = 32
	abnormalWireTimeUnit         = 100
	abnormalSnapshotOpcode       = 0x36c7
	abnormalDamageCreditOpcode   = 0x3128
	abnormalDamageCreditBytes    = 8
	abnormalSnapshotHeaderBytes  = 4
	abnormalVitalsHeaderBytes    = 12
	abnormalVitalsDirtyMask      = 0x100
	abnormalVitalsChannel        = 4
	abnormalDamageOverTimeReason = 2
	abnormalBombReason           = 1
	abnormalStatusVitalsSource   = 2
)

/*
==================
playerAbnormalStore

playerAbnormalStore owns every character's abnormal-state block (the
character block at +D30 of a player object). Stored blocks are immutable
snapshots; every change commits a fresh copy under the character door.
==================
*/
type playerAbnormalStore struct {
	mu     sync.Mutex
	blocks map[string]playerAbnormalEntry
}

/*
================
playerAbnormalEntry

An immutable block belongs to one named character in one division.
================
*/
type playerAbnormalEntry struct {
	division string
	name     string
	block    *abnormal.Block
}

/*
================
playerAbnormalKey

Match the authority's case-insensitive character identity across divisions.
================
*/
func playerAbnormalKey(division, name string) string {
	return strings.ToLower(division) + "\x00" + strings.ToLower(name)
}

/*
================
playerAbnormal

Return the committed snapshot. Callers copy it before changing any slot.
================
*/
func (rt *Runtime) playerAbnormal(division, name string) *abnormal.Block {
	rt.playerAbnormals.mu.Lock()
	defer rt.playerAbnormals.mu.Unlock()
	return rt.playerAbnormals.blocks[playerAbnormalKey(division, name)].block
}

/*
================
storePlayerAbnormal

Replace a character's committed block and retire empty blocks from tick work.
================
*/
func (rt *Runtime) storePlayerAbnormal(division, name string, block *abnormal.Block) {
	rt.playerAbnormals.mu.Lock()
	defer rt.playerAbnormals.mu.Unlock()
	key := playerAbnormalKey(division, name)
	if block == nil || block.Mask == 0 && !block.Active() {
		delete(rt.playerAbnormals.blocks, key)
		return
	}
	if rt.playerAbnormals.blocks == nil {
		rt.playerAbnormals.blocks = map[string]playerAbnormalEntry{}
	}
	rt.playerAbnormals.blocks[key] = playerAbnormalEntry{division: division, name: name, block: block}
}

/*
================
playerAbnormalCandidates

Release the registry lock before any tick enters the character transaction.
================
*/
func (rt *Runtime) playerAbnormalCandidates() []playerAbnormalEntry {
	rt.playerAbnormals.mu.Lock()
	defer rt.playerAbnormals.mu.Unlock()
	out := make([]playerAbnormalEntry, 0, len(rt.playerAbnormals.blocks))
	for _, entry := range rt.playerAbnormals.blocks {
		out = append(out, entry)
	}
	return out
}

/*
================
PlayerMovementBlocked

The reference server's move command drops ground commands while frozen, asleep, rooted or stunned.
================
*/
func (rt *Runtime) PlayerMovementBlocked(division, name string) bool {
	block := rt.playerAbnormal(division, name)
	if block != nil && block.Mask&(abnormal.Freeze.Bit()|abnormal.Sleep.Bit()|abnormal.Root.Bit()|abnormal.Stun.Bit()) != 0 {
		return true
	}
	petGID, mounted := rt.mountedCosGID(division, name)
	return mounted && rt.cosMovementBlockedFor(division, name, petGID)
}

/*
================
PlayerAttackLocked

The reference server's attack lock: true while the casting instance
at char+C08 is set ([skillComp+1D8], a skill action has not
yet released its positive-time step) or the standing-instance pointer at
char+C44 is set ([skillComp+214]: the stns +448 slot written
by the buff modifier pass and cleared on retirement). The predicate does not read char+C0C, the
protective-wall slot ([skillComp+1DC]) that the skill engage fills
and wall retirement clears; natively
the standing wall roots through motion state 11. This
port folds its wallStanding (char+C0C) in beside chainStageBlocked
(char+C08) so the same root holds on the lanes below.
The motion-change lock (vt+0x5A0) feeds it to
the move command, which drops both direct
ground moves and the auto-navigator pursuit's internal 0x7021 moves,
and to the normal-attack auto command:
a player cannot walk out of a cast or a standing wall.
================
*/
func (rt *Runtime) PlayerAttackLocked(division, name string) bool {
	return rt.chainStageBlocked(division, name) || rt.wallStanding(division, name)
}

/*
==================
playerCastBlocked

playerCastBlocked is the native pre-engage abnormal gate (0x3009): freeze,
sleep and stun (mask 4041) forbid every command. The 'nmf' waiver and the
MOB_RM_SEALSTONE exemption name no v1.150 player skill.
==================
*/
func (rt *Runtime) playerCastBlocked(division, name string) bool {
	block := rt.playerAbnormal(division, name)
	return block != nil && block.Mask&(abnormal.Freeze.Bit()|abnormal.Sleep.Bit()|abnormal.Stun.Bit()) != 0
}

/*
==================
playerAbnormalOwner

playerAbnormalOwner adapts a character to the abnormal callbacks. It runs
only inside the character's commit door, mutates the live record for HP/MP
and records every other consequence for publication after the door.
==================
*/
type playerAbnormalOwner struct {
	rt       *Runtime
	division string
	c        *enterworld.Character
	block    *abnormal.Block
	now      int64
	sources  map[uint32]abnormalSourceState
	bond     *petBondPlan

	maskBefore   uint32
	changed      bool
	statsChanged bool
	speedChanged bool
	cancel       bool
	halted       bool
	hpChanged    bool
	mpChanged    bool
	fatal        bool
	hits         []abnormalHit
	detonations  []abnormal.Slot
	deathEffects []wire.Frame
	deathTarget  []wire.Frame
	deathKiller  *enterworld.Character
	deathKill    playerKill
	warCombat    domain.GuildWarCombat
	endedEffects []statuseffect.Effect
	endedPublic  []wire.Frame
	endedActor   []wire.Frame
}

/*
================
newPlayerAbnormalOwner

Copy the committed block without reading other authority records. Source
facts for admission or ticking must be prepared before the write lock.
================
*/
func (rt *Runtime) newPlayerAbnormalOwner(division string, c *enterworld.Character, now int64) *playerAbnormalOwner {
	o := &playerAbnormalOwner{rt: rt, division: division, c: c, now: now}
	if current := rt.playerAbnormal(division, c.Name); current != nil {
		copied := *current
		o.block = &copied
	} else {
		o.block = &abnormal.Block{}
	}
	o.maskBefore = o.block.Mask
	return o
}

/*
================
Alive

Honor a lethal hit already applied earlier in this working block.
================
*/
func (o *playerAbnormalOwner) Alive() bool {
	return !o.fatal && enterworld.CharacterAlive(o.c)
}

/*
================
IsPlayer

Select the player callbacks and player-only status publication rules.
================
*/
func (o *playerAbnormalOwner) IsPlayer() bool { return true }

/*
================
IsMonster

Player statuses must not dispatch monster AI events.
================
*/
func (o *playerAbnormalOwner) IsMonster() bool { return false }

/*
================
CurrentHP

Clamp persisted HP to the keeper projected with this working status block.
================
*/
func (o *playerAbnormalOwner) CurrentHP() uint32 {
	return uint32(clampKeeperVital(o.c.CurrentHP, int64(o.MaxHP())))
}

/*
================
MaxHP

The native panic callback applies the working block's factor before computing its HP drain.
================
*/
func (o *playerAbnormalOwner) MaxHP() uint32 {
	return o.keeperMax(abnormalMaxHPParam, enterworld.DerivedMaxHP(o.c))
}

/*
================
MaxMP

The native combustion callback applies the working block's factor before computing its MP drain.
================
*/
func (o *playerAbnormalOwner) MaxMP() uint32 {
	return o.keeperMax(abnormalMaxMPParam, enterworld.DerivedMaxMP(o.c))
}

/*
================
keeperMax

Use the derived baseline only when the keeper supplies no positive maximum.
================
*/
func (o *playerAbnormalOwner) keeperMax(param uint16, derived int64) uint32 {
	if v := o.Param(param); v > 0 {
		return uint32(v)
	}
	return uint32(derived)
}

/*
================
Param

Project the live character with the working block's own modifier writes.
================
*/
func (o *playerAbnormalOwner) Param(id uint16) float32 {
	stats, _, err := combat.PlayerStatsWithModifiers(o.c, o.rt.statCatalogs(), o.rt.statModifierWrites(o.division, o.c), o.block)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"division": o.division, "character": o.c.Name, "param": id}).Error("abnormal parameter projection failed")
		return 0
	}
	v, _ := stats.Param(id)
	return v
}

/*
================
SourceExists

Read admitted values only; a source lookup here would reenter the store lock.
================
*/
func (o *playerAbnormalOwner) SourceExists(gid uint32) bool {
	if gid == enterworld.ObjectIDForCharacter(o.c) {
		return true
	}
	return o.sources[gid].exists
}

/*
================
SourceDead

Self-inflicted statuses observe HP changes already made in this transaction.
Other sources use the snapshot admitted before the write.
================
*/
func (o *playerAbnormalOwner) SourceDead(gid uint32) bool {
	if gid == enterworld.ObjectIDForCharacter(o.c) {
		return !o.Alive()
	}
	return o.sources[gid].dead
}

/*
================
Roll

Use the owning character's deterministic effect stream for positive chances.
================
*/
func (o *playerAbnormalOwner) Roll(key uint32, chance int32) bool {
	if chance <= 0 {
		return false
	}
	proc, err := o.rt.effectOutcome(criticalActor{division: o.division, character: o.c.Name}, key, uint32(chance))
	return err == nil && proc
}

/*
================
Now

Every callback in a committed tick shares the admitted simulation timestamp.
================
*/
func (o *playerAbnormalOwner) Now() int64 { return o.now }

/*
================
ParamsChanged

Coalesce keeper and speed publication until all callbacks finish.
================
*/
func (o *playerAbnormalOwner) ParamsChanged(speed bool) {
	o.statsChanged = true
	o.speedChanged = o.speedChanged || speed
}

/*
==================
SetMotion

No v1.150 wire carries a player's motion hold: 0x3122 publishes state
channels only, and the client derives the frozen/asleep/stunned pose from
the mask. The hold is kept on the world plane for gates that read the
native motion byte (state+0x2), such as the 74B5 mount door,
because it outlives the mask by the 1.5 s thaw after a freeze.
==================
*/
func (o *playerAbnormalOwner) SetMotion(state, next uint8, delay float32) {
	if o.rt.Worlds == nil {
		return
	}
	// Natively state zero falls back to a still-active freeze, stun or sleep
	// (the resting motion state), as the monster owner does.
	if state == simulation.MotionNone {
		switch {
		case o.block.Has(abnormal.Freeze):
			state = 0xa
		case o.block.Has(abnormal.Stun):
			state = 9
		case o.block.Has(abnormal.Sleep):
			state = 0x13
		}
	}
	hold := monster.MotionHold{}
	switch {
	case state == simulation.MotionNone:
	case next == 0xff:
		hold = monster.MotionHold{State: state, UntilMs: math.MaxInt64}
	default:
		// Only (0xA, 0, 1.5) reaches here: the thaw after a freeze.
		hold = monster.MotionHold{State: state, UntilMs: o.now + int64(float64(delay)*1000)}
	}
	o.rt.Worlds.Update(simulation.WorldKey(o.division, o.c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(o.c) },
		func(world *simulation.WorldState) { world.AbnormalMotion = hold })
}

/*
================
CancelActions

The native cancel stops the live mover before withdrawing commands. Settle the position
under the authority lock; publish cancellation and correction after commit.
================
*/
func (o *playerAbnormalOwner) CancelActions(all bool) {
	o.StopMove()
	o.endedEffects = append(o.endedEffects, o.rt.retireAbnormalSkills(o.division, o.c, all)...)
	o.cancel = true
}

/*
================
StopMove

Settle the mover at its live point on its retained cell (+4B0).
================
*/
func (o *playerAbnormalOwner) StopMove() {
	if o.rt.Worlds == nil {
		return
	}
	key := simulation.WorldKey(o.division, o.c.Name)
	moving := false
	state := o.rt.Worlds.Update(key,
		func() simulation.WorldState { return simulation.SeedWorldState(o.c) },
		func(world *simulation.WorldState) {
			moving = world.MoveSegment.Valid()
			if moving {
				world.SettleLive(o.now)
			}
		})
	if moving {
		writeBackWorld(o.c, state)
		o.halted = true
	}
}

/*
================
AIEvent

The native fear and confusion callbacks check the monster discriminator; players have no AI callback.
================
*/
func (o *playerAbnormalOwner) AIEvent(uint8, uint8, uint32) {}

/*
================
Hit

Vfunc 4FC commits the debit and a possible lethal transition in the same write.
================
*/
func (o *playerAbnormalOwner) Hit(source uint32, credited bool, damage uint32, reason uint8, _ abnormal.Status) {
	if !o.Alive() || damage == 0 || credited && source == enterworld.ObjectIDForCharacter(o.c) {
		return
	}
	remaining := int64(o.CurrentHP())
	if o.sources[source].killer.monster != nil && o.rt.petShieldRecipientAllowed(o.c) {
		damage = o.rt.absorbPetShield(o.division, enterworld.ObjectIDForCharacter(o.c), damage, o.now)
	}
	if o.sources[source].killer.monster != nil {
		damage = sharePetBond(o.bond, damage)
	}
	debit := int64(vitals.HitDebit(uint32(remaining), damage))
	remaining -= debit
	o.c.CurrentHP = &remaining
	o.hpChanged = true
	// The native hit path publishes the authored hit independently of the saturated debit.
	o.hits = append(o.hits, abnormalHit{source: source, credited: credited, damage: damage, reason: reason})
	if remaining == 0 {
		o.fatal = true
		killer := o.sources[source].killer
		if credited {
			o.deathKiller = killer.player
		}
		o.warCombat = o.rt.prepareGuildWarCombat(o.division, o.c, killer)
		o.deathKill = playerKill{kind: o.rt.deathKind(o.division, o.c, killer), victimLevel: rewardLevel(o.c)}
		if o.deathKiller != nil {
			// The native hit path calls the striker before the victim's death relief.
			o.deathKill = o.sources[source].kill.withVictim(o.deathKiller, o.c)
		}
		o.deathEffects, o.deathTarget = o.rt.settlePlayerDeathInDoor(o.division, o.c, o.sources[source].killer, o.now)
	}
}

/*
================
ConsumeResources

Resource drains preserve one HP; MP may reach zero without a death transition.
================
*/
func (o *playerAbnormalOwner) ConsumeResources(hp, mp int32, _ uint8) {
	if hp > 0 {
		current := int64(o.CurrentHP())
		next := max(current-int64(hp), 1)
		o.c.CurrentHP = &next
		o.hpChanged = true
	}
	if mp > 0 {
		current := clampKeeperVital(o.c.CurrentMP, int64(o.MaxMP()))
		next := max(current-int64(mp), 0)
		o.c.CurrentMP = &next
		o.mpChanged = true
	}
}

/*
================
Detonate

The native detonation applies the bomb's authored damage and defers its explosion frame.
================
*/
func (o *playerAbnormalOwner) Detonate(slot abnormal.Slot) {
	o.detonations = append(o.detonations, slot)
	o.Hit(slot.SourceGID, true, slot.Damage1C, abnormalBombReason, abnormal.TimeBomb)
}

/*
================
commit

Publish the working block into the registry; death clears it first.
================
*/
func (o *playerAbnormalOwner) commit() {
	if o.fatal {
		o.block.ClearAll(o)
	}
	o.changed = o.changed || o.block.Mask != o.maskBefore
	o.rt.storePlayerAbnormal(o.division, o.c.Name, o.block)
	if len(o.endedEffects) != 0 {
		public, actor := o.rt.finishEndedEffects(o.division, o.c, o.endedEffects, o.now)
		o.endedPublic = append(o.endedPublic, public...)
		o.endedActor = append(o.endedActor, actor...)
		o.endedEffects = nil
	}
}

/*
================
applyHit

Run a surviving victim's hit consequences under the authority write lock.
The caller prepared source facts before entering the transaction.
================
*/
func (o *playerAbnormalOwner) applyHit(hit abnormal.HitContext, records []abnormal.Record) {
	if o.block.Mask != 0 {
		o.changed = o.block.BreakOnHit(o, hit) || o.changed
	}
	for _, record := range records {
		if o.block.Apply(o, record, o.now) {
			o.changed = true
		}
	}
	o.commit()
	if o.statsChanged {
		hp, mp := o.rt.clampStoredGaugeToKeeper(o.division, o.c)
		o.hpChanged = o.hpChanged || hp
		o.mpChanged = o.mpChanged || mp
	}
}

/*
================
clearPlayerAbnormalInDoor

Apply the native abnormal clear when another gameplay owner commits the character's death.
================
*/
func (rt *Runtime) clearPlayerAbnormalInDoor(division string, c *enterworld.Character, now int64) *playerAbnormalOwner {
	if rt.playerAbnormal(division, c.Name) == nil {
		return nil
	}
	o := rt.newPlayerAbnormalOwner(division, c, now)
	o.changed = o.block.ClearAll(o)
	o.commit()
	return o
}

/*
==================
settlePlayerDeathInDoor

settlePlayerDeathInDoor is the shared fatal transition: the corpse settles
at its live point, body effects retire, and the death penalty commits in
the same door as the lethal HP.
==================
*/
func (rt *Runtime) settlePlayerDeathInDoor(division string, c *enterworld.Character, killer deathKiller, now int64) (effects, progression []wire.Frame) {
	rt.clearPotionRecovery(division, c.Name)
	state := rt.Worlds.Update(simulation.WorldKey(division, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(world *simulation.WorldState) { world.SettleDeath(now) })
	writeBackWorld(c, state)
	// Also retire the legacy persisted segment echo. A reconnect must seed
	// the corpse, not the destination of its last living movement.
	c.World.MoveSegment = nil
	// Natively the death records where the player died, the
	// reverse return's second destination.
	if point, ok := recordedPoint(c, state.Spawn); ok {
		c.World.LastDeathPoint = point
	}
	effects = rt.retireBodyEffectsOnDeath(division, c)
	// The native death writes the interaction state 0:
	// an open warehouse ends with its user's death.
	rt.endStorageInteraction(division, c.Name)
	// Natively the ridden vehicle is released with its rider's death.
	released, owned := rt.releaseRiddenVehicleInDoor(division, c, now)
	effects = append(effects, released...)
	// Natively a pending return or reverse-return cast is cancelled.
	effects = append(effects, rt.cancelReturnOnDeathInDoor(division, c)...)
	// The native death leaves battle before the life change.
	effects = append(effects, rt.leaveBattleState(division, c, now)...)
	// The death's cost (pkdeath.go) commits in the fatal HP closure, so the
	// corpse and its EXP loss, drop and PK relief are one durable
	// transition; packet routing happens after the door closes.
	cost := rt.settleDeathCostInDoor(division, c, killer, now)
	effects = append(effects, cost.public...)
	progression = cost.actor
	// Port-only, not native: any death clears the streak (mostwanted.go).
	wantedPrivate, wantedPublic := rt.clearMostWantedInDoor(division, c, "death", now)
	effects = append(effects, wantedPublic...)
	progression = append(progression, wantedPrivate...)
	if rt.ReleaseQuestCapturesOnDeath != nil {
		frames, _ := rt.ReleaseQuestCapturesOnDeath(c)
		progression = append(progression, frames...)
	}
	// Natively the death ends with the victim's private notice of its kind,
	// which labels the death box's rebirth button (wire/deathnotice.go).
	progression = append(progression, owned...)
	return effects, append(progression, wire.DeathNoticeFrame(rt.deathNoticeKind(c.DeathKind)))
}

/*
==================
playerAbnormalFrames

playerAbnormalFrames splits one committed owner's publication: frames every
observer sees, frames only the victim sees, and private damage echoes to
credited player sources.
==================
*/
type playerAbnormalFrames struct {
	sourceGID   uint32
	exceptActor bool
	public      []wire.Frame
	actor       []wire.Frame
	sources     []privateFrames
}

/*
==================
playerAbnormalPublication

playerAbnormalPublication follows a committed owner: withdrawn casts (570),
the stop (4B0), the speed pair, the private stat block (303D), the
victim's 0x36C7 snapshot (server 30D2) and the shared vitals mask
(dirty bit 0x100 -> 33A6).
==================
*/
func (rt *Runtime) playerAbnormalPublication(division string, c *enterworld.Character, o *playerAbnormalOwner) playerAbnormalFrames {
	var out playerAbnormalFrames
	if o == nil {
		return out
	}
	out.public = append(out.public, rt.commitPetBond(o.bond)...)
	out.public = append(out.public, o.endedPublic...)
	out.actor = append(out.actor, o.endedActor...)
	gid := enterworld.ObjectIDForCharacter(c)
	out.sourceGID = gid
	if o.cancel {
		rt.ClearCombatIntent(division, c.Name)
		finals := rt.cancelPreparingProjectile(division, c.Name)
		out.public = append(out.public, finals...)
	}
	if o.halted && rt.Worlds != nil {
		spawn := rt.Worlds.Snapshot(simulation.WorldKey(division, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }).Spawn
		pos := wire.Position{RegionID: spawn.RegionID, X: float32(spawn.X), Y: float32(spawn.Y), Z: float32(spawn.Z), Heading: spawn.Angle}
		out.public = append(out.public, wire.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: gid, Position: pos}.Encode()})
	}
	if o.speedChanged {
		out.public = append(out.public, rt.refreshMovementEffects(division, c, o.now)...)
		out.public = append(out.public, wire.ActionSpeedFrame(gid, o.Param(actionSpeedParameter)))
	}
	if o.statsChanged {
		if stats, err := rt.PlayerBaseStats(division, c); err == nil {
			out.actor = append(out.actor, wire.Frame{Opcode: wire.OpBaseStats, Payload: stats.Encode()})
		} else {
			log.WithError(err).WithFields(log.Fields{"division": division, "character": c.Name}).Error("abnormal stat projection failed")
		}
	}
	for _, hit := range o.hits {
		if hit.damage > 0 {
			out.public = append(out.public, rt.tailwindDamage(division, c, hit.source, o.now)...)
		}
		// The native hit path also sends the victim an echo, even when its source
		// vanished or died. The source's credit does not gate that packet.
		if hit.reason != abnormalDamageOverTimeReason {
			continue
		}
		frame := abnormalDamageFrame(gid, hit.damage)
		out.actor = append(out.actor, frame)
		if !hit.credited {
			continue
		}
		if source := rt.findCharacterByGid(division, hit.source); source != nil {
			out.public = append(out.public, rt.tailwindDamage(division, source, gid, o.now)...)
			out.sources = append(out.sources, privateFrames{source.ID, []wire.Frame{frame}})
		}
	}
	for _, slot := range o.detonations {
		out.public = append(out.public, timeBombFrame(gid, slot.Damage1C, o.fatal))
	}
	if o.hpChanged || o.mpChanged {
		values := rt.publishedVitals(division, c)
		out.actor = append(out.actor, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceFlags(abnormalStatusVitalsSource), values)})
		if o.hpChanged {
			out.public = append(out.public, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(gid, simulation.VitalsSourceFlags(abnormalStatusVitalsSource), values.CurrentHP)})
		}
	}
	if o.fatal {
		rt.recordFortressDeath(division, c, o.deathKiller, o.now)
		rt.publishGuildWarCombat(division, o.warCombat, o.now)
		if o.deathKiller != nil {
			killer := rt.findCharacterByGid(division, enterworld.ObjectIDForCharacter(o.deathKiller))
			if killer != nil {
				var actor, public []wire.Frame
				var after jobKillPayout
				if !rt.deps.Update(killer, "player-abnormal-kill", func() bool {
					actor, public, after = rt.payPlayerKillInDoor(division, killer, c, o.deathKill, o.now)
					// The pay is unconditional once the door holds the
					// killer: its bookkeeping (a counted Most Wanted kill
					// among it) has no refusal of its own, and an empty
					// frame list is not one - a counted kill whose side
					// produced no frames still commits, notices included.
					return true
				}) {
					// The door itself refused (no live killer record):
					// nothing of the killer's side committed, no notice.
					after.notices = nil
				}
				out.sources = append(out.sources, privateFrames{killer.ID, actor})
				out.public = append(out.public, public...)
				for _, recipient := range rt.payJobKillShares(after) {
					out.sources = append(out.sources, privateFrames{recipient.CharacterID, recipient.Frames})
				}
			}
		}
		out.public = append(out.public, o.deathEffects...)
		lifePublication := beginFatalLifePublication(gid)
		baseline := lifePublication.publishDeathBaseline()
		death := lifePublication.publishDead()
		out.public = append(out.public, wire.Frame{Opcode: baseline.opcode, Payload: baseline.payload}, wire.Frame{Opcode: death.opcode, Payload: death.payload})
		out.actor = append(out.actor, o.deathTarget...)
	}
	if o.changed {
		block := rt.playerAbnormal(division, c.Name)
		out.actor = append(out.actor, wire.Frame{Opcode: abnormalSnapshotOpcode, Payload: playerAbnormalSnapshotPayload(block, o.now)})
		out.public = append(out.public, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: abnormalVitalsPayload(gid, block)})
	}
	return out
}

/*
==================
playerAbnormalSnapshotPayload

playerAbnormalSnapshotPayload is the v1.150 0x36C7 body (client 77C110):
u32 mask, then per set bit low to high u16 duration/100, u16 elapsed/100
and one byte: the level for bits in 203F, the grade for bits in 017FCFC0.
The reference server truncates both divisions under RC=chop.
==================
*/
func playerAbnormalSnapshotPayload(block *abnormal.Block, now int64) []byte {
	w := wire.NewWriter(abnormalSnapshotHeaderBytes)
	if block == nil {
		return w.U32(0).Payload()
	}
	w.U32(block.Mask)
	for bit := 0; bit < abnormalMaskBits; bit++ {
		value := uint32(1) << bit
		if block.Mask&value == 0 {
			continue
		}
		var slot *abnormal.Slot
		for i := range block.Slots {
			if block.Slots[i].Active && abnormal.Status(i).Bit() == value {
				slot = &block.Slots[i]
				break
			}
		}
		if slot == nil {
			w.U16(0).U16(0).U8(0)
			continue
		}
		elapsed := uint32(now - slot.StartedAt)
		var level uint8
		switch {
		case value&abnormalLevelMask != 0:
			level = uint8(slot.Level)
		case value&abnormal.GradeMask != 0:
			level = slot.Grade
		}
		w.U16(uint16(slot.DurationMs / abnormalWireTimeUnit)).U16(uint16(elapsed / abnormalWireTimeUnit)).U8(level)
	}
	return w.Payload()
}

/*
================
abnormalVitalsPayload

The shared abnormal channel carries the mask, then grades in ascending bit order.
================
*/
func abnormalVitalsPayload(gid uint32, block *abnormal.Block) []byte {
	var mask uint32
	var grades []uint8
	if block != nil {
		mask, grades = block.Mask, block.Grades()
	}
	w := wire.NewWriter(abnormalVitalsHeaderBytes).U32(gid).U16(abnormalVitalsDirtyMask).U8(abnormalVitalsChannel).U32(mask)
	for _, g := range grades {
		w.U8(g)
	}
	return w.Payload()
}

/*
================
forgetPlayerAbnormalSource

Drop the departing actor's block and detach its identity from surviving slots.
================
*/
func (rt *Runtime) forgetPlayerAbnormalSource(division, name string, gid uint32) {
	rt.playerAbnormals.mu.Lock()
	defer rt.playerAbnormals.mu.Unlock()
	delete(rt.playerAbnormals.blocks, playerAbnormalKey(division, name))
	for key, entry := range rt.playerAbnormals.blocks {
		if !strings.EqualFold(entry.division, division) {
			continue
		}
		copied := *entry.block
		if copied.ForgetSource(gid, name) {
			entry.block = &copied
			rt.playerAbnormals.blocks[key] = entry
		}
	}
}
