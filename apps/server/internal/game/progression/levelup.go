/*
===========================================================================

levelup.go - atomic experience, level transitions, and reward packets.

Owns the curve walk and persisted progression fields. Candidate state is
validated before commit; the recovery adapter supplies installed combat
effects without acquiring a second character lock.

===========================================================================
*/

package progression

// Native server 4E5250 completes the level transition before SP and EXP.
// Its upward branch at 4E5065 invokes reduced recovery and level presentation.
// The v1.150 consumers are independent: 777670 presents the level effect,
// 75BE90 installs maxima, 77A080 installs current gauges, and 779620 walks EXP.
// This port sends the completed maxima and recovery inside that transition
// boundary, before the SP update and EXP tail. Stat points ride only the EXP
// tail; adding a separate type-3 update would apply that total twice.
//
// Combat and quests call the same door-free updater inside their transaction.
// The diagnostic opcode is separately admitted at registration and request time.

import (
	"os"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// LevelCap is the Legend III era character level cap: 90, pinned by the
// era's official patch notes ("Increased level cap: Advance to level
// 90"). The shipped
// leveldata carries 140 curve rows, but rows past the cap are the same
// future-content data the 240 EU mastery ceiling is - the cap is the
// era's rule. At the cap experience still accrues but freezes just
// below the next-level threshold, so neither side of the wire ever
// walks a crossing.
const LevelCap int64 = 90

// StatPointsPerLevel / autoStatPerLevel are the pinned retail growth
// rule (adopt wave; proven arithmetically by the era observations - a
// fresh level-2 character with nothing spent shows 214 HP, which only
// STR 21 produces): per level gained, +1 STR and +1 INT automatically
// plus 3 free stat points. The 3-step future-path contract this
// implements lives at bootstrap/charactervitals/vitals.go.
const (
	StatPointsPerLevel int64 = 3
	autoStatPerLevel   int64 = 1
	// DeathPenaltyProtectedMaxLevel is the v1.188 GameServer gate at
	// CGObjPC_ApplyDeathPenalties 0x004e6a31..0x004e6a35: level <= 10
	// returns without touching experience.
	DeathPenaltyProtectedMaxLevel int64 = 10
)

// SkillExpPerSP is the skill-exp accumulator period: natively pinned by
// the client's own wrap (sub_779620 adds the delta to CICPlayer+0x830
// and reduces it mod 0x190 = 400, @0x779b02..0x779b29). The server MUST
// wrap its persisted SkillExp identically or the two accumulators drift
// apart on the next login snapshot.
const SkillExpPerSP int64 = 400

// skillExpSPYieldEnabled gates the skill-exp -> SP conversion (1 SP per
// completed SkillExpPerSP period). ENABLED by COORD ruling (levelup-wave
// board seq 202, closing the C5 question parked at seq 55). The two
// evidence classes stay distinct: the PERIOD (400) is natively pinned by
// the client's own wrap above; the YIELD "1 SP per 400" is era-attested
// EXTERNAL observation, not a v1.150 client pin - COORD-approved because
// (a) the client discards the quotient, so the conversion is
// server-owned by construction and SP reaches the client only as the
// 0x30B3 type 2 ABSOLUTE (no client expectation to contradict, no
// possible desync), and (b) with the yield off SP is strictly
// decreasing (this file holds the only producer; mastery training and
// skill learning are pure consumers), a certain total loss of function
// against a speculative pacing error. Still one constant to change if
// better evidence lands.
const skillExpSPYieldEnabled = true

// statPointWordMax is the width of every stat-point carrier on this
// plane: the 0x30D2 trailing tail, 0x30B3 type 3 and the char-data
// field are all u16.
const statPointWordMax int64 = 0xffff

// ---- 0xDE01 dev grant trigger ----

// OpDevGrantExp is the dev exp-grant trigger. NOT A RETAIL OPCODE and
// deliberately impossible to mistake for one: the v1.150 client's C->S
// space is 0x7xxx and its full inbound registrar (@0074d349..0074ed47,
// ~200 entries) carries nothing in 0xDExx. It remains as an isolated
// progression diagnostic even though ordinary combat now owns retail EXP;
// it is REGISTERED ONLY when SRO_DEV_EXP_GRANT=1 (default off - see
// Runtime.Register).
//
// The bound character must carry host-reconciled GMPrivilege. The env flag
// controls whether the diagnostic opcode exists at all; it never grants an
// ordinary player a levelling primitive.
//
// Payload: exactly [u32 expDelta][u32 skillExpDelta], each clamped to
// the 0x30D2 s32 ceiling. Refusals are ALL SILENT (a dev tool has no
// retail ack channel; the grant burst is the success response): wrong
// length, both deltas zero, non-GM or unbound session, delete-pending
// character, or an unwalkable curve.
const OpDevGrantExp uint16 = 0xDE01

// EnvDevExpGrant enables the dev trigger's registration when "1".
const EnvDevExpGrant = "SRO_DEV_EXP_GRANT"

/*
================
DevExpGrantEnabled

The diagnostic opcode is registered only when explicitly enabled at startup.
================
*/
func DevExpGrantEnabled() bool {
	return os.Getenv(EnvDevExpGrant) == "1"
}

/*
================
HandleDevGrantExp

The diagnostic still requires a bound GM character and a complete payload.
It shares the production grant transaction after admission.
================
*/
func (rt *Runtime) HandleDevGrantExp(divisionID string, character *enterworld.Character, payload []byte) OpResult {
	if character == nil || !character.GMPrivilege {
		return OpResult{}
	}
	r := wire.NewReader(payload)
	expDelta, err := r.U32()
	if err != nil {
		return OpResult{}
	}
	skillExpDelta, err := r.U32()
	if err != nil {
		return OpResult{}
	}
	if r.Done() != nil {
		return OpResult{}
	}
	return rt.GrantExperience(character, clampExpDelta(int64(expDelta)), clampExpDelta(int64(skillExpDelta)), 0)
}

// ---- the authority core ----

/*
==================
GrantExperience

GrantExperience applies an experience / skill-exp grant to the bound
character and answers the wire burst. Everything - the curve walk,
the level increments, the per-level stat and stat-point grants, the
MaxLevel watermark, the skill-exp wrap and the response snapshots -
runs inside ONE commit-door closure, so a concurrent stat allocation
or mastery training can never interleave with a half-applied level.

sourceGid rides the 0x30D2 animate field (0 = no gauge animation; a
future combat plane passes the killed entity's gid).

The emitted exp delta is the delta the core ACTUALLY APPLIED, which
the level cap can clamp below the requested amount - the client
re-walks the same curve from the emitted value, so emitting the
requested amount after clamping would walk the client past the
server (the parity hazard EncodeExpUpdate documents).

Refusal shape: an empty OpResult (no frames). The retail client has
no "exp grant refused" conversation to have - grants are
server-initiated - so there is nothing honest to send.
==================
*/
func (rt *Runtime) GrantExperience(character *enterworld.Character, expDelta, skillExpDelta int64, sourceGid uint32) OpResult {
	if character == nil {
		return OpResult{}
	}
	var frames []wire.Frame
	rt.deps.Update(character, "grant-exp", func() bool {
		var changed bool
		frames, changed = rt.applyExperience(character, expDelta, skillExpDelta, sourceGid)
		return changed
	})
	return OpResult{
		Frames:    frames,
		Broadcast: wire.ProgressionBroadcastFrames(frames),
	}
}

/*
==================
ExperienceUpdater

ExperienceUpdater returns the progression update used by another
character transaction, notably an atomic quest turn-in. The returned
function never opens a second authority door; its caller must invoke it
from inside Dependencies.Update.
==================
*/
func (rt *Runtime) ExperienceUpdater() func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
	return rt.applyExperience
}

/*
==================
DeathPenaltyUpdater

DeathPenaltyUpdater returns the door-free ordinary monster-death updater.
Monster combat calls it from inside the SAME character transaction that
commits fatal HP, so a crash cannot persist a corpse without its applicable
progression consequence. Packet delivery remains a downstream concern.
==================
*/
func (rt *Runtime) DeathPenaltyUpdater() func(*enterworld.Character) ([]wire.Frame, bool) {
	return rt.applyOrdinaryDeathPenalty
}

/*
==================
ApplyOrdinaryDeathPenalty

ApplyOrdinaryDeathPenalty is the standalone transactional entry used by
focused tests and future non-combat death owners. Live monster combat uses
DeathPenaltyUpdater to avoid opening a nested authority door.
==================
*/
func (rt *Runtime) ApplyOrdinaryDeathPenalty(character *enterworld.Character) OpResult {
	if character == nil {
		return OpResult{}
	}
	var frames []wire.Frame
	rt.deps.Update(character, "ordinary-death-exp-penalty", func() bool {
		var changed bool
		frames, changed = rt.applyOrdinaryDeathPenalty(character)
		return changed
	})
	return OpResult{
		Frames:    frames,
		Broadcast: wire.ProgressionBroadcastFrames(frames),
	}
}

/*
==================
ordinaryDeathPenaltyLoss

ordinaryDeathPenaltyLoss reproduces the unmodified v1.188 GameServer
branch at 0x004e6ae6..0x004e6b74:

	min(trunc(ExpRequired(level) * 0.02), leveldata[column 5] * 100), >= 1

Rizin corrects the decompiler's phantom x87 argument: an optional
character modifier is read by sub_4b3740(0x101, PC+0x1ec). This rebuild
has no such effect plane yet, so the evidenced base loss is applied. Missing
curve/cap authority refuses rather than inventing a penalty.
==================
*/
func ordinaryDeathPenaltyLoss(levels enterworld.LevelDataSource, level int64) (int64, bool) {
	if level <= DeathPenaltyProtectedMaxLevel || levels == nil {
		return 0, level <= DeathPenaltyProtectedMaxLevel
	}
	required, requiredOK := levels.ExpRequired(level)
	basis, basisOK := levels.MonsterExpBasis(level)
	if !requiredOK || !basisOK || required <= 0 || basis < 0 {
		return 0, false
	}
	loss := required / 50 // trunc(2%) for a positive integer requirement.
	cap := basis * 100
	if loss > cap {
		loss = cap
	}
	if loss < 1 {
		loss = 1
	}
	return loss, true
}

/*
================
applyOrdinaryDeathPenalty

Called inside the fatal-HP transaction; protected levels need no curve row.
================
*/
func (rt *Runtime) applyOrdinaryDeathPenalty(character *enterworld.Character) ([]wire.Frame, bool) {
	if character == nil || character.DeletePending {
		return nil, false
	}
	loss, ok := ordinaryDeathPenaltyLoss(rt.deps.LevelData(), characterLevel(character))
	if !ok {
		log.Warnf("progression: death exp penalty refused - incomplete leveldata row for level %d", characterLevel(character))
		return nil, false
	}
	if loss == 0 {
		return nil, false
	}
	return rt.applyExperience(character, -loss, 0, 0)
}

/*
================
applyExperience

The caller already owns the character transaction. All fallible projection
runs on a detached candidate before persisted fields or packets are exposed.
================
*/
func (rt *Runtime) applyExperience(
	character *enterworld.Character,
	expDelta, skillExpDelta int64,
	sourceGid uint32,
) ([]wire.Frame, bool) {
	if character == nil || character.DeletePending {
		return nil, false
	}
	next := character.Snapshot()
	expDelta, skillExpDelta = rt.Growth.scale(rt.deps.LevelData(), characterLevel(next), expDelta, skillExpDelta)
	expDelta = clampExpDelta(expDelta)
	skillExpDelta = clampSkillExpDelta(skillExpDelta)
	if expDelta == 0 && skillExpDelta == 0 {
		return nil, false
	}

	// The curve is authority DATA (leveldata column 1); no table means
	// no grant, never "level free" (the leveldata.go posture).
	if expDelta != 0 && rt.deps.LevelData() == nil {
		log.Warn("progression: exp update refused - no leveldata source wired")
		return nil, false
	}

	walk := walkExpCurve(rt.deps.LevelData(), characterLevel(next), coercePoints(next.Experience), expDelta)
	if !walk.ok {
		// A level without a curve row cannot be walked on either side of
		// the wire; refuse the whole grant.
		log.Warnf("progression: exp grant refused - leveldata has no row for level %d", walk.level)
		return nil, false
	}

	// Skill exp: wrap exactly like the client accumulator; the quotient is
	// the SP yield (see skillExpSPYieldEnabled).
	skillExpTotal := coercePoints(next.SkillExp) + skillExpDelta
	newSkillExp := skillExpTotal % SkillExpPerSP
	spChanged := false
	remainingSP := int64(0)
	if skillExpSPYieldEnabled {
		if spGained := skillExpTotal / SkillExpPerSP; spGained > 0 {
			remainingSP = clampSkillPoints(coercePoints(next.SkillPoints) + spGained)
			next.SkillPoints = &remainingSP
			spChanged = true
		}
	}

	if walk.applied == 0 && skillExpDelta == 0 {
		// The cap clamped the entire grant away.
		return nil, false
	}

	statPoints := coercePoints(next.StatPoints)
	if statPoints > statPointWordMax {
		statPoints = statPointWordMax
	}
	levelChanged := walk.levelsGained > 0 || walk.levelsLost > 0
	maxLevel := characterLevel(next)
	if next.MaxLevel != nil && *next.MaxLevel > maxLevel {
		maxLevel = *next.MaxLevel
	}
	newMaxLevels := int64(0)
	if walk.level > maxLevel {
		newMaxLevels = walk.level - maxLevel
	}
	if walk.levelsGained > 0 {
		// Materialize absent currents at the pre-level maxima before level
		// and stats move, so recovery reductions apply to the whole deficit.
		if next.CurrentHP == nil {
			full := keeperOrDerived(rt, next, true)
			next.CurrentHP = &full
		}
		if next.CurrentMP == nil {
			full := keeperOrDerived(rt, next, false)
			next.CurrentMP = &full
		}

		raisedStr := clampStatWord(enterworld.CharacterStrength(next) + autoStatPerLevel*newMaxLevels)
		next.Strength = &raisedStr
		raisedInt := clampStatWord(enterworld.CharacterIntellect(next) + autoStatPerLevel*newMaxLevels)
		next.Intellect = &raisedInt
		statPoints += StatPointsPerLevel * newMaxLevels
		if statPoints > statPointWordMax {
			statPoints = statPointWordMax
		}
		next.StatPoints = &statPoints
	}
	gaugeClamped := false
	if levelChanged {
		next.Level = &walk.level
		if walk.levelsLost > 0 {
			hp, mp := clampGaugeToProjection(rt, next)
			gaugeClamped = hp || mp
		}
		if next.MaxLevel == nil || *next.MaxLevel < walk.level {
			watermark := walk.level
			next.MaxLevel = &watermark
		}
	}
	// The beginner mark is a server-authoritative visual flag. Native stops
	// exposing its checkbox after level 19 and the retail tooltip promises
	// the mark disappears at high level, so the level-transition owner clears
	// it exactly when crossing that boundary and publishes the normal B683
	// state update after the progression packet.
	visualFlags := enterworld.ResolveVisualFlags(next)
	beginnerMarkCleared := walk.level > enterworld.BeginnerMarkMaxLevel &&
		visualFlags&enterworld.VisualFlagBeginner != 0
	if beginnerMarkCleared {
		visualFlags &^= enterworld.VisualFlagBeginner
		value := int64(visualFlags)
		next.VisualFlags = &value
	}
	next.Experience = &walk.exp
	next.SkillExp = &newSkillExp

	var statBlock []byte
	if levelChanged {
		display, err := rt.playerBaseStats(next)
		if err != nil {
			log.Warnf("progression: exp grant refused - %v", err)
			return nil, false
		}
		statBlock = enterworld.BuildLoginStatBlock(next, display)
		if walk.levelsGained > 0 {
			if err := rt.recoverLevelVitals(next, display); err != nil {
				log.Warnf("progression: level recovery refused - %v", err)
				return nil, false
			}
		}
	}

	// Preserve the retail semantic producer order. Presentation belongs to
	// the upward level-transition owner; the explicit v1.150 stat snapshot is
	// the rest of that transition boundary. SP conversion follows, and the
	// EXP/skill-EXP delta closes the burst last.
	frames := make([]wire.Frame, 0, 5)
	if walk.levelsGained > 0 {
		frames = append(frames,
			wire.Frame{
				Opcode:  wire.OpLevelUpEffect,
				Payload: wire.EncodeLevelUpEffect(enterworld.ObjectIDForCharacter(next)),
			},
		)
	}
	if levelChanged {
		frames = append(frames, wire.Frame{
			Opcode:  wire.OpBaseStats,
			Payload: statBlock,
		})
		if gaugeClamped {
			frames = append(frames, wire.Frame{Opcode: 0x33A6, Payload: clampedGaugePayload(next, rt)})
		}
		if walk.levelsGained > 0 && enterworld.CharacterAlive(next) {
			frames = append(frames, levelRecoveryFrame(next))
		}
	}
	if spChanged {
		frames = append(frames, wire.Frame{
			Opcode:  wire.OpPointsUpdate,
			Payload: wire.EncodePointsSkillUpdate(uint32(remainingSP), false),
		})
	}
	frames = append(frames, wire.Frame{
		Opcode: wire.OpExpUpdate,
		Payload: wire.EncodeExpUpdate(
			sourceGid,
			int32(walk.applied),
			int32(skillExpDelta),
			walk.levelsGained > 0,
			uint16(statPoints),
		),
	})
	if beginnerMarkCleared {
		frames = append(frames, wire.Frame{
			Opcode: wire.OpVisualFlagsUpdate,
			Payload: wire.EncodeVisualFlagsUpdate(
				enterworld.ObjectIDForCharacter(next),
				visualFlags,
			),
		})
	}

	// Publish only after the fallible curve and combat-stat graph both
	// succeeded. This keeps a refused grant from leaking an in-memory
	// half-level even when applyExperience is nested in another authority
	// transaction (quest rewards use this seam).
	character.Experience = next.Experience
	character.SkillExp = next.SkillExp
	// 4E5710 keeps the requested loss, not what the level walk applied:
	// resurrection returns a percent of what the death took.
	if expDelta < 0 {
		character.LastExpLoss = -expDelta
	}
	if spChanged {
		character.SkillPoints = next.SkillPoints
	}
	if levelChanged {
		character.CurrentHP = next.CurrentHP
		character.CurrentMP = next.CurrentMP
		character.Strength = next.Strength
		character.Intellect = next.Intellect
		character.StatPoints = next.StatPoints
		character.Level = next.Level
		character.MaxLevel = next.MaxLevel
	}
	if beginnerMarkCleared {
		character.VisualFlags = next.VisualFlags
	}
	return frames, true
}

/*
================
expWalkResult

Carries both the applied wire delta and the level crossings. The requested
delta may exceed the level cap and must never be echoed as if it committed.
================
*/
type expWalkResult struct {
	// level / exp are the post-walk character level and WITHIN-LEVEL
	// experience remainder (the client stores the same remainder - its
	// walk subtracts each crossed level's requirement, @0x7797d6..
	// 0x779800).
	level int64
	exp   int64
	// levelsGained counts upward crossings (multi-level single grants
	// are the native shape - the client loops).
	levelsGained int64
	// levelsLost is the negative path's symmetric crossing count. The native
	// client changes only current level/exp; MaxLevel and earned base/stat
	// points stay at their all-time watermark.
	levelsLost int64
	// applied is the exp delta actually absorbed: equal to the request
	// except when the level cap clamps, and ALWAYS the value the wire
	// must carry.
	applied int64
	// ok is false when a needed curve row is missing - the whole grant
	// must refuse (fail closed), never walk blind.
	ok bool
}

/*
==================
walkExpCurve

walkExpCurve is the server's copy of the client's own level walk
(sub_779620 @0x7797d6..0x779800) over the same shipped curve
(leveldata column 1 via LevelDataSource.ExpRequired), plus the level
cap the client-side walk never needed (retail servers simply stopped
granting at the cap): at LevelCap the experience freezes at
requirement-1 so no crossing is ever emitted or walked.
==================
*/
func walkExpCurve(levels enterworld.LevelDataSource, level, exp, delta int64) expWalkResult {
	result := expWalkResult{level: level, exp: exp, ok: true}
	if delta == 0 {
		return result
	}
	if delta < 0 {
		total := exp + delta
		for total < 0 {
			if result.level <= 1 {
				// The native loop relies on the server never walking below level
				// one. Clamp the applied wire delta to exactly the available EXP.
				result.applied = delta - total
				result.exp = 0
				return result
			}
			previous := result.level - 1
			required, priced := levels.ExpRequired(previous)
			if !priced {
				return expWalkResult{level: previous, ok: false}
			}
			total += required
			result.level = previous
			result.levelsLost++
		}
		result.exp = total
		result.applied = delta
		return result
	}
	// Within-level exp is always below its level's requirement
	// (<= 34.9e9 at row 140) and delta is clamped to the s32 ceiling,
	// so the running total cannot overflow int64.
	total := exp + delta
	crossed := int64(0)
	for {
		required, priced := levels.ExpRequired(result.level)
		if !priced {
			return expWalkResult{level: result.level, ok: false}
		}
		if total < required {
			result.exp = total
			break
		}
		if result.level >= LevelCap {
			// Freeze just below the boundary: emitting a crossing the
			// server refuses to apply would desync the client's walk.
			result.exp = required - 1
			break
		}
		total -= required
		crossed += required
		result.level++
		result.levelsGained++
	}
	result.applied = crossed + result.exp - exp
	return result
}

/*
================
clampExpDelta

Positive grants and death losses share the signed 30D2 delta field.
================
*/
func clampExpDelta(value int64) int64 {
	if value < -0x80000000 {
		return -0x80000000
	}
	if value > 0x7fffffff {
		return 0x7fffffff
	}
	return value
}

/*
================
clampSkillExpDelta

Skill EXP is gain-only. Its unsigned modulo accumulator has no loss contract.
================
*/
func clampSkillExpDelta(value int64) int64 {
	if value < 0 {
		return 0
	}
	return clampExpDelta(value)
}
