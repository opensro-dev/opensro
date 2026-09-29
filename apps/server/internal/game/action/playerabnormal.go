/*
===========================================================================

playerabnormal.go - abnormal states on players and their publication

===========================================================================
*/

package action

import (
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/internal/vitals"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
playerAbnormalStore

playerAbnormalStore owns every character's abnormal-state block (the
CGObjChar block at +D30 of a CGObjPC). Stored blocks are immutable
snapshots; every change commits a fresh copy under the character door.
==================
*/
type playerAbnormalStore struct {
	mu     sync.Mutex
	blocks map[string]playerAbnormalEntry
}

type playerAbnormalEntry struct {
	division string
	name     string
	block    *abnormal.Block
}

func playerAbnormalKey(division, name string) string {
	return strings.ToLower(division) + "\x00" + strings.ToLower(name)
}

// playerAbnormal returns the character's committed block, or nil.
func (rt *Runtime) playerAbnormal(division, name string) *abnormal.Block {
	rt.playerAbnormals.mu.Lock()
	defer rt.playerAbnormals.mu.Unlock()
	return rt.playerAbnormals.blocks[playerAbnormalKey(division, name)].block
}

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

func (rt *Runtime) playerAbnormalCandidates() []playerAbnormalEntry {
	rt.playerAbnormals.mu.Lock()
	defer rt.playerAbnormals.mu.Unlock()
	out := make([]playerAbnormalEntry, 0, len(rt.playerAbnormals.blocks))
	for _, entry := range rt.playerAbnormals.blocks {
		out = append(out, entry)
	}
	return out
}

// PlayerMovementBlocked is 4B0EA0's gate: a ground command is dropped while
// the mover is frozen, asleep, rooted or stunned (mask C1 | 4000).
func (rt *Runtime) PlayerMovementBlocked(division, name string) bool {
	block := rt.playerAbnormal(division, name)
	return block != nil && block.Mask&(abnormal.Freeze.Bit()|abnormal.Sleep.Bit()|abnormal.Root.Bit()|abnormal.Stun.Bit()) != 0
}

/*
==================
playerCastBlocked

playerCastBlocked is 58D8F0's pre-engage abnormal gate (0x3009): freeze,
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

	maskBefore   uint32
	changed      bool
	statsChanged bool
	speedChanged bool
	cancel       bool
	halted       bool
	hpChanged    bool
	mpChanged    bool
	fatal        bool
	hits         []abnormalPlayerHit
	detonations  []abnormal.Slot
	deathEffects []wire.Frame
	deathTarget  []wire.Frame
}

type abnormalPlayerHit struct {
	source   uint32
	credited bool
	damage   uint32
	reason   uint8
}

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

func (o *playerAbnormalOwner) Alive() bool     { return !o.fatal && enterworld.CharacterAlive(o.c) }
func (o *playerAbnormalOwner) IsPlayer() bool  { return true }
func (o *playerAbnormalOwner) IsMonster() bool { return false }
func (o *playerAbnormalOwner) CurrentHP() uint32 {
	return uint32(clampKeeperVital(o.c.CurrentHP, int64(o.MaxHP())))
}

// MaxHP/MaxMP read the keeper with the working block: 4A5320/4A54A0 apply
// their factor to param 3/4 before sizing the drain from the new maximum.
func (o *playerAbnormalOwner) MaxHP() uint32 { return o.keeperMax(3, enterworld.DerivedMaxHP(o.c)) }
func (o *playerAbnormalOwner) MaxMP() uint32 { return o.keeperMax(4, enterworld.DerivedMaxMP(o.c)) }

func (o *playerAbnormalOwner) keeperMax(param uint16, derived int64) uint32 {
	if v := o.Param(param); v > 0 {
		return uint32(v)
	}
	return uint32(derived)
}

// Param reads the live keeper with the working block's own writes.
func (o *playerAbnormalOwner) Param(id uint16) float32 {
	stats, _, err := combat.PlayerStatsWithModifiers(o.c, o.rt.statCatalogs(), o.rt.effects.ModifierWrites(o.division, o.c.Name), o.block)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"division": o.division, "character": o.c.Name, "param": id}).Error("abnormal parameter projection failed")
		return 0
	}
	v, _ := stats.Param(id)
	return v
}

func (o *playerAbnormalOwner) SourceExists(gid uint32) bool {
	return o.rt.abnormalSourceExists(o.division, gid)
}

func (o *playerAbnormalOwner) SourceDead(gid uint32) bool {
	return o.rt.abnormalSourceDead(o.division, gid)
}

func (o *playerAbnormalOwner) Roll(key uint32, chance int32) bool {
	if chance <= 0 {
		return false
	}
	proc, err := o.rt.effectOutcome(criticalActor{division: o.division, character: o.c.Name}, key, uint32(chance))
	return err == nil && proc
}

func (o *playerAbnormalOwner) Now() int64 { return o.now }

func (o *playerAbnormalOwner) ParamsChanged(speed bool) {
	o.statsChanged = true
	o.speedChanged = o.speedChanged || speed
}

/*
==================
SetMotion

SetMotion has no v1.150 player carrier: 0x3122 publishes state channels
only, and the client derives the frozen/asleep/stunned pose from the mask.
The command gates read the mask directly.
==================
*/
func (o *playerAbnormalOwner) SetMotion(uint8, uint8, float32) {}

func (o *playerAbnormalOwner) CancelActions(bool) { o.cancel = true }

// StopMove settles the mover at its live point on its retained cell (+4B0).
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

// AIEvent is reached only for monster owners (4A4BD0 / 4A4F70 check +28).
func (o *playerAbnormalOwner) AIEvent(uint8, uint8, uint32) {}

// Hit is vfunc 4FC on the player: the debit commits on the live record, and a
// lethal tick settles death in the same door.
func (o *playerAbnormalOwner) Hit(source uint32, credited bool, damage uint32, reason uint8, _ abnormal.Status) {
	if o.fatal || damage == 0 {
		return
	}
	remaining := int64(o.CurrentHP())
	debit := int64(vitals.HitDebit(uint32(remaining), damage))
	remaining -= debit
	o.c.CurrentHP = &remaining
	o.hpChanged = true
	o.hits = append(o.hits, abnormalPlayerHit{source: source, credited: credited, damage: uint32(debit), reason: reason})
	if remaining == 0 {
		o.fatal = true
		o.deathEffects, o.deathTarget = o.rt.settlePlayerDeathInDoor(o.division, o.c, o.now)
	}
}

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

// Detonate is 59B300: the bomb's authored damage lands as a reason-1 hit.
func (o *playerAbnormalOwner) Detonate(slot abnormal.Slot) {
	o.detonations = append(o.detonations, slot)
	o.Hit(slot.SourceGID, true, slot.Damage1C, 1, abnormal.TimeBomb)
}

// commit stores the working block; death clears it (4A59F0).
func (o *playerAbnormalOwner) commit() {
	if o.fatal {
		o.block.ClearAll(o)
	}
	o.changed = o.changed || o.block.Mask != o.maskBefore
	o.rt.storePlayerAbnormal(o.division, o.c.Name, o.block)
}

func (rt *Runtime) abnormalSourceExists(division string, gid uint32) bool {
	if gid == 0 {
		return false
	}
	if rt.Monsters != nil {
		if _, ok := rt.Monsters.Get(division, gid); ok {
			return true
		}
	}
	return rt.findCharacterByGid(division, gid) != nil
}

func (rt *Runtime) abnormalSourceDead(division string, gid uint32) bool {
	if rt.Monsters != nil {
		if instance, ok := rt.Monsters.Get(division, gid); ok {
			return instance.CurrentHP == 0
		}
	}
	if c := rt.findCharacterByGid(division, gid); c != nil {
		snapshot := rt.characterSnapshot(division, c)
		return snapshot == nil || !enterworld.CharacterAlive(snapshot)
	}
	return false
}

// applyPlayerAbnormalInDoor runs a surviving victim's hit consequences inside
// the HP commit door: the damage breaks first, then the rolled records.
func (rt *Runtime) applyPlayerAbnormalInDoor(division string, c *enterworld.Character, damaged bool, records []abnormal.Record, now int64) *playerAbnormalOwner {
	if !damaged && len(records) == 0 {
		return nil
	}
	o := rt.newPlayerAbnormalOwner(division, c, now)
	if damaged && o.block.Mask != 0 {
		o.changed = o.block.BreakOnHit(o) || o.changed
	}
	for _, record := range records {
		if o.block.Apply(o, record, now) {
			o.changed = true
		}
	}
	o.commit()
	if o.statsChanged {
		hp, mp := rt.clampStoredGaugeToKeeper(division, c)
		o.hpChanged = o.hpChanged || hp
		o.mpChanged = o.mpChanged || mp
	}
	return o
}

// clearPlayerAbnormalInDoor is 4A59F0 for a death committed by another owner.
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
func (rt *Runtime) settlePlayerDeathInDoor(division string, c *enterworld.Character, now int64) (effects, progression []wire.Frame) {
	state := rt.Worlds.Update(simulation.WorldKey(division, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(world *simulation.WorldState) { world.SettleDeath(now) })
	writeBackWorld(c, state)
	// Also retire the legacy persisted segment echo. A reconnect must seed
	// the corpse, not the destination of its last living movement.
	c.World.MoveSegment = nil
	effects = rt.retireBodyEffectsOnDeath(division, c)
	// CGObjPC_ProcessNormalDeath leaves battle (529C93) before the life change.
	effects = append(effects, rt.leaveBattleState(division, c, now)...)
	if rt.ApplyDeathPenalty != nil {
		// This updater is explicitly door-free. Keeping it inside the fatal
		// HP closure makes corpse state and any level>10 EXP loss one durable
		// transition; packet routing happens after the door closes.
		progression, _ = rt.ApplyDeathPenalty(c)
	}
	if rt.ReleaseQuestCapturesOnDeath != nil {
		frames, _ := rt.ReleaseQuestCapturesOnDeath(c)
		progression = append(progression, frames...)
	}
	return effects, progression
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
	public  []wire.Frame
	actor   []wire.Frame
	sources []privateFrames
}

/*
==================
playerAbnormalPublication

playerAbnormalPublication follows a committed owner: withdrawn casts (570),
the stop (4B0), the speed pair (4AA410), the private stat block (303D), the
victim's 0x36C7 snapshot (server 30D2, 4A5C60) and the shared vitals mask
(dirty bit 0x100 -> 33A6).
==================
*/
func (rt *Runtime) playerAbnormalPublication(division string, c *enterworld.Character, o *playerAbnormalOwner) playerAbnormalFrames {
	var out playerAbnormalFrames
	if o == nil {
		return out
	}
	gid := enterworld.ObjectIDForCharacter(c)
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
	}
	if o.statsChanged {
		if stats, err := rt.PlayerBaseStats(division, c); err == nil {
			out.actor = append(out.actor, wire.Frame{Opcode: wire.OpBaseStats, Payload: stats.Encode()})
		} else {
			log.WithError(err).WithFields(log.Fields{"division": division, "character": c.Name}).Error("abnormal stat projection failed")
		}
	}
	for _, hit := range o.hits {
		// 52A33D's 3058 echo, v1.150 3128 (gid, raw damage), to a credited
		// player source of a damage-over-time tick.
		if !hit.credited || hit.reason != 2 {
			continue
		}
		if source := rt.findCharacterByGid(division, hit.source); source != nil {
			out.sources = append(out.sources, privateFrames{source.ID, []wire.Frame{{Opcode: 0x3128, Payload: wire.NewWriter(8).U32(gid).U32(hit.damage).Payload()}}})
		}
	}
	for _, slot := range o.detonations {
		out.public = append(out.public, timeBombFrame(gid, slot.Damage1C, o.fatal))
	}
	if o.hpChanged || o.mpChanged {
		values := rt.publishedVitals(division, c)
		out.actor = append(out.actor, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceFlags(2), values)})
		if o.hpChanged {
			out.public = append(out.public, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(gid, simulation.VitalsSourceFlags(2), values.CurrentHP)})
		}
	}
	if o.fatal {
		out.public = append(out.public, o.deathEffects...)
		lifePublication := beginFatalLifePublication(gid)
		baseline := lifePublication.publishDeathBaseline()
		death := lifePublication.publishDead()
		out.public = append(out.public, wire.Frame{Opcode: baseline.opcode, Payload: baseline.payload}, wire.Frame{Opcode: death.opcode, Payload: death.payload})
		out.actor = append(out.actor, o.deathTarget...)
	}
	if o.changed {
		block := rt.playerAbnormal(division, c.Name)
		out.actor = append(out.actor, wire.Frame{Opcode: 0x36C7, Payload: playerAbnormalSnapshotPayload(block, o.now)})
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
Server 4A5C60 truncates both divisions under RC=chop.
==================
*/
func playerAbnormalSnapshotPayload(block *abnormal.Block, now int64) []byte {
	w := wire.NewWriter(4)
	if block == nil {
		return w.U32(0).Payload()
	}
	w.U32(block.Mask)
	for bit := 0; bit < 32; bit++ {
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
		case value&0x203f != 0:
			level = uint8(slot.Level)
		case value&abnormal.GradeMask != 0:
			level = slot.Grade
		}
		w.U16(uint16(slot.DurationMs / 100)).U16(uint16(elapsed / 100)).U8(level)
	}
	return w.Payload()
}

// abnormalVitalsPayload is the shared 33A6 abnormal channel (flags 4): the
// mask, then the grade bytes of 017FCFC0 in ascending bit order.
func abnormalVitalsPayload(gid uint32, block *abnormal.Block) []byte {
	var mask uint32
	var grades []uint8
	if block != nil {
		mask, grades = block.Mask, block.Grades()
	}
	w := wire.NewWriter(12).U32(gid).U16(0x100).U8(4).U32(mask)
	for _, g := range grades {
		w.U8(g)
	}
	return w.Payload()
}

/*
==================
rollMonsterOnPlayer

rollMonsterOnPlayer ports 590680 for a monster's hit on a player: the
victim's keeper supplies element resists (1B..20), flat reductions (91..96,
which reat passives raise) and the disease bonus (A9); its learned real
passives supply the status-resistance buckets (59DE50).
==================
*/
func (rt *Runtime) rollMonsterOnPlayer(division string, instance monster.Instance, params *abnormal.SkillParams, target *enterworld.Character, defender combat.Stats, blocked bool) ([]abnormal.Record, error) {
	if params == nil || !params.Present() {
		return nil, nil
	}
	param := func(id uint16) float32 {
		v, _ := defender.Param(id)
		return v
	}
	in := abnormal.RollInput{
		Params:      params,
		Blocked:     blocked,
		TargetLevel: defender.Level,
		TargetBonus: param(0xa9),
		CasterLevel: instance.Ref.Level,
		SourceGID:   instance.Gid,
		TargetGID:   enterworld.ObjectIDForCharacter(target),
		Resistance:  defender.StatusResistance,
	}
	for i := range in.TargetResist {
		// Roll order fz fb es bu ps zb reads 1B 1C 1E 1D 1F 20.
		in.TargetResist[i] = param(0x1b + [...]uint16{0, 1, 3, 2, 4, 5}[i])
		in.TargetFlat[i] = param(0x91 + uint16(i))
	}
	random := &abnormalRandom{rt: rt, actor: criticalActor{division: division, monster: instance.Gid}}
	records := abnormal.Roll(in, random)
	return records, random.err
}

// advancePlayerAbnormals runs 4A4390 for every character with a block.
func (rt *Runtime) advancePlayerAbnormals(now int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, entry := range rt.playerAbnormalCandidates() {
		out = append(out, rt.advancePlayerAbnormal(entry.division, entry.name, now)...)
	}
	return out
}

func (rt *Runtime) advancePlayerAbnormal(division, name string, now int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		rt.storePlayerAbnormal(division, name, nil)
		return nil
	}
	var o *playerAbnormalOwner
	committed := rt.deps.Update(c, "player-abnormal", func() bool {
		if c.DeletePending {
			return false
		}
		o = rt.newPlayerAbnormalOwner(division, c, now)
		if !enterworld.CharacterAlive(c) {
			o.changed = o.block.ClearAll(o)
		} else if result := o.block.Update(o, now); result.Changed {
			o.changed = true
		}
		o.commit()
		if o.statsChanged {
			hp, mp := rt.clampStoredGaugeToKeeper(division, c)
			o.hpChanged = o.hpChanged || hp
			o.mpChanged = o.mpChanged || mp
		}
		return o.changed || o.hpChanged || o.mpChanged || o.statsChanged || o.halted
	})
	if !committed || o == nil {
		return nil
	}
	if o.fatal {
		rt.bindResidentRegion(simulation.WorldKey(division, c.Name), now)
	}
	frames := rt.playerAbnormalPublication(division, c, o)
	return playerAbnormalDivisionFrames(division, c.ID, frames)
}

func playerAbnormalDivisionFrames(division string, actorID int64, frames playerAbnormalFrames) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	convert := func(in []wire.Frame) []simulation.Frame {
		fs := make([]simulation.Frame, 0, len(in))
		for _, f := range in {
			fs = append(fs, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		return fs
	}
	if len(frames.public) > 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: division, Frames: convert(frames.public)})
	}
	if len(frames.actor) > 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: actorID, Frames: convert(frames.actor)})
	}
	for _, p := range frames.sources {
		out = append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: p.id, Frames: convert(p.frames)})
	}
	return out
}

// forgetPlayerAbnormalSource detaches a departing character as a caster from
// every block, and drops its own block with the actor.
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
