/*
===========================================================================

skilldata.go - the skill table: loading and projecting skilldata rows

===========================================================================
*/

package enterworld

import (
	"fmt"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/world/monster"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

// The skill-learn authority table.
//
// The native learn costs and prerequisites are NOT computed: the client
// reads them out of the shipped skilldata textdata. The client's loader
// (sub_722e20 @0x00722f78/@0x00722fc0) feeds skilldata.txt - an index of
// shard files - plus skilldata_virtual.txt through the same type-1 parser,
// and each row lands in a CSkillData record whose info block starts at
// +0x694 (sub_7f8560 returns it).
//
// The row parser is sub_7f9310: 117 sequential tab-field reads after the
// caller consumed the leading service flag, so parser read N is column
// N+1 of the 118-column shipped rows (verified: every one of the 27835
// shipped rows carries exactly 118 columns). The columns below are the
// ones the learn plane needs, each pinned by a client USE of its info
// offset, not by lore:
//
//	col 1  -> info+0x00  skill id (the 0x72CB request names it)
//	col 2  -> info+0x04  group key: sub_84ffb0 @0x0084ffb0 compares it
//	          across learned skills; sub_8509f0 @0x00850aba matches the
//	          entry to replace on upgrade
//	col 7  -> info+0x60  skill level within the group (u8): sub_84ffb0's
//	          level compare, sub_8509f0 @0x00850ac9's prev+1 == new walk
//	col 9  -> info+0x64  chain link (u32): the id of the NEXT sub-skill in
//	          a chain (1S -> 2S -> 3S -> 0). The only client read is the
//	          tooltip aggregator sub_806ee0 @0x00807009, which walks the
//	          links from the learned ROOT row - the sub-rows are never
//	          learned themselves
//	col 34/35 -> info+0xa4/0xa8  required mastery ids: sub_8507f0
//	          @0x0085087f walks exactly the two dwords 0xa4..0xa8
//	col 36/37 -> info+0xac/0xad  required LEVEL of that mastery (u8
//	          pair @0x00850889, compared against the character's mastery
//	          record level byte)
//	col 38/39 -> info+0xb0/0xb4  required STR/INT: the notice table's
//	          category-5 codes 3/4 are UIIT_STT_SKILL_LEARN_STR/
//	          INT_INSUFFICIENCY (sub_689420 @0x0068a219/@0x0068a21e).
//	          Shipped skilldata carries 0 in every row, so these gates
//	          only fire on custom data (same posture as itemdata
//	          ReqStr/ReqInt).
//	col 40..42 -> info+0xb8..0xc0  prerequisite skill GROUP ids:
//	          sub_850150 @0x008501e6 compares these three dwords against
//	          another skill's group key (info+0x04)
//	col 43..45 -> info+0xc4..0xc6  required learned LEVEL of that group
//	          (u8 triple in the same walk)
//	col 46 -> info+0xc8  the SP cost: named by the client's own assert
//	          "pSkill->pSkillData->GetData()->m_nReq_Sp" @0x00585092
//
// Deliberately a TABLE and not a formula: a missing file degrades LOUD
// and the learn gate refuses rather than guessing a cost (the
// leveldata.go posture).

/*
==================
SkillRequirement

SkillRequirement is one "needs X at level >= N" pair; ID 0 is the
shipped sentinel for "no requirement" (22642 of 27835 rows carry 0 in
the first mastery slot).
==================
*/
type SkillRequirement struct {
	ID    uint32
	Level int64
}

/*
==================
SkillAttack

SkillAttack is the first encoded `att` block in one v1.150 skill row.
CSkillData_IndexEncodedParamBlocks indexes this block and the v1.188
CFormulae helpers consume the same five-value body. Only the four values
whose server roles are pinned are named; Value5 is retained as evidence
without inventing semantics.
==================
*/
type SkillAttack struct {
	MasteryEnhancement bool               // getv MAAT; native 59E770 gates mastery scaling
	MasteryIDs         [2]uint32          // required mastery identities, not minimum training levels
	DownAttack         SkillDownAttack    // da: applied to each integer lane against motion state 8
	Parameters         SkillParameterMask // getv bindings, separate from displayed stats
	Present            bool
	Flags              uint32
	Percent            int64
	Min                int64
	Max                int64
	Value5             int64
	// ImpactCount is the number of action-result stages generated for one
	// accepted action. Retail defaults this to one, then
	// SkillActor_BuildImpactStages (v1.188 0x58e7c3..0x58e7dc) overrides it
	// from the encoded `cm` pair when cm[0] == 2. Sword base carries
	// `cm,2,2`; this is why one sword action owns two damage rows even though
	// it remains one cast/token/cooldown transaction.
	ImpactCount uint8
	// Atca is RefSkill+0x3BC (587E79): {mask, percent}. SkillCombat_
	// CalculateHitOutcome 58F52F multiplies the summed lanes by
	// (1 + percent/100) when mask shares a bit with the target's abnormal
	// mask (+0xD34). Shipped pierce rows use 0x41C0: sleep, root, slow, stun.
	Atca        bool
	AtcaMask    uint32
	AtcaPercent uint32
}

/*
================
SkillRow

SkillRow is one skilldata record's learn-plane fields.
================
*/
type SkillRow struct {
	// Validated replacement inputs, independent from executable admission.
	// A refusal is retained instead of treating a malformed program as neutral.
	Replacement        statuseffect.ReplacementDescriptor
	ReplacementPinned  bool
	ReplacementRefusal string
	// Native cast lifecycle, distinct from voluntary buff cancellation policy.
	LinkedSkillID           uint32
	CancellationDeferred    bool
	InstantSelfEffectPinned bool   // complete non-attack self effect activation
	OffenseRefusal          string // exact first failed production gate; not a full missing-feature claim
	Knockdown               SkillKnockdown
	Reqc                    SkillReqc
	// SelectorMask is scls (0x73636C73) at RefSkill+0x380. Argument 1 is bit 0,
	// installed by CSkillManager_InstallSelector while that skill is active.
	SelectorMask uint32
	Reqi         SkillReqi
	Aura         SkillAura
	// BuffModifiers are the dru / odar blocks any buff installs (594AC0).
	BuffModifiers SkillBuffModifiers
	Heal          SkillHeal
	CastGate      SkillCastGate
	Knockback     SkillKnockback
	// Abnormal is every tagRefSkill status block rolled by 590680.
	Abnormal abnormal.SkillParams
	Threat   SkillThreat
	// Column 19 (client info+8c). Research server 4AED19 accepts exactly 1.
	ContinueBasicAttack bool
	NameAttackContent   bool
	ProjectileSpeed     uint32 // source column 16; world units/second, NOT milliseconds
	ActionHandler       SkillActionHandler
	Ammunition          SkillAmmunition
	CriticalModifier    SkillCriticalModifier
	PassiveCritical     SkillPassiveCritical
	PassiveParameters   SkillPassiveParameters
	PassiveDefense      SkillPassiveDefense
	BodyStatus          SkillBodyStatus
	PositionEffect      SkillPositionEffect
	MovementModifier    SkillMovementModifier
	Summon              monster.SummonSkill
	Imbue               SkillImbue
	// Wall is the Force walls' pw block (skillwall.go).
	Wall SkillWall
	// WallBypass is ck, lfst, pdmg or pdm2 (589EE0): the attack skips a
	// defender's wall absorb record.
	WallBypass bool
	// Ck is the ck block (+0x248): no block chance for its hits (58E624).
	Ck                   bool
	Recovery             SkillRecovery
	TimedEffect          SkillTimedEffect
	Concealment          SkillConcealment
	MonsterCapture       SkillMonsterCapture
	Duplicate            SkillDuplicate
	DamageCancel         SkillDamageCancel
	MonsterSelfEffect    SkillMonsterSelfEffect
	Consumption          SkillConsumption
	DirectOffensePinned  bool
	OffensiveStagePinned bool // complete executable stage; root admission also validates every link
	// StatusCast marks a damage-free hostile status program (skillstatuscast.go):
	// its single impact is a zero-damage record that only rolls statuses.
	StatusCast bool
	// AreaBurst marks an untargeted caster-centred attack (skillareaburst.go):
	// no target, its victims are the hostile monsters around the caster.
	AreaBurst bool
	// FixedDamage marks a pdmg hit (skillfixeddamage.go): its single impact
	// deals the authored amount, and dmgt converts the damage into MP.
	FixedDamage SkillFixedDamage
	// CombatTrap is a planted hostile trap program (skilltrap.go).
	CombatTrap    SkillCombatTrap
	OffensiveArea SkillOffensiveArea
	// Native encoded alcu/luck blocks feed ParamKeeper AC/AD respectively.
	AlchemyStoneBonus     uint32
	AlchemyReinforceBonus uint32
	SpawnToken            bool
	// Raw column 8, retail CSkillData_ParseTextRow 7F9451 / info+61.
	// Unlike SpawnToken, action timing distinguishes value 2 from other values.
	ActionKind            uint8
	SpawnStatus           bool
	EffectRider           bool
	HuntingPoint          bool
	StealthDuration       bool
	EffectDurationPresent bool
	HideDetectionBuff     bool
	IndefiniteBuffTimer   bool
	EffectDurationMs      uint32
	ID                    uint32
	// Codename is the row's stable string key (col 3, e.g.
	// SKILL_CH_SWORD_BASE_01). Numeric skill ids DRIFT between client
	// versions (the published 1.188 default-skill list resolves to chain
	// sub-rows and test rows in our v1.150 table - lane6 notes 3.2), so
	// anything that must name a specific row across versions resolves by
	// codename; the creation seed (defaultskills.go) is the first user.
	Codename      string
	NameSymbol    string
	Icon          string
	BuffSecondary bool // CSkillData +0x134, encoded bbuf tag (84B2F0 / 6DF560).
	// SpeedBuff is CSkillData hste (+0x94) or hst2 (+0x98). 6DE630/6E2580
	// suppress every such primary slot except the last one with a nonzero value.
	SpeedBuff SkillSpeedBuff
	// Hide is hide (+0x1F0) and Detect is dttp (+0x1EC), both [mask, level].
	// 8608A0 marks a hide buff suppressed once the carrier's detection covers it.
	// Sight is dtt (+0x1E8): the viewer's own detection (85CC70).
	Hide, Detect, Sight SkillStatusLevel
	// DetectRange is the radius of a shape-1 efr (+0x64, 85CC70); 0 means
	// the detection reaches every distance.
	DetectRange uint32
	Group       uint32
	// Level is the skill's level within its group (col 7 / info+0x60).
	Level int64
	// ChainNext is the chain link (col 9 / info+0x64): the id of the next
	// sub-skill row of a chain, 0 for none. Pointer and target always
	// share group AND level in the shipped data, and targets never carry
	// an SP cost.
	ChainNext uint32
	// ChainSub marks a row some other row's ChainNext points at. Such a
	// row is never learnable: the client resolves it from the chain ROOT
	// (tooltip walk sub_806ee0 @0x00807009) and its skill board can only
	// compose 0x72CB with the root id (sub_588af0 @0x00588c03 ->
	// sub_7efff0 first-match; the root is first in parse order and the
	// lowest id in all 401 shipped chain sets). Computed after load, not
	// a column.
	ChainSub bool
	// ActionCastingTimeMs is column 12 / CSkillData info+0x70. Retail
	// authors monster attacks as two adjacent phases: time from action start
	// to the contact boundary, followed by ActionDurationMs recovery. The
	// action actor owns both phases; dropping this first column closes B505
	// before the BSR contact event for rows such as Movia's 1077+923 ms and
	// 1394+606 ms attacks.
	// ActionCastingTimePinned distinguishes a legitimate zero from a malformed
	// or missing cell.
	ActionCastingTimeMs     uint32
	ActionCastingTimePinned bool
	// ActionDurationMs is column 13 / CSkillData info+0x74. The shipped
	// _RefSkill schema names this Action_ActionDuration; it is the recovery
	// phase after ActionCastingTimeMs, not the complete action lifetime.
	// ActionDurationPinned distinguishes a legitimate zero from a malformed
	// or missing cell.
	ActionDurationMs     uint32
	ActionDurationPinned bool
	// CoolTimeMs is column 14 / CSkillData info+0x78. The v1.150 client
	// reads this exact dword to arm the skill cooldown after an accepted
	// action; the server uses the same value as the repeat cadence of base
	// attack intent. TimingPinned distinguishes a legitimate zero from a
	// malformed/missing cell.
	CoolTimeMs uint32
	// 7F94FB stores column 18 at info+88; 84B0E0 reads its high byte (+8B).
	CoolTimeGroup uint8
	TimingPinned  bool
	// ActionRange is column 21. Monster default skills carry the authored
	// range here (melee examples 4..10, Bandit Archer 130). EU caster base
	// attacks author 150; zero-range basics delegate to equipped RefItem
	// range. ActionRangePinned preserves that
	// distinction from a malformed cell.
	ActionRange       float64
	ActionRangePinned bool
	// Masteries are the two required-mastery slots (cols 34/36, 35/37).
	Masteries [2]SkillRequirement
	// ReqStr/ReqInt gate on the character's STR/INT words (cols 38/39;
	// all-zero in shipped data).
	ReqStr int64
	ReqInt int64
	// Prerequisites are the three required-group slots (cols 40+43,
	// 41+44, 42+45): the character must hold a learned skill of that
	// GROUP at that level or higher.
	Prerequisites [3]SkillRequirement
	// SPCost is m_nReq_Sp (col 46).
	SPCost int64
	// TargetRequired is RefSkill+0x97 / column 22. +0x98 is TargetType_Animal
	// (column 23), not this byte.
	TargetRequired bool
	// Targets is columns 22..32, the bytes Skill_ValidateTargetPermissions reads.
	Targets SkillTargets
	// RequiredWeaponKinds are info+0xd8/+0xdc / columns 50/51. Values map
	// directly to the equipped weapon's TID4; 255 is the empty sentinel and
	// kind 1 is unarmed.
	RequiredWeaponKinds [2]uint8
	// Attack is the primary encoded attack block beginning at column 69.
	// Runtime currently accepts only rows whose primary block is `att`; an
	// absent or different block fails closed.
	Attack SkillAttack
	// CombatPinned is true only when target shape, both weapon-kind cells,
	// and the complete primary attack block were present and valid. It keeps
	// malformed combat-tail cells distinct from their legal zero/sentinel
	// values.
	CombatPinned bool
	// VoluntaryCancelBlocked is the encoded `nbuf` marker. The v1.188
	// SkillData_ParseEncodedParams writer stores the marker body pointer at
	// record+0x35c; ActiveCharacterEffect_RequestStop(effect, false) refuses a
	// client stop while it is present unless the live effect's source
	// descriptor explicitly overrides that protection. This belongs to skill
	// data, not to a UI-provided "cancelable" flag.
	BuffCancelConfirm      bool
	BuffCancelInstance     bool
	VoluntaryCancelBlocked bool
}

/*
==================
ActionLifecycleMs

ActionLifecycleMs returns the complete authored action-actor lifetime.
The sum is widened before addition so valid u32 source cells cannot wrap.
This is the server B505/behavior deadline contract; BAN duration remains a
client presentation input and is never consulted here.
==================
*/
func (r SkillRow) ActionLifecycleMs() (uint64, bool) {
	if !r.ActionCastingTimePinned || !r.ActionDurationPinned {
		return 0, false
	}
	return uint64(r.ActionCastingTimeMs) + uint64(r.ActionDurationMs), true
}

/*
==================
SkillDataSource

SkillDataSource resolves skilldata rows. A nil source means the table
never loaded, which the skill-learn gate treats as "cannot price or
verify a learn" (refuse), never as "free".
==================
*/
type SkillDataSource interface {
	// SkillByID answers the row for a skill id; ok=false when the
	// shipped data has no such row, which must refuse the learn.
	SkillByID(id uint32) (SkillRow, bool)
}

// Column indices (0-based over the full 118-column row; see the header
// comment for the parser-offset derivation and use-site pins).
const (
	skilldataColID             = 1
	skilldataColGroup          = 2
	skilldataColCodename       = 3
	skilldataColLevel          = 7
	skilldataColChainNext      = 9
	skilldataColActionCasting  = 12
	skilldataColActionDuration = 13
	skilldataColCoolTimeMs     = 14
	skilldataColActionRange    = 21
	skilldataColReqMastery1    = 34
	skilldataColReqMastery2    = 35
	skilldataColReqMasteryLv1  = 36
	skilldataColReqMasteryLv2  = 37
	skilldataColReqStr         = 38
	skilldataColReqInt         = 39
	skilldataColReqGroup1      = 40
	skilldataColReqGroup2      = 41
	skilldataColReqGroup3      = 42
	skilldataColReqGroupLv1    = 43
	skilldataColReqGroupLv2    = 44
	skilldataColReqGroupLv3    = 45
	skilldataColReqSP          = 46
	skilldataColTargetRequired = 22
	skilldataColWeaponKind1    = 50
	skilldataColWeaponKind2    = 51
	skilldataColActionHandler  = 68
	skilldataColPrimaryTag     = 69
	skilldataColAttackFlags    = 70
	skilldataColAttackPercent  = 71
	skilldataColAttackMin      = 72
	skilldataColAttackMax      = 73
	skilldataColAttackValue5   = 74
	skilldataColEncodedTail    = 69
	skilldataMinColumns        = 47
)

// little-endian "att\0", as stored in the numeric skilldata parameter cell.
const skillAttackTag int64 = 0x00617474

// little-endian "cm\0\0". SkillData_ParseEncodedParams (v1.188
// 0x587905..0x587950) stores its two-value body at SkillDataRecord+0x288;
// SkillActor_BuildImpactStages consumes shape [2, impactCount].
const skillMultiImpactTag int64 = 0x00006d63

// multi-character constant `nbuf` as compared by the v1.188 skill-data
// encoded-parameter parser (0x588bb0). Its body pointer becomes record+0x35c,
// the non-forced effect-stop protection checked at 0x59eff0.
const skillNoVoluntaryCancelTag int64 = 0x6e627566

/*
==================
TextdataSkills

TextdataSkills is the SkillDataSource over the extracted skilldata shards.
Load lets the composition root make the table a readiness requirement;
lookups remain cached and fail closed if a non-production caller skips it.
==================
*/
type TextdataSkills struct {
	dir string

	once    sync.Once
	rows    skillStorage
	plans   map[uint32]SkillExecutionPlan
	loadErr error
	// byCodename indexes the same rows by their col-3 codename (the
	// version-stable key; see SkillRow.Codename). Codenames are unique
	// across the shipped table; a duplicate keeps the FIRST row and the
	// load logs it loud, because a silently ambiguous codename could
	// mis-seed the creation list.
	byCodename map[string]uint32
	byRank     map[skillRankKey]uint32
	// shared keeps the process-wide parse alive while this loader reads its
	// resident rows; UseBoundedCache drops it.
	shared    *TextdataSkills
	sharedKey string
}

// sharedSkillParses deduplicates identical skill projections per process.
var sharedSkillParses sharedParses[TextdataSkills]

/*
================
NewTextdataSkills

NewTextdataSkills returns a lazy loader over dir (skilldata.txt index +
shards + skilldata_virtual.txt).
================
*/
func NewTextdataSkills(dir string) *TextdataSkills {
	return &TextdataSkills{dir: dir}
}

/*
==================
Load

Load materializes every indexed skill shard into memory and reports an
unusable projection. Production calls this before opening network admission,
so cleanup of the extraction cache cannot break later character creation.
==================
*/
func (t *TextdataSkills) Load() error {
	t.once.Do(t.load)
	return t.loadErr
}

/*
================
SkillByID

SkillByID implements SkillDataSource.
================
*/
func (t *TextdataSkills) SkillByID(id uint32) (SkillRow, bool) {
	t.once.Do(t.load)
	row, ok := t.rows.lookup(id)
	return row, ok
}

/*
==================
SkillByCodename

SkillByCodename resolves a row by its version-stable col-3 codename;
ok=false when the shipped table has no such row (callers fail loud -
see DefaultSkillRows).
==================
*/
func (t *TextdataSkills) SkillByCodename(codename string) (SkillRow, bool) {
	t.once.Do(t.load)
	id, ok := t.byCodename[codename]
	if !ok {
		return SkillRow{}, false
	}
	return t.rows.get(id), true
}

/*
================
Len

Len reports how many skill rows loaded (0 = textdata absent).
================
*/
func (t *TextdataSkills) Len() int {
	t.once.Do(t.load)
	return t.rows.len()
}

/*
==================
load

load adopts the process-wide parse of the same files. The parse is
immutable once published: rows, compiled plans and the codename index are
only read afterwards, and UseBoundedCache replaces fields, never contents.
==================
*/
func (t *TextdataSkills) load() {
	shards := skillShards(t.dir)
	paths := []string{filepath.Join(t.dir, "skilldata.txt")}
	for _, shard := range shards {
		paths = append(paths, filepath.Join(t.dir, shard))
	}
	t.sharedKey = textdataFingerprint(paths...)
	parsed := sharedSkillParses.get(t.sharedKey, func() *TextdataSkills {
		p := &TextdataSkills{dir: t.dir}
		p.parse(shards)
		return p
	})
	t.shared = parsed
	t.rows, t.plans, t.byCodename, t.loadErr = parsed.rows, parsed.plans, parsed.byCodename, parsed.loadErr
	t.indexLearnedRanks()
}

/*
==================
skillShards

skillShards reads skilldata.txt, an INDEX naming the shard files; the
client feeds it and skilldata_virtual.txt through the same parser
(sub_722e20 @0x00722f78 then @0x00722fc0), so the server loads the same set.
==================
*/
func skillShards(dir string) []string {
	shards := []string{}
	for _, fields := range readTextdataFile(filepath.Join(dir, "skilldata.txt")) {
		if len(fields) == 1 && fields[0] != "" {
			// The server projection canonicalizes textdata filenames to lowercase;
			// the authored index retains Windows casing (SkillData_5000.txt).
			shards = append(shards, strings.ToLower(fields[0]))
		}
	}
	return append(shards, "skilldata_virtual.txt")
}

/*
================
parse
================
*/
func (t *TextdataSkills) parse(shards []string) {
	t.rows = skillStorage{}
	t.byCodename = map[string]uint32{}

	for _, shard := range shards {
		for _, fields := range readTextdataFile(filepath.Join(t.dir, shard)) {
			if len(fields) < skilldataMinColumns {
				continue
			}
			id, ok := textdataInt(fields[skilldataColID])
			if !ok || id < 1 || id > 0xffffffff {
				continue
			}
			row := SkillRow{
				ContinueBasicAttack:    textdataU32(fields[19]) == 1,
				CancellationDeferred:   nativeSkillDefersCancellation(fields, spawnParamArity),
				NameAttackContent:      nativeNameAttackContent(fields),
				PassiveCritical:        encodedPassiveCritical(fields),
				PassiveParameters:      encodedPassiveParameters(fields),
				PassiveDefense:         encodedPassiveDefense(fields),
				Summon:                 encodedUniqueSummon(fields),
				ID:                     uint32(id),
				SpawnToken:             textdataU32(fields[8]) != 0,
				ActionKind:             uint8(textdataU32(fields[8])),
				SpawnStatus:            encodedSpawnStatus(fields),
				EffectDurationMs:       encodedEffectDuration(fields),
				EffectDurationPresent:  encodedTailContainsTag(fields, 0x64757261),
				HideDetectionBuff:      encodedPrimaryParameterEquals(fields, 0x6c6e6b73, 3, 0, false),
				IndefiniteBuffTimer:    encodedPrimaryParameterEquals(fields, 0x656672, 0, 3, true),
				BodyStatus:             encodedBodyStatus(fields),
				MovementModifier:       encodedMovementModifier(fields),
				EffectRider:            encodedEffectRider(fields),
				HuntingPoint:           encodedTailContainsTag(fields, 0x686e7470), // 84C276: hntp -> +250
				StealthDuration:        encodedStealthDuration(fields),
				AlchemyStoneBonus:      encodedAlchemyBonus(fields, 0x616c6375),
				AlchemyReinforceBonus:  encodedAlchemyBonus(fields, 0x6c75636b),
				Codename:               fields[skilldataColCodename],
				Group:                  textdataU32(fields[skilldataColGroup]),
				Level:                  textdataNonNegative(fields[skilldataColLevel]),
				ChainNext:              textdataU32(fields[skilldataColChainNext]),
				ReqStr:                 textdataNonNegative(fields[skilldataColReqStr]),
				ReqInt:                 textdataNonNegative(fields[skilldataColReqInt]),
				SPCost:                 textdataNonNegative(fields[skilldataColReqSP]),
				RequiredWeaponKinds:    [2]uint8{0xff, 0xff},
				VoluntaryCancelBlocked: encodedTailContainsTag(fields, skillNoVoluntaryCancelTag),
				BuffSecondary:          encodedTailContainsTag(fields, 0x62627566),
				BuffCancelConfirm:      encodedTailContainsTag(fields, 0x63627566),
				BuffCancelInstance:     encodedTailContainsTag(fields, 0x6c6e6b73),
				SpeedBuff:              encodedSpeedBuff(fields),
				Abnormal:               encodedAbnormalParams(fields),
				Hide:                   encodedStatusLevel(fields, 0x68696465),
				Detect:                 encodedStatusLevel(fields, 0x64747470),
				Sight:                  encodedStatusLevel(fields, 0x647474),
				DetectRange:            encodedDetectRange(fields),
			}
			// Presentation columns are absent from historical learn-only fixtures.
			if len(fields) > 62 {
				row.NameSymbol, row.Icon = fields[62], fields[61]
			}
			if actionCasting, ok := textdataInt(fields[skilldataColActionCasting]); ok &&
				actionCasting >= 0 && actionCasting <= 0xffffffff {
				row.ActionCastingTimeMs = uint32(actionCasting)
				row.ActionCastingTimePinned = true
			}
			// Flight metadata is shared by player and monster cast owners. Never
			// hide it behind parseSkillOffense's player-only eligibility gates:
			// Baroi has column 15 != 0 and no equipped-arrow consumption block.
			if speed, ok := textdataInt(fields[16]); ok && speed >= 0 && speed <= 0xffffffff {
				row.ProjectileSpeed = uint32(speed)
			}
			if len(fields) > skilldataColActionHandler {
				if kind, ok := textdataInt(fields[skilldataColActionHandler]); ok && kind >= 0 && kind <= 0xff {
					row.ActionHandler = SkillActionHandler(kind)
				}
			}
			if actionDuration, ok := textdataInt(fields[skilldataColActionDuration]); ok &&
				actionDuration >= 0 && actionDuration <= 0xffffffff {
				row.ActionDurationMs = uint32(actionDuration)
				row.ActionDurationPinned = true
			}
			if coolTime, ok := textdataInt(fields[skilldataColCoolTimeMs]); ok &&
				coolTime >= 0 && coolTime <= 0xffffffff {
				row.CoolTimeMs = uint32(coolTime)
				row.CoolTimeGroup = uint8(textdataU32(fields[18]) >> 24)
				row.TimingPinned = true
			}
			if actionRange, ok := textdataInt(fields[skilldataColActionRange]); ok &&
				actionRange >= 0 {
				row.ActionRange = float64(actionRange)
				row.ActionRangePinned = true
			}
			if len(fields) > skilldataColAttackValue5 {
				targetRequired, targetOK := textdataInt(fields[skilldataColTargetRequired])
				weapon1, weapon1OK := textdataByte(fields[skilldataColWeaponKind1])
				weapon2, weapon2OK := textdataByte(fields[skilldataColWeaponKind2])
				// Targeting and equipment belong to the skill record, including
				// heal/buff programs whose first instruction is not att.
				if targetOK && (targetRequired == 0 || targetRequired == 1) {
					row.Targets = skillTargetsFromColumns(fields)
					row.TargetRequired = row.Targets.Required
				}
				if weapon1OK && weapon2OK {
					row.RequiredWeaponKinds = [2]uint8{weapon1, weapon2}
				}
				tag, tagOK := textdataInt(fields[skilldataColPrimaryTag])
				flags, flagsOK := textdataInt(fields[skilldataColAttackFlags])
				percent, percentOK := textdataInt(fields[skilldataColAttackPercent])
				minimum, minOK := textdataInt(fields[skilldataColAttackMin])
				maximum, maxOK := textdataInt(fields[skilldataColAttackMax])
				value5, value5OK := textdataInt(fields[skilldataColAttackValue5])

				if targetOK && (targetRequired == 0 || targetRequired == 1) &&
					weapon1OK && weapon2OK &&
					tagOK && tag == skillAttackTag &&
					flagsOK && flags >= 0 && flags <= 0xffffffff &&
					percentOK && minOK && maxOK && value5OK {
					row.RequiredWeaponKinds = [2]uint8{weapon1, weapon2}
					row.Attack = SkillAttack{
						Parameters: encodedAttackParameters(fields),
						Present:    true,
						Flags:      uint32(flags),
						Percent:    percent,
						Min:        minimum,
						Max:        maximum,
						Value5:     value5,
						// Retail initializes the generated-result count to one even
						// when no `cm` block exists.
						ImpactCount: 1,
					}
					if impactCount, valid := encodedMultiImpactCount(fields); valid {
						row.Attack.ImpactCount = impactCount
					} else if encodedTailContainsTag(fields, skillMultiImpactTag) {
						// A malformed cm block reaches the retail positive-count
						// assertion. Keep the row in the learn/catalog plane, but do
						// not publish it to combat as a silently altered one-hit action.
						row.Attack.Present = false
						row.Attack.ImpactCount = 0
						log.Warnf("bootstrap: skilldata row %d (%s) has a malformed cm impact block; combat disabled for this row", row.ID, row.Codename)
					}
					if row.Attack.Present {
						row.CombatPinned = true
					}
				}
			}
			if modifier, valid := encodedCriticalModifier(fields); valid {
				row.CriticalModifier = modifier
			} else {
				// A malformed critical block must never become an ordinary attack.
				// Retain the learn/catalog row, as with malformed multi-impact data.
				row.Attack.Present = false
				row.CombatPinned = false
			}
			parseSkillOffense(fields, &row)
			row.PositionEffect = decodeSkillPosition(fields, row)
			parseSkillRecovery(fields, &row)
			parseSkillImbue(fields, &row)
			parseSkillWall(fields, &row)
			if replacement, err := compileSkillReplacement(fields); err == nil {
				row.Replacement = replacement
				row.ReplacementPinned = true
			} else {
				row.ReplacementRefusal = err.Error()
			}
			row.InstantSelfEffectPinned = row.Imbue.Pinned || instantMovementSkill(fields, row)
			parseSkillTimedEffect(fields, &row)
			parseSkillConcealment(fields, &row)
			if row.Concealment.Pinned && row.Concealment.Hide {
				// The plain-setter projection refuses levelled hides; the
				// concealment program executes them.
				row.BodyStatus.Supported = true
			}
			parseSkillMonsterCapture(fields, &row)
			parseSkillDuplicate(fields, &row)
			parseSkillMonsterSelfEffect(fields, &row)
			if row.Codename != "" {
				if firstID, dup := t.byCodename[row.Codename]; dup && firstID != row.ID {
					log.Warnf("bootstrap: skilldata codename %q is ambiguous (ids %d and %d); keeping the first - codename lookups must not seed off ambiguous rows", row.Codename, firstID, row.ID)
				} else {
					t.byCodename[row.Codename] = row.ID
				}
			}
			row.Masteries = [2]SkillRequirement{
				{ID: textdataU32(fields[skilldataColReqMastery1]), Level: textdataNonNegative(fields[skilldataColReqMasteryLv1])},
				{ID: textdataU32(fields[skilldataColReqMastery2]), Level: textdataNonNegative(fields[skilldataColReqMasteryLv2])},
			}
			row.Attack.MasteryEnhancement = encodedTailHasParameter(fields, 0x67657476, 0x4d414154)
			row.Attack.MasteryIDs = [2]uint32{row.Masteries[0].ID, row.Masteries[1].ID}
			if row.Imbue.Pinned {
				row.Imbue.Attack.MasteryEnhancement = row.Attack.MasteryEnhancement
				row.Imbue.Attack.MasteryIDs = row.Attack.MasteryIDs
			}
			row.Prerequisites = [3]SkillRequirement{
				{ID: textdataU32(fields[skilldataColReqGroup1]), Level: textdataNonNegative(fields[skilldataColReqGroupLv1])},
				{ID: textdataU32(fields[skilldataColReqGroup2]), Level: textdataNonNegative(fields[skilldataColReqGroupLv2])},
				{ID: textdataU32(fields[skilldataColReqGroup3]), Level: textdataNonNegative(fields[skilldataColReqGroupLv3])},
			}
			t.rows.set(row.ID, row)
		}
	}
	if t.rows.len() == 0 {
		t.loadErr = fmt.Errorf("skilldata not found under verified projection %s", t.dir)
		log.Warnf("bootstrap: %v; skill learning will refuse every request", t.loadErr)
		return
	}
	// Chain sub-rows are the TARGETS of other rows' links. Marking them
	// needs the whole table, so it runs after every shard loaded.
	for _, row := range t.rows.values() {
		if row.ChainNext == 0 {
			continue
		}
		if target, ok := t.rows.lookup(row.ChainNext); ok {
			target.ChainSub = true
			t.rows.set(row.ChainNext, target)
		}
	}
	// 7E9170/7E5CC0 run after the complete skill map has been loaded.
	next := make(map[uint32]uint32, t.rows.len())
	for id, row := range t.rows.values() {
		next[id] = row.ChainNext
	}
	linked, err := nativeSkillCastLinks(next)
	if err != nil {
		t.loadErr = err
		t.rows = skillStorage{}
		t.byCodename = map[string]uint32{}
		return
	}
	for id, root := range linked {
		row := t.rows.get(id)
		row.LinkedSkillID = root
		t.rows.set(id, row)
	}
	t.plans = make(map[uint32]SkillExecutionPlan, t.rows.len())
	// Compile once after chain membership is final; no sync.Once re-entry.
	for id, row := range t.rows.values() {
		t.plans[id] = compileExecutionPlan(t.rows, row)
	}
	log.Infof("bootstrap: skilldata loaded from %s (%d skill row(s))", t.dir, t.rows.len())
}

/*
================
SkillUiRow

SkillUiRow is a read-only projection of the same table used by training and combat.
The client never supplies prices or prerequisites back to the authority.
================
*/
type SkillUiRow struct {
	BuffCancel         string              `json:"buffCancel,omitempty"`
	BuffCancelInstance bool                `json:"buffCancelInstance,omitempty"`
	ReqStr             int64               `json:"reqStr,omitempty"`
	ReqInt             int64               `json:"reqInt,omitempty"`
	BuffSecondary      bool                `json:"buffSecondary,omitempty"`
	SpeedBuff          *SkillUiSpeedBuff   `json:"speedBuff,omitempty"`
	Hide               *SkillUiStatusLevel `json:"hide,omitempty"`
	Detect             *SkillUiStatusLevel `json:"detect,omitempty"`
	Sight              *SkillUiStatusLevel `json:"sight,omitempty"`
	DetectRange        uint32              `json:"detectRange,omitempty"`
	Name               string              `json:"name"`
	NameSymbol         string              `json:"nameSymbol,omitempty"`
	Icon               string              `json:"icon,omitempty"`
	SPCost             int64               `json:"spCost"`
	Trainable          bool                `json:"trainable"`
	TargetRequired     bool                `json:"targetRequired"`
	// TargetSelf marks a target-required row that also admits its caster
	// (column 26, TargetGroup_Self): the client aims a cast with nothing
	// selected at its own character. Omitted when false.
	TargetSelf    bool                `json:"targetSelf,omitempty"`
	GroundTarget  bool                `json:"groundTarget,omitempty"`
	CooldownGroup uint8               `json:"cooldownGroup,omitempty"`
	CooldownMs    uint32              `json:"cooldownMs"`
	Masteries     [2]SkillRequirement `json:"masteries"`
	Prerequisites [3]SkillRequirement `json:"prerequisites"`
}

/*
================
SkillUiSpeedBuff

SkillUiSpeedBuff is the buff-viewer speed stacking marker (6DE630).
================
*/
type SkillUiSpeedBuff struct {
	Active bool `json:"active"`
}

/*
================
SkillUiStatusLevel

SkillUiStatusLevel is a [mask, level] pair read by 8608A0 / 85CE40.
================
*/
type SkillUiStatusLevel struct {
	Mask  uint32 `json:"mask"`
	Level uint32 `json:"level"`
}

/*
================
skillUiStatusLevel
================
*/
func skillUiStatusLevel(value SkillStatusLevel) *SkillUiStatusLevel {
	if !value.Present {
		return nil
	}
	return &SkillUiStatusLevel{Mask: value.Mask, Level: value.Level}
}

/*
================
SpawnSkillRow
================
*/
type SpawnSkillRow struct {
	LinkedSkillID        uint32      `json:"linkedSkillId,omitempty"`
	CancellationDeferred bool        `json:"cancellationDeferred,omitempty"`
	NameAttackContent    bool        `json:"nameHit,omitempty"`
	UI                   *SkillUiRow `json:"ui,omitempty"`
	Level                uint8       `json:"level"`
	Group                uint32      `json:"group"`
	ID                   uint32      `json:"id"`
	Token                bool        `json:"token"`
	Status               bool        `json:"status"`
	EffectDurationMs     uint32      `json:"effectDurationMs"`
	ZeroEffectDuration   bool        `json:"zeroEffectDuration,omitempty"`
	HideDetectionBuff    bool        `json:"hideDetectionBuff,omitempty"`
	IndefiniteBuffTimer  bool        `json:"indefiniteBuffTimer,omitempty"`
	EffectRider          bool        `json:"effectRider"`
	HuntingPoint         bool        `json:"huntingPoint,omitempty"`
	StealthDuration      bool        `json:"stealthDuration,omitempty"`
}

/*
================
SpawnSkillRows
================
*/
func (t *TextdataSkills) SpawnSkillRows() []SpawnSkillRow {
	t.once.Do(t.load)
	rows := make([]SpawnSkillRow, 0, t.rows.len())
	for _, row := range t.rows.values() {
		projection := SpawnSkillRow{LinkedSkillID: row.LinkedSkillID, CancellationDeferred: row.CancellationDeferred, NameAttackContent: row.NameAttackContent, Level: uint8(row.Level), Group: row.Group, ID: row.ID, Token: row.SpawnToken, Status: row.SpawnStatus, EffectRider: row.EffectRider, EffectDurationMs: row.EffectDurationMs, ZeroEffectDuration: row.EffectDurationPresent && row.EffectDurationMs == 0, HideDetectionBuff: row.HideDetectionBuff, IndefiniteBuffTimer: row.IndefiniteBuffTimer}
		projection.HuntingPoint, projection.StealthDuration = row.HuntingPoint, row.StealthDuration
		if row.Icon != "" || strings.HasPrefix(row.Codename, "SKILL_CH_") || strings.HasPrefix(row.Codename, "SKILL_EU_") {
			projection.UI = &SkillUiRow{BuffSecondary: row.BuffSecondary, Name: row.Codename, SPCost: row.SPCost, Trainable: !row.ChainSub && row.SPCost > 0, TargetRequired: row.TargetRequired, TargetSelf: row.TargetRequired && row.Targets.Self, GroundTarget: row.PositionEffect.Pinned, CooldownMs: row.CoolTimeMs, CooldownGroup: row.CoolTimeGroup, Masteries: row.Masteries, Prerequisites: row.Prerequisites}
			projection.UI.BuffCancel = "" // Omitted means the native ordinary/direct branch.
			if row.VoluntaryCancelBlocked && !row.BuffCancelInstance {
				projection.UI.BuffCancel = "blocked"
			} else if row.BuffCancelConfirm {
				projection.UI.BuffCancel = "confirm"
			}
			projection.UI.BuffCancelInstance = row.BuffCancelInstance
			if row.SpeedBuff.Present {
				projection.UI.SpeedBuff = &SkillUiSpeedBuff{Active: row.SpeedBuff.Active}
			}
			projection.UI.Hide, projection.UI.Detect = skillUiStatusLevel(row.Hide), skillUiStatusLevel(row.Detect)
			projection.UI.Sight, projection.UI.DetectRange = skillUiStatusLevel(row.Sight), row.DetectRange
			projection.UI.NameSymbol, projection.UI.Icon = row.NameSymbol, row.Icon
			projection.UI.ReqStr, projection.UI.ReqInt = row.ReqStr, row.ReqInt
		}
		rows = append(rows, projection)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

/*
================
spawnSkillSnapshot
================
*/
func spawnSkillSnapshot(source SkillDataSource) []SpawnSkillRow {
	if source, ok := source.(interface{ SpawnSkillRows() []SpawnSkillRow }); ok {
		return source.SpawnSkillRows()
	}
	return nil
}

/*
==================
SkillActionHandler

RefSkill+0x168, column 68: the index SkillActionHandler (589B50) takes into
g_aSkillActionHandlers. The row reader fills the columns in order, so it is
the word before Param1 (+0x16C, SkillGlobal_BuildParameterIndex 58764B).
Every shipped row with a flying speed is 1 and every dura row is 3, but a
projectile may fly at speed 0 (SKILL_EU_CROSSBOW_BASE_01,
SKILL_CH_SPEAR_SHOOT_*), so the speed does not stand in for the kind.
==================
*/
type SkillActionHandler uint8

const (
	SkillActionInstant    SkillActionHandler = 0
	SkillActionProjectile SkillActionHandler = 1
	SkillActionPersistent SkillActionHandler = 3
	SkillActionContinuous SkillActionHandler = 4
)
