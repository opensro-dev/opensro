/*
===========================================================================

activeeffect.go - installing and retiring character effects and modifiers

The registry owns instance lifetime, parameter contributions and durable job
checkpoints. Installation and retirement publish from that same state.

===========================================================================
*/

package action

import (
	"sync/atomic"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/simulation"
)

// Process-wide identity prevents an effect retired in an earlier runtime or
// actor lifetime from clearing a newer application with a reused wire token.
var bodyEffectSequence atomic.Uint64

/*
==================
retireBodyEffectsOnDeath

Called by the fatal-HP mutation, before life-state publication. The registry
removes these applications now, so a subsequent expiry cannot retire them
again or clear a reused token's new status.
==================
*/
func (rt *Runtime) retireBodyEffectsOnDeath(division string, c *enterworld.Character) []wire.Frame {
	var frames []wire.Frame
	if c.BerserkUntilMs != 0 || c.NativeBodyStatus == 1 {
		c.BerserkUntilMs = 0
		rt.berserkActors.Delete(simulation.WorldKey(division, c.Name))
		if c.NativeBodyStatus == 1 && c.TransitionBodyStatus(domain.BodyStatusTransition{}) {
			frames = append(frames, bodyStatusFrame(enterworld.ObjectIDForCharacter(c), 0))
		}
		frames = append(frames, rt.refreshMovementEffects(division, c, rt.Now().UnixMilli())...)
	}
	ended := rt.effects.RetireBodyStatusesOnDeath(division, c.Name)
	if len(ended) == 0 {
		return frames
	}
	tokens := make([]uint32, 0, len(ended))
	for _, effect := range ended {
		if effect.BodyStatusOwner != 0 && c.TransitionBodyStatus(domain.BodyStatusTransition{RetireOwner: effect.BodyStatusOwner}) {
			frames = append(frames, bodyStatusFrame(enterworld.ObjectIDForCharacter(c), 0))
		}
		tokens = append(tokens, effect.InstanceToken)
	}
	clearTransform(c, ended)
	frames = append(frames, rt.refreshMovementEffects(division, c, rt.Now().UnixMilli())...)
	payload, err := (wire.EndedEffectInstances{InstanceTokens: tokens}).Encode()
	if err == nil {
		frames = append(frames, wire.Frame{Opcode: wire.OpEndedEffectInstances, Payload: payload})
	}
	return frames
}

/*
==================
ApplyCharacterEffect

ApplyCharacterEffect is the checked producer boundary for server gameplay
systems that create a persistent character effect. The skill table owns
canonical group identity and the `nbuf` non-forced-stop protection; callers
may only supply the live source-descriptor override proven at
ActiveCharacterEffect_RequestStop+0x20.
==================
*/
func (rt *Runtime) ApplyCharacterEffect(
	divisionID, characterName string,
	skillID, instanceToken uint32,
	state statuseffect.State,
	sourceDescriptorAllowsVoluntaryStop bool,
) bool {
	return rt.ApplyCharacterEffectPresentation(divisionID, characterName, skillID, instanceToken, state, sourceDescriptorAllowsVoluntaryStop, EffectPresentation{Phase: 2}, rt.Now().UnixMilli())
}

/*
================
EffectPresentation

Presentation is distinct from registry lifecycle state. Native 59AF00
serializes +20/+24, while state eligibility comes from +0c.
================
*/
type EffectPresentation struct {
	ForcedTargetGID uint32
	Phase           uint8
	Rider           uint32
	// TransformRefObjID is the RefObj an msch 1 cast carries (context
	// +0x20, set by BeginIndirectSkill from a monster mask), or the model
	// of the player an msch 2 Duplicate copies, with that player's record
	// byte and worn slots (4F0320).
	TransformRefObjID  uint32
	TransformShape     uint8
	TransformEquipment [9]uint32
	// DefenseAddend is a recipient context's +0x34/+0x38 (58381F): the
	// caster's getv HLBP or HLSM value, added to defp's physical/magical.
	DefenseAddend [2]uint32
}

/*
================
ApplyCharacterEffectPresentation

Validate the live recipient before entering its single effect mutation door.
================
*/
func (rt *Runtime) ApplyCharacterEffectPresentation(divisionID, characterName string, skillID, instanceToken uint32, state statuseffect.State, canStop bool, presentation EffectPresentation, nowMs int64) bool {
	if rt == nil || rt.effects == nil || rt.deps == nil {
		return false
	}
	unlock := rt.lockDivision(divisionID)
	defer unlock()

	character := rt.findCharacter(divisionID, characterName)
	snapshot := rt.characterSnapshot(divisionID, character)
	if snapshot == nil || snapshot.DeletePending {
		return false
	}
	skills := rt.deps.SkillData()
	if skills == nil {
		return false
	}
	row, known := skills.SkillByID(skillID)
	if !known || row.Group == 0 {
		return false
	}
	if row.BodyStatus.Present && (!row.BodyStatus.Supported ||
		(row.BodyStatus.Value != 6 && row.BodyStatus.Value != 7) ||
		!enterworld.CharacterAlive(snapshot)) {
		return false
	}

	var frames []wire.Frame
	if !rt.deps.Update(character, "apply-character-effect", func() bool {
		var ok bool
		frames, ok = rt.commitCharacterEffect(divisionID, character, row, instanceToken, state, canStop, presentation, nowMs)
		return ok
	}) {
		return false
	}
	// Ordered enqueue under the division operation lock: no teardown can
	// overtake application. Hub delivery callbacks enqueue and never reenter.
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(divisionID, snapshot.Name, frames)
	}
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(divisionID, snapshot.Name, frames)
	}
	return true
}

/*
================
commitCharacterEffect

Caller holds the division and character update doors. Item use and direct
skills commit through this same effect owner, without nested transactions.
================
*/
func (rt *Runtime) commitCharacterEffect(divisionID string, character *enterworld.Character, row enterworld.SkillRow, instanceToken uint32, state statuseffect.State, canStop bool, presentation EffectPresentation, nowMs int64) ([]wire.Frame, bool) {
	return rt.commitCharacterEffectWithCheckpoint(divisionID, character, row, instanceToken, state, canStop, presentation, nowMs, true)
}

/*
================
commitCharacterEffectWithCheckpoint

Restoring a batch commits its durable records once, after every installation.
An individual installation must not overwrite the jobs still being restored.
================
*/
func (rt *Runtime) commitCharacterEffectWithCheckpoint(divisionID string, character *enterworld.Character, row enterworld.SkillRow, instanceToken uint32, state statuseffect.State, canStop bool, presentation EffectPresentation, nowMs int64, checkpoint bool) ([]wire.Frame, bool) {
	persistent := row.TimedJobExecutable()
	if row.Group == 0 {
		return nil, false
	}
	move := row.MovementModifier
	if move.Present && (!move.Supported || !move.Persistent && !enterworld.CharacterAlive(character)) {
		return nil, false
	}
	wirePhase := presentation.Phase
	if !row.SpawnStatus {
		// Native 59AF00 omits context mode for ordinary unlinked effects;
		// client 776450 defaults the absent byte to 2. The authority retains
		// mode 1 for self applications, including its replacement semantics.
		wirePhase = 2
	}
	payload, err := (wire.AttachedEffect{GID: enterworld.ObjectIDForCharacter(character), SkillID: row.ID, InstanceToken: instanceToken, Phase: wirePhase, Rider: presentation.Rider}).Encode(wire.AttachedEffectLayout{Status: row.SpawnStatus, Rider: row.EffectRider})
	if err != nil || nowMs < 0 {
		return nil, false
	}
	var expires int64
	if row.EffectDurationPresent || row.EffectDurationMs != 0 {
		// Native adds the authored duration and context rider as a uint32.
		duration := int64(row.EffectDurationMs + presentation.Rider)
		if persistent {
			duration = duration / 1000 * 1000
		} // 59B91D: timed jobs store seconds.
		if duration <= 0 && persistent {
			return nil, false
		}
		expires = nowMs + duration
		if expires < nowMs {
			return nil, false
		}
	}
	var bodyOwner uint64
	if row.BodyStatus.Present {
		bodyOwner = bodyEffectSequence.Add(1)
	}
	effect := statuseffect.Effect{
		ForcedTargetGID: presentation.ForcedTargetGID,
		OwnerGID:        enterworld.ObjectIDForCharacter(character),
		BodyStatusOwner: bodyOwner,
		DurationPresent: row.EffectDurationPresent || row.EffectDurationMs != 0,
		StartedAtMs:     nowMs,
		Imbue:           row.Imbue.Pinned,
		Persistent:      persistent,
		DeathProtected:  row.ReplacementPinned && row.Replacement.Cbuf,
		DivisionID:      divisionID,
		CharacterName:   character.Name,
		SkillID:         row.ID,
		SkillGroup:      row.Group,
		InstanceToken:   instanceToken,
		State:           state,
		Phase:           presentation.Phase, Rider: presentation.Rider, ExpiresAtMs: expires,
		ClientCancelable: !row.VoluntaryCancelBlocked || canStop,
	}
	if row.ReplacementPinned && row.Replacement.Activity != 0 && presentation.Phase == 1 {
		effect.EventCancelMask = row.Replacement.EventCancelMask
	}
	if hide := row.Concealment; hide.Pinned && hide.Hide {
		// 5965A2..59667D: the recipient's own STSP answers the hide's cut.
		effect.HidePenaltyPercent = hide.SpeedPercent
		if hide.SpeedBonus {
			stats, _, err := rt.playerCombatStats(divisionID, character)
			if err != nil {
				return nil, false
			}
			effect.HideBonusPercent = stats.SkillParameters[enterworld.ParameterStealthSpeed]
		}
	}
	word := transformWord(row)
	if word != 0 {
		// 5940BA / 5940F1: msch 1 and 2 apply the RefObj or player their
		// context names.
		if presentation.TransformRefObjID == 0 {
			return nil, false
		}
		effect.TransformRefObjID = presentation.TransformRefObjID
	}
	var statusFrames []wire.Frame
	// 594AC0 installs every block the row carries in one pass.
	writes := buffModifierWrites(row.BuffModifiers)
	itemWrites, err := rt.timedItemModifierWrites(divisionID, character, row.TimedEffect)
	if err != nil {
		return nil, false
	}
	writes = append(writes, itemWrites...)
	if row.TimedEffect.Pinned {
		writes = append(writes, combat.AttributeEffectWrites(row.TimedEffect.Attributes)...)
	}
	if row.TimedEffect.Pinned && row.TimedEffect.Block.Present {
		writes = append(writes, combat.BlockRateWrites(row.TimedEffect.Block.Mask, row.TimedEffect.Block.Value)...)
	}
	if row.TimedEffect.Pinned && row.TimedEffect.Defense {
		stats, _, err := rt.playerCombatStats(divisionID, character)
		if err != nil {
			return nil, false
		}
		d := row.TimedEffect
		defense, err := combat.DefenseEffectWrites(combat.DefenseModifierInput{
			Physical: d.Physical + presentation.DefenseAddend[0], Magical: d.Magical + presentation.DefenseAddend[1], CapPercent: d.CapPercent,
			CurrentPhysical: float32(stats.PhysicalDefense), CurrentMagical: float32(stats.MagicalDefense),
		})
		if err != nil {
			return nil, false
		}
		writes = append(writes, defense...)
	}
	if len(writes) > 0 {
		if effect.Modifiers, err = statuseffect.NewModifiers(writes); err != nil {
			return nil, false
		}
	}
	// These admitted producers have no source/area link context. Durable jobs
	// restore both words; ordinary recipient effects have mode-specific words.
	unlinked := !row.Replacement.Lnks && !row.Replacement.Efr2
	producer := row.MovementModifier.Supported || row.BodyStatus.Supported || row.Imbue.Pinned || row.TimedEffect.Pinned
	if row.ReplacementPinned && (effect.Persistent || unlinked && producer) {
		effect.InstalledStates, effect.RetirementStates = statuseffect.UnlinkedStateOperations(row.Replacement, presentation.Phase, effect.Persistent)
	}
	apply := func() bool {
		if character.DeletePending || row.BodyStatus.Present && (character.NativeBodyStatus == 1 || !enterworld.CharacterAlive(character)) {
			return false
		}
		previous := rt.effects.Snapshot(divisionID, character.Name)
		if row.MovementModifier.Present {
			effect.Movement = true
			effect.MovementPercent = row.MovementModifier.Percent
			effect.MovementKind = row.MovementModifier.Kind
		}
		if effect.Persistent {
			for _, job := range character.TimedSkillJobs {
				if job.SkillID == row.ID && job.Token != instanceToken {
					return false
				}
			}
		}
		if !rt.effects.Apply(effect) {
			return false
		}
		if bodyOwner == 0 {
			for _, old := range previous {
				sameInstance := old.SkillGroup == row.Group && old.InstanceToken == instanceToken
				if !sameInstance || old.BodyStatusOwner == 0 {
					continue
				}
				if character.TransitionBodyStatus(domain.BodyStatusTransition{RetireOwner: old.BodyStatusOwner}) {
					statusFrames = append(statusFrames, bodyStatusFrame(enterworld.ObjectIDForCharacter(character), 0))
				}
			}
		}
		if bodyOwner != 0 && character.TransitionBodyStatus(domain.BodyStatusTransition{Value: row.BodyStatus.Value, Owner: bodyOwner}) {
			statusFrames = append(statusFrames, bodyStatusFrame(enterworld.ObjectIDForCharacter(character), row.BodyStatus.Value))
		}
		if effect.Persistent && checkpoint {
			rt.checkpointSkillJobs(character, rt.effects.Snapshot(divisionID, character.Name), nowMs)
		}
		// 4F00F0 always refills the block; 4F0040 only an empty one.
		if effect.TransformRefObjID != 0 && (word == 1 || character.TransformMode == 0) {
			statusFrames = append(statusFrames, applyTransform(character, word, presentation))
		}
		statusFrames = append(statusFrames, rt.refreshMovementEffects(divisionID, character, nowMs)...)
		return true
	}
	if !apply() {
		return nil, false
	}
	return append(statusFrames, wire.Frame{Opcode: wire.OpAttachedEffect, Payload: payload}), true
}

/*
==================
drainStoppedCharacterEffects

drainStoppedCharacterEffects owns the second half of the stop lifecycle.
One packet is kept per character batch (as in retail); coalescing may place
several packets in one division push without merging their instance lists.
==================
*/
func (rt *Runtime) drainStoppedCharacterEffects() []simulation.DivisionFrames {
	if rt == nil || rt.effects == nil {
		return nil
	}
	// Drain, conditional restoration and ordered enqueue are one action-owner
	// transaction across divisions. No replacement can overtake retirement.
	rt.maintenance.Lock()
	defer rt.maintenance.Unlock()
	var out []simulation.DivisionFrames
	for _, batch := range rt.effects.DrainStopRequested() {
		var statusFrames []wire.Frame
		if c := rt.findCharacter(batch.DivisionID, batch.CharacterName); c != nil {
			rt.deps.Update(c, "retire-character-effect", func() bool {
				changed := rt.retireSkillJobs(c, batch.Effects)
				for _, effect := range batch.Effects {
					if effect.BodyStatusOwner != 0 && c.TransitionBodyStatus(domain.BodyStatusTransition{RetireOwner: effect.BodyStatusOwner}) {
						statusFrames = append(statusFrames, bodyStatusFrame(enterworld.ObjectIDForCharacter(c), 0))
						changed = true
					}
				}
				changed = clearTransform(c, batch.Effects) || changed
				statusFrames = append(statusFrames, rt.refreshMovementEffects(batch.DivisionID, c, rt.Now().UnixMilli())...)
				return changed || len(statusFrames) != 0
			})
			// 4B3660 removes instance-owned contributions before calling the
			// player's private 303D stat publisher. Never broadcast these stats.
			changedParameters := false
			for _, effect := range batch.Effects {
				changedParameters = changedParameters || effect.Modifiers.HasWrites()
			}
			if changedParameters {
				var drop []wire.Frame
				rt.deps.Update(c, "clamp-gauge-after-effect", func() bool {
					hp, mp := rt.clampStoredGaugeToKeeper(batch.DivisionID, c)
					drop = rt.gaugeDropFrames(batch.DivisionID, c, hp, mp, false)
					return hp || mp
				})
				stats, err := rt.PlayerBaseStats(batch.DivisionID, c)
				if err != nil {
					log.WithError(err).WithFields(log.Fields{"division": batch.DivisionID, "character": c.Name}).Error("effect retirement stat projection failed")
				} else {
					frames := append([]wire.Frame{{Opcode: wire.OpBaseStats, Payload: stats.Encode()}}, drop...)
					if rt.PushCharacterFrames != nil {
						rt.PushCharacterFrames(batch.DivisionID, c.Name, frames)
					} else {
						converted := make([]simulation.Frame, 0, len(frames))
						for _, frame := range frames {
							converted = append(converted, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
						}
						out = append(out, simulation.DivisionFrames{DivisionID: batch.DivisionID, OnlyCharacterID: c.ID, Frames: converted})
					}
				}
			}
		}
		// Apply enforces the same u8 ownership bound as the native retirement
		// vector, so every committed row remains serializable at teardown.
		if len(batch.Effects) == 0 || len(batch.Effects) > statuseffect.MaxAttachedEffectsPerCharacter {
			continue
		}
		tokens := make([]uint32, len(batch.Effects))
		for index, effect := range batch.Effects {
			tokens[index] = effect.InstanceToken
		}
		payload, err := (wire.EndedEffectInstances{InstanceTokens: tokens}).Encode()
		if err != nil {
			continue
		}
		if rt.PushCharacterFrames != nil && rt.PushDivisionPeerFrames != nil {
			// Production enqueue occurs before releasing maintenance ownership.
			// Even an obsolete status owner must enqueue its ended-token packet
			// here: that wire token may be reused immediately after retirement.
			statusFrames = append(statusFrames, wire.Frame{Opcode: wire.OpEndedEffectInstances, Payload: payload})
			rt.publishBodyStatus(batch.DivisionID, batch.CharacterName, statusFrames)
			continue
		}
		var frames []simulation.Frame
		for _, frame := range statusFrames {
			frames = append(frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope})
		}
		frames = append(frames, simulation.Frame{Opcode: wire.OpEndedEffectInstances, Payload: payload})
		out = append(out, simulation.DivisionFrames{
			DivisionID: batch.DivisionID,
			Frames:     frames,
		})
	}
	return out
}

/*
================
EntrySkills

The wire and JSON bootstrap consume this registry snapshot without recreating
effects or extending their remaining lifetimes.
================
*/
func (rt *Runtime) EntrySkills(divisionID, characterName string) []enterworld.EntrySkill {
	return rt.entrySkillsAt(divisionID, characterName, rt.Now().UnixMilli())
}

/*
================
entrySkillsAt

Project only live recipient-owned effects at the supplied world instant.
================
*/
func (rt *Runtime) entrySkillsAt(divisionID, characterName string, nowMs int64) []enterworld.EntrySkill {
	if rt == nil || rt.deps == nil || rt.effects == nil || rt.deps.SkillData() == nil {
		return nil
	}
	rows := rt.effects.Snapshot(divisionID, characterName)
	out := make([]enterworld.EntrySkill, 0, len(rows))
	for _, effect := range rows {
		// 59CDC0 omits source-phase linked rows from ordinary spawn effects;
		// they use the private B5ED relationship presentation instead.
		if effect.LinkToken != 0 && effect.Phase == 1 {
			continue
		}
		if effect.StopRequested || effect.Expired(nowMs) {
			continue
		}
		ref, ok := rt.deps.SkillData().SkillByID(effect.SkillID)
		if !ok {
			continue
		}
		row := enterworld.EntrySkill{ID: effect.SkillID, Status: effect.Phase, HasStatus: ref.SpawnStatus}
		if !row.HasStatus {
			row.Status = 2
		}
		if ref.SpawnToken {
			token := effect.InstanceToken
			remaining := effect.RemainingMs(nowMs)
			row.Token = &token
			row.Remaining = &remaining
		}
		out = append(out, row)
	}
	return out
}

/*
================
buffModifierWrites

The dru (595A97) and odar (596004) part of 594AC0.
================
*/
func buffModifierWrites(m enterworld.SkillBuffModifiers) []paramkeeper.Write {
	var writes []paramkeeper.Write
	if m.Dru {
		for i, params := range [2][2]uint16{{0x80, 0x81}, {0x82, 0x83}} {
			for _, param := range params {
				writes = append(writes, paramkeeper.Write{Parameter: param, Channel: paramkeeper.Flat, Value: float32(m.DruWords[i])})
			}
		}
	}
	if m.Odar {
		value := float32(-float64(m.OdarWord))
		for _, slot := range [...]struct {
			bits  uint32
			param uint16
		}{{4 | 1, 0xae}, {4 | 2, 0xaf}, {8 | 1, 0xb0}, {8 | 2, 0xb1}} {
			if m.OdarBits&slot.bits == slot.bits {
				writes = append(writes, paramkeeper.Write{Parameter: slot.param, Channel: paramkeeper.PercentProduct, Value: value})
			}
		}
	}
	return writes
}
