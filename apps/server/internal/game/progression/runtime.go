/*
===========================================================================

runtime.go - stat points, mastery and skill training

Package progression is the character-progression plane: spending stat points
on STR/INT, spending skill points to train a mastery, and spending skill
points to learn a skill.

The wire contracts live in internal/game/item/wire/statwire.go, pinned to the
v1.150 client's senders and ack handlers. The native client's own gates
(the plus buttons only enable while remaining points > 0; the mastery
level-up button only enables under the level and SP checks) are ADVISORY
- this server is the authority, so every gate is re-checked here, and a
request the retail UI could never compose is refused rather than trusted.

Both operations mutate persisted character fields, so each one runs its
check AND its write inside a single commit-door closure: the door holds
the store lock across the closure, which makes check-decrement-increment
atomic against any other lane touching the same record.

===========================================================================
*/
package progression

import (
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// MasteryLevelCap is the absolute per-mastery ceiling the client's own
// level-up gate enforces (sub_5841d0: level < 0x78).
const MasteryLevelCap int64 = 120

// 59E450 returns 34xx; the v1.150 learning response carries the low byte.
const (
	skillLearnMasteryLevelRefusal uint8 = 0x02
	skillLearnRaceRefusal         uint8 = 0x05
	skillLearnPrerequisiteMissing uint8 = 0x06
	skillLearnPrerequisiteLevel   uint8 = 0x07
	skillLearnUnavailable         uint8 = 0x09
	skillLearnRankRefusal         uint8 = 0x0c
)

// Total-mastery allowance (notice 07:05): the budget the v1.150 client
// itself DISPLAYS on the skill board but never locally enforces, which is
// why the gate is server-owned. Client pin (re-verified against
// temp/dumps/SRO_Client_psuedo.txt for this lane): sub_58c310 @0x0058c310
// composes a bare u"%d/%d" of (sum of mastery levels)/(cap), selecting the
// cap on the NATIVE country byte charBody+0x9c read @0x0058c372 - byte 0
// (Chinese) takes the flat 0x12c @0x0058c3bc, byte 1 (European) doubles the
// per-character level (x87_r7_1 + x87_r7_1) and clamps at 0xf0
// @0x0058c388..0x0058c39c. The summed quantity is sub_850790 @0x00850790
// walking the mastery list accumulating each node's LEVEL byte; the
// train-enable gate sub_5841d0 never consults it. Era: Legend III, level
// cap 90.
const (
	// TotalMasteryCapChina is the flat Chinese allowance (0x12c). Flat,
	// NOT level-scaled: a period level-71 character displays "300/300".
	TotalMasteryCapChina int64 = 300
	// TotalMasteryCapEuropeCeiling is the hard ceiling (0xf0) clamping
	// the European 2-levels-per-character-level multiplier. It never
	// binds below level 120; the live low-level bound is 2 x level.
	TotalMasteryCapEuropeCeiling int64 = 240
)

/*
==================
totalMasteryCap

totalMasteryCap answers the character's 07:05 allowance. The selector is
the native country byte (0 China / 1 Europe), the same byte the client's
display path branches on. It must NOT be the Node-facing RaceIndex enum:
that convention is INVERTED (enterworld.RaceChina is 1 there, while the
native byte for China is 0), so a raw RaceIndex branch would hand the
Chinese flat cap to Europeans and the European multiplier to Chinese.
==================
*/
func totalMasteryCap(c *enterworld.Character) int64 {
	if enterworld.NativeCountryByte9C(c) == 0 {
		return TotalMasteryCapChina
	}
	allowance := 2 * characterLevel(c)
	if allowance > TotalMasteryCapEuropeCeiling {
		allowance = TotalMasteryCapEuropeCeiling
	}
	return allowance
}

/*
==================
masteryLevelSum

masteryLevelSum is the gated quantity: the sum of the character's
trained mastery levels - not SP spent, not a count of masteries. The
same walk as the client's sub_850790 (each node's level byte, which as
a byte can never be negative; a corrupt negative record is ignored
rather than allowed to widen the budget).
==================
*/
func masteryLevelSum(c *enterworld.Character) int64 {
	total := int64(0)
	for _, row := range c.Masteries {
		if row.Level > 0 {
			total += row.Level
		}
	}
	return total
}

/*
==================
OpResult

OpResult is one operation's answer: the ordered frames for the acting
session and the public presentation projection for division peers. Stat,
mastery, SP and EXP state stays private; only a level-up effect may appear
in Broadcast.
==================
*/
type OpResult struct {
	Frames    []wire.Frame
	Broadcast []wire.Frame
}

/*
================
Runtime

Owns progression decisions under the character transaction. Composition
provides installed-effect projections; callbacks must not open another
character transaction or publish uncommitted state.
================
*/
type Runtime struct {
	deps Dependencies
	// BaseStats is bound once by composition to the shard's installed-effect
	// owner. Standalone runtimes without effects use the static graph below.
	BaseStats          func(*enterworld.Character) (wire.BaseStats, error)
	RecoverLevelVitals func(*enterworld.Character) error
	Withdrawal         WithdrawalHooks
	// NowMs stamps the milestone levels a character reaches for the
	// community site (Character.RecordLevelReached); nil reads the wall clock.
	NowMs func() int64
	// Growth is the closed-beta rate switch (growth.go); zero is native.
	Growth GrowthRates
	// MasteryTotalOverride is the beta budget; zero keeps native race rules.
	MasteryTotalOverride int64
}

/*
==================
NewRuntime

NewRuntime builds the stat plane over the authoritative character and
progression-data sources. The character source must be the one enter-world
uses, so both lanes resolve the same *Character pointers.
==================
*/
func NewRuntime(deps Dependencies) *Runtime {
	return &Runtime{deps: deps}
}

/*
================
playerBaseStats

Production includes installed effects. Standalone runtimes project the
static keeper graph through the same wire boundary.
================
*/
func (rt *Runtime) playerBaseStats(character *enterworld.Character) (wire.BaseStats, error) {
	if rt.BaseStats != nil {
		return rt.BaseStats(character)
	}
	return combat.PlayerBaseStats(character, combat.Catalogs{Items: rt.deps.ItemReferences(), Skills: rt.deps.SkillData(), MagicOptions: rt.deps.MagicOptionDefinitions()})
}

/*
==================
keeperOrDerived

keeperOrDerived materializes a nil current at the pre-change maximum.
The keeper value wins when the stat projection carried one; otherwise the
closed form is the same number the keeper produces with no item or abnormal input.
==================
*/
func keeperOrDerived(rt *Runtime, c *enterworld.Character, hp bool) int64 {
	if block, err := rt.playerBaseStats(c); err == nil {
		if hp && block.MaxHP != 0 {
			return int64(block.MaxHP)
		}
		if !hp && block.MaxMP != 0 {
			return int64(block.MaxMP)
		}
	}
	if hp {
		return enterworld.DerivedMaxHP(c)
	}
	return enterworld.DerivedMaxMP(c)
}

/*
================
clampedGaugePayload

Encodes both current gauges after a downward clamp. An absent stored
current means full at the projected maximum.
================
*/
func clampedGaugePayload(c *enterworld.Character, rt *Runtime) []byte {
	hp, mp := keeperOrDerived(rt, c, true), keeperOrDerived(rt, c, false)
	if c.CurrentHP != nil {
		hp = *c.CurrentHP
	}
	if c.CurrentMP != nil {
		mp = *c.CurrentMP
	}
	if hp < 0 {
		hp = 0
	}
	if mp < 0 {
		mp = 0
	}
	return wire.NewWriter(15).U32(enterworld.ObjectIDForCharacter(c)).U16(0).U8(0x03).U32(uint32(hp)).U32(uint32(mp)).Payload()
}

/*
================
clampGaugeToProjection

Lowering a maximum may discard excess current points but never heals.
Absent currents retain their full-at-maximum representation.
================
*/
func clampGaugeToProjection(rt *Runtime, c *enterworld.Character) (hp bool, mp bool) {
	if c == nil {
		return false, false
	}
	if c.CurrentHP != nil {
		maxHP := keeperOrDerived(rt, c, true)
		if *c.CurrentHP > maxHP {
			c.CurrentHP = &maxHP
			hp = true
		}
	}
	if c.CurrentMP != nil {
		maxMP := keeperOrDerived(rt, c, false)
		if *c.CurrentMP > maxMP {
			c.CurrentMP = &maxMP
			mp = true
		}
	}
	return hp, mp
}

/*
================
statKind

Selects the allocated stat while preserving one shared spending transaction.
================
*/
type statKind int

const (
	statStrength statKind = iota
	statIntellect
)

//============================================================================

/*
================
HandleAllocStr

Spends one point through the strength request/ack pair, 727A/B27A.
================
*/
func (rt *Runtime) HandleAllocStr(divisionID string, character *enterworld.Character, payload []byte) OpResult {
	return rt.allocate(character, payload, statStrength, wire.OpAllocStrResponse)
}

/*
================
HandleAllocInt

Spends one point through the intellect request/ack pair, 7552/B552.
================
*/
func (rt *Runtime) HandleAllocInt(divisionID string, character *enterworld.Character, payload []byte) OpResult {
	return rt.allocate(character, payload, statIntellect, wire.OpAllocIntResponse)
}

/*
==================
allocate

allocate is the shared body of both stat requests. The native request
carries NO payload, so anything in the body is a shape the retail client
cannot compose and the request is refused.

On success the ack is the ONLY thing that moves the client's remaining
count (its handler decrements CICPlayer+0x83c), so this never appends a
0x30B3 type-3 absolute update: that would double-apply the spend.
The 0x343C block follows to move the live STR/INT words, which char-data
never carries - and, since the adopt-wave, the DERIVED MaxHP/MaxMP the
raised stat produces (bootstrap/charactervitals/vitals.go), so the spend visibly
moves the player's gauge maximum. Current HP/MP is deliberately NOT
touched (the LEAVE policy: the client clamps at read, headroom is the
point of the purchase).

Maxima are derived and do not exist in the persisted type. Currents ARE
real persisted state: an absent current reads as "full at max" on every
emission, which
would silently follow a raised maximum upward, so before raising the
stat this door MATERIALIZES the affected pool's current at its
pre-raise derived value - the bar keeps its number exactly as the
LEAVE policy demands (retail shows 200/210 after a creation-stat +STR,
never 210/210).
==================
*/
func (rt *Runtime) allocate(character *enterworld.Character, payload []byte, kind statKind, ackOpcode uint16) OpResult {
	refuse := OpResult{Frames: []wire.Frame{{
		Opcode:  ackOpcode,
		Payload: wire.EncodePointsAck(false, wire.ErrCodeStatAllocRefused),
	}}}

	if character == nil || len(payload) != 0 {
		return refuse
	}

	// The gate, the write AND the response snapshot share one door
	// closure: the remaining-point check cannot go stale between reading
	// and spending, and the 0x343C block is encoded while the store lock
	// is still held. Reading the record's fields after the door returned
	// would be an unsynchronized read of shared state (the hazard the
	// store's read-door contract exists to prevent).
	var statBlock []byte
	var clampedHP, clampedMP bool
	granted := rt.deps.Update(character, mutateLabel(kind), func() bool {
		if character.DeletePending {
			return false
		}
		next := character.Snapshot()
		remaining := coercePoints(next.StatPoints)
		if remaining <= 0 {
			return false
		}
		remaining--
		next.StatPoints = &remaining

		switch kind {
		case statStrength:
			// Pin the current BEFORE the max moves (LEAVE): an absent
			// current would otherwise read as full at the raised max.
			if next.CurrentHP == nil {
				full := keeperOrDerived(rt, next, true)
				next.CurrentHP = &full
			}
			raised := clampStatWord(enterworld.CharacterStrength(next) + 1)
			next.Strength = &raised
		case statIntellect:
			if next.CurrentMP == nil {
				full := keeperOrDerived(rt, next, false)
				next.CurrentMP = &full
			}
			raised := clampStatWord(enterworld.CharacterIntellect(next) + 1)
			next.Intellect = &raised
		}
		clampedHP, clampedMP = clampGaugeToProjection(rt, next)
		display, err := rt.playerBaseStats(next)
		if err != nil {
			log.Warnf("progression: %s refused - %v", mutateLabel(kind), err)
			return false
		}
		statBlock = enterworld.BuildLoginStatBlock(next, display)

		// Commit only the stat plane's owned fields after every fallible
		// derivation succeeded. A malformed item row cannot consume a point
		// and then make the transaction report refusal.
		character.StatPoints = next.StatPoints
		character.CurrentHP = next.CurrentHP
		character.CurrentMP = next.CurrentMP
		character.Strength = next.Strength
		character.Intellect = next.Intellect
		return true
	})
	if !granted {
		return refuse
	}

	frames := []wire.Frame{
		{Opcode: ackOpcode, Payload: wire.EncodePointsAck(true, 0)},
		{Opcode: wire.OpBaseStats, Payload: statBlock},
	}
	if clampedHP || clampedMP {
		frames = append(frames, wire.Frame{Opcode: 0x33A6, Payload: clampedGaugePayload(character, rt)})
	}
	return OpResult{Frames: frames}
}

/*
================
mutateLabel

Keeps the persisted transaction label tied to the stat that was requested.
================
*/
func mutateLabel(kind statKind) string {
	if kind == statIntellect {
		return "stat-alloc-int"
	}
	return "stat-alloc-str"
}

// ---- 0x7165 mastery training ----

/*
==================
HandleMasteryLevelUp

HandleMasteryLevelUp spends skill points to raise one mastery a level
(0x7165 -> 0xB165, plus the absolute SP refresh on 0x30B3 type 2).

Gate order mirrors what the client's own enable check reads, with the
authority checks the client cannot be trusted for:

 1. the mastery record must EXIST on the character (the racial set is
    seeded at creation; a missing record means the character never had
    that mastery, and the equip plane treats a missing record as a
    miss-continue, so training one into existence would invent state);
 2. the next level must not pass the character level, nor the 120 cap;
 3. the TOTAL-mastery allowance (notice 07:05): the post-training sum
    of mastery levels must not pass the per-race budget (CH flat 300 /
    EU min(2 x level, 240) - see totalMasteryCap). Checked inside the
    commit door, before the SP spend: the sum is recomputed under the
    store lock so concurrent trainings cannot both slip past the
    budget, and a refusal for the limit family (07:05) fires before
    the pricing refusal (07:02), mirroring how the per-mastery limit
    (07:04) already precedes it;
 4. the character must hold the train's SP cost: the leveldata
    column-2 row of the CURRENT level (the level being left), and the
    0->1 first train is FREE - see the pricing block below for the
    three concordant witnesses.

==================
*/
func (rt *Runtime) HandleMasteryLevelUp(divisionID string, character *enterworld.Character, payload []byte) OpResult {
	request, err := wire.DecodeMasteryLevelUpRequest(payload)
	if err != nil || character == nil {
		return masteryRefusal(wire.ErrCodeMasteryLevelLimit)
	}
	// The native UI always sends 1. A bulk amount would have to spend the
	// SP of every intermediate level, and no capture pins how retail
	// batches that, so it is refused instead of approximated.
	if request.Amount != 1 {
		return masteryRefusal(wire.ErrCodeMasteryLevelLimit)
	}

	// PRICING (levelup wave, LANE-2): the L -> L+1 train costs the
	// leveldata column-2 row of the CURRENT level L, and the 0 -> 1
	// first train is FREE. Three concordant witnesses:
	//   - the v1.150 client's train-enable gate reads the CURRENT
	//     level's row for its SP-affordability check (sub_5841d0
	//     @0x00584389: sub_7e0f20(currentLevel)+0x10 <= heldSP);
	//   - the v1.150 mastery tooltip displays the same row as the next
	//     level's required SP and HARDCODES ZERO for a level-0 row
	//     (sub_55ab40 @0x0055b21a..b22b: edi_5 = 0 unless level != 0);
	//   - the cross-version research GameServer accumulates its cost
	//     over rows [current, current+amount) (loop @0x0059c64c) and
	//     its level==0 branch BYPASSES pricing entirely, jumping to the
	//     success path (@0x0059c62a..0x0059c62c) - corroboration only,
	//     per the cross-version rule; the adopted prices are our own
	//     shipped leveldata rows, and the display half is v1.150-native.
	// The client's enable gate and tooltip are the only prices a v1.150
	// player can see; charging any other row would contradict our own
	// client's display (the old SkillPointCost(newLevel) call did, at
	// every leveldata boundary).
	//
	// For a priced train (current >= 1) the cost stays authority DATA,
	// not a formula: an unavailable table refuses rather than trains
	// free. The free 0->1 train needs no row (there is no leveldata
	// row 0 to price - the pinned client/server behaviour above, not a
	// missing-data default), so it stays trainable even degraded.
	// The total-mastery ceiling (notice 07:05) is enforced below, inside
	// the door. EVIDENCE GRADATIONS (three claims at three different
	// strengths, do not blur them):
	//  1. PINNED: a server-side total-cap CONCEPT existed - the protocol
	//     reserved a dedicated refusal reason (guide category 7 code 5,
	//     UIIT_STT_SKILL_LEARN_MASTERY_TOTAL_LIMIT) and the client cannot
	//     be the enforcer (composer gates on SP only; the sum walker's
	//     only callers are display formatters; binary-sealed twice,
	//     independently: board seq 191 and 242).
	//  2. UNPROVEN: whether retail's server ever actually FIRED that
	//     refusal - the wire carries no value to inspect; no evidence
	//     either way.
	//  3. INFERRED: the NUMBER (CH 300 / EU min(2*level, 240)) comes from
	//     the client's OWN display (two independent sites), on the stated
	//     argument that a shipped UI showing players a budget its server
	//     did not honour would be a retail defect. An inference we chose
	//     to enforce (COORD seq 201), not a proof.
	// History of the value: the allowance was only ever a compiled immediate
	// inside the absent GameServer binary, until research located the client's
	// display sites.
	// Characters already above their allowance are never repaired or
	// broken by this gate - their stored levels stand and every plane
	// keeps reading them; they simply refuse further training with 07:05.

	trained := false
	refusal := wire.ErrCodeMasteryLevelLimit
	remainingSP := int64(0)
	newLevel := int64(0)
	rt.deps.Update(character, "mastery-levelup", func() bool {
		if character.DeletePending {
			return false
		}
		current, known := enterworld.MasteryLevel(character, request.MasteryID)
		if !known {
			return false
		}
		newLevel = current + 1
		if newLevel > MasteryLevelCap || newLevel > characterLevel(character) {
			return false
		}

		// Gate 3: the 07:05 budget, on the door-fresh sum. Amount is
		// pinned to 1 above, so the post-training sum is sum+1.
		if masteryLevelSum(character)+1 > rt.masteryAllowance(character) {
			refusal = wire.ErrCodeMasteryTotalLimit
			return false
		}

		cost := int64(0)
		if current > 0 {
			refusal = wire.ErrCodeMasterySkillPoints
			if rt.deps.LevelData() == nil {
				return false
			}
			var priced bool
			cost, priced = rt.deps.LevelData().SkillPointCost(current)
			if !priced {
				return false
			}
		}
		available := coercePoints(character.SkillPoints)
		if available < cost {
			refusal = wire.ErrCodeMasterySkillPoints
			return false
		}
		available -= cost
		character.SkillPoints = &available
		remainingSP = available

		for i := range character.Masteries {
			if character.Masteries[i].ID == request.MasteryID {
				character.Masteries[i].Level = newLevel
				trained = true
				break
			}
		}
		if !trained {
			// The record vanished between the read above and the door;
			// roll the spend back rather than charge for nothing.
			restored := available + cost
			character.SkillPoints = &restored
		}
		return trained
	})
	if !trained {
		return masteryRefusal(refusal)
	}

	return OpResult{Frames: []wire.Frame{
		{
			Opcode:  wire.OpMasteryLevelUpResponse,
			Payload: wire.EncodeMasteryLevelUpAck(request.MasteryID, uint8(newLevel)),
		},
		{
			// Absolute SP, silently: the ack above is the player-visible
			// feedback, and the notify flag's gain/loss toast would read
			// as a penalty for what is a purchase.
			Opcode:  wire.OpPointsUpdate,
			Payload: wire.EncodePointsSkillUpdate(uint32(clampSkillPoints(remainingSP)), false),
		},
	}}
}

/*
================
masteryRefusal

A refused training request emits only its error acknowledgement.
================
*/
func masteryRefusal(errorCode uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{
		Opcode:  wire.OpMasteryLevelUpResponse,
		Payload: wire.EncodeMasteryLevelUpError(errorCode),
	}}}
}

// ---- 0x72CB skill learn ----

/*
==================
HandleSkillLearn

HandleSkillLearn learns one skill by skilldata id (0x72CB -> 0xB2CB,
plus the absolute SP refresh on 0x30B3 type 2 - the ack handler
sub_75bb20 never touches the client's SP, so without the refresh its
display goes stale).

The native 59E450 validator checks row/SP eligibility, masteries, STR/INT,
country, the exact next rank, three prerequisite groups and finally SP.
Keep this order: simultaneous failures must return the same refusal byte.
Chain children remain internal execution records rather than learned entries.
The authority door owns all character reads, SP debit and rank replacement.

==================
*/
func (rt *Runtime) HandleSkillLearn(divisionID string, character *enterworld.Character, payload []byte) OpResult {
	request, err := wire.DecodeSkillLearnRequest(payload)
	if err != nil || character == nil {
		return skillLearnRefusal(wire.ErrCodeSkillLearnRefused)
	}

	// The row is authority DATA, not a formula: an unavailable table
	// refuses rather than learns free.
	if rt.deps.SkillData() == nil {
		return skillLearnRefusal(wire.ErrCodeSkillLearnRefused)
	}
	row, known := rt.deps.SkillData().SkillByID(request.SkillID)
	if !known || row.SPCost == 0 {
		// 59E47D / 59E4A3: absent and zero-SP rows are not trainable.
		return skillLearnRefusal(skillLearnUnavailable)
	}

	// Gate 2: chain sub-rows are never learnable - the retail client can
	// only compose 0x72CB with a chain's ROOT id, and the sub-rows resolve
	// from the root via the col-9 link at display/use time (see the gate
	// list above). Refusing here keeps the learned list root-only, the
	// only state the retail client can hold.
	if row.ChainSub {
		return skillLearnRefusal(wire.ErrCodeSkillLearnRefused)
	}

	// Gate 7 + the write, inside one door closure: the SP check cannot go
	// stale between reading and spending. The learned-list write mirrors
	// the client's own model (sub_8509f0): REPLACE the group's previous
	// entry on upgrade, append on a first learn.
	granted := false
	refusal := wire.ErrCodeSkillLearnRefused
	remainingSP := int64(0)
	var statBlock []byte
	committed := rt.deps.Update(character, "skill-learn", func() bool {
		if character.DeletePending {
			return false
		}
		for _, requirement := range row.Masteries {
			if requirement.ID == 0 {
				continue
			}
			level, exists := enterworld.MasteryLevel(character, requirement.ID)
			if !exists {
				return false
			}
			if level < requirement.Level {
				refusal = skillLearnMasteryLevelRefusal
				return false
			}
		}
		if enterworld.CharacterStrength(character) < row.ReqStr {
			refusal = wire.ErrCodeSkillLearnStr
			return false
		}
		if enterworld.CharacterIntellect(character) < row.ReqInt {
			refusal = wire.ErrCodeSkillLearnInt
			return false
		}
		// 59E579..59E591: country 3 is unrestricted; other bytes must match.
		if row.RequiredRace != enterworld.SkillRaceAny && int(row.RequiredRace) != enterworld.NativeCountryByte9C(character) {
			refusal = skillLearnRaceRefusal
			return false
		}
		if rt.learnedGroupLevel(character, row.Group) != row.Level-1 {
			refusal = skillLearnRankRefusal
			return false
		}
		for _, requirement := range row.Prerequisites {
			if requirement.ID == 0 {
				continue
			}
			level := rt.learnedGroupLevel(character, requirement.ID)
			if level == 0 {
				refusal = skillLearnPrerequisiteMissing
				return false
			}
			if level < requirement.Level {
				refusal = skillLearnPrerequisiteLevel
				return false
			}
		}

		available := coercePoints(character.SkillPoints)
		if available < row.SPCost {
			refusal = wire.ErrCodeSkillLearnSP
			return false
		}
		next := character.Snapshot()
		refreshStats := passiveChangesStats(row)
		available -= row.SPCost
		next.SkillPoints = &available
		remainingSP = available

		replaced := false
		for i, learnedID := range next.Skills {
			if existing, ok := rt.deps.SkillData().SkillByID(learnedID); ok && existing.Group == row.Group {
				refreshStats = refreshStats || passiveChangesStats(existing)
				next.Skills[i] = row.ID
				replaced = true
				break
			}
		}
		if !replaced {
			next.Skills = append(next.Skills, row.ID)
		}

		if refreshStats {
			display, err := rt.playerBaseStats(next)
			if err != nil {
				log.Warnf("progression: skill learn stat derivation refused - %v", err)
				return false
			}
			statBlock = enterworld.BuildLoginStatBlock(next, display)
		}
		character.SkillPoints = next.SkillPoints
		character.Skills = next.Skills
		granted = true
		return true
	})
	if !committed || !granted {
		return skillLearnRefusal(refusal)
	}

	frames := []wire.Frame{
		{
			Opcode:  wire.OpSkillLearnResponse,
			Payload: wire.EncodeSkillLearnAck(row.ID),
		},
		{
			// Absolute SP, silently: the ack above is the player-visible
			// feedback, and the notify flag's gain/loss toast would read
			// as a penalty for what is a purchase (the mastery-training
			// posture).
			Opcode:  wire.OpPointsUpdate,
			Payload: wire.EncodePointsSkillUpdate(uint32(clampSkillPoints(remainingSP)), false),
		},
	}
	if statBlock != nil {
		frames = append(frames, wire.Frame{Opcode: wire.OpBaseStats, Payload: statBlock})
	}
	return OpResult{Frames: frames}
}

/*
==================
passiveChangesStats

A learned passive is a standing skill instance whose keeper writes
(594AC0) change the derived stats: the setv, hpi/mpi/hr/er, br, reat/real
and damage blocks (PassiveParameters), the flat critical (PassiveCritical)
and the defense passives. Learning or replacing one resends the stat block,
or the client keeps the old maximum MP, HP and rates until something else
refreshes them. Inferred: native rebuilds the derived parameters on any
passive learn, as withdrawal (59FA85) does on any removal.
==================
*/
func passiveChangesStats(row enterworld.SkillRow) bool {
	return row.PassiveParameters.Pinned || row.PassiveCritical.Pinned || row.PassiveDefense.Pinned
}

/*
==================
learnedGroupLevel

learnedGroupLevel answers the character's current learned level of a
skill group (0 = not learned). The persisted list holds one id per
group (the client's replace-on-upgrade model), but the walk still takes
the highest resolvable match rather than trusting the invariant.
Callers hold the authority mutation door; rt.deps.SkillData() is non-nil.
==================
*/
func (rt *Runtime) learnedGroupLevel(character *enterworld.Character, group uint32) int64 {
	best := int64(0)
	for _, learnedID := range character.Skills {
		row, ok := rt.deps.SkillData().SkillByID(learnedID)
		if !ok || row.Group != group {
			continue
		}
		if row.Level > best {
			best = row.Level
		}
	}
	return best
}

/*
================
skillLearnRefusal

The refusal leaves SP and learned skills unchanged on both ends of the wire.
================
*/
func skillLearnRefusal(errorCode uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{
		Opcode:  wire.OpSkillLearnResponse,
		Payload: wire.EncodeSkillLearnError(errorCode),
	}}}
}

//============================================================================

/*
================
coercePoints

Absent or negative persisted pools cannot finance a progression operation.
================
*/
func coercePoints(source *int64) int64 {
	if source == nil || *source < 0 {
		return 0
	}
	return *source
}

/*
================
characterLevel

Uses the bootstrap level floor when importing an absent persisted value.
================
*/
func characterLevel(c *enterworld.Character) int64 {
	if c == nil || c.Level == nil || *c.Level < 1 {
		return 1
	}
	return *c.Level
}

/*
================
clampStatWord

STR and INT must fit the client's unsigned words at player+834/+836.
================
*/
func clampStatWord(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > enterworld.StatWordMax {
		return enterworld.StatWordMax
	}
	return value
}

/*
================
clampSkillPoints

SP uses the positive signed range shared by persistence and char-data.
================
*/
func clampSkillPoints(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > 0x7fffffff {
		return 0x7fffffff
	}
	return value
}

/*
================
nowMs

The runtime's clock in Unix milliseconds.
================
*/
func (rt *Runtime) nowMs() int64 {
	if rt.NowMs != nil {
		return rt.NowMs()
	}
	return time.Now().UnixMilli()
}
