/*
===========================================================================

skillcatalogue.go - the skill catalogue the browser client is sent

A read-only projection of the parsed skilldata (skilldata.go): the rows the
client's skill window, shortcut bars and press path read (SkillUiRow), and
the spawn-time reference rows every client needs (SpawnSkillRow). The
authority never reads anything back from it.

===========================================================================
*/

package enterworld

import (
	"sort"
	"strings"
)

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
	TargetSelf    bool   `json:"targetSelf,omitempty"`
	GroundTarget  bool   `json:"groundTarget,omitempty"`
	CooldownGroup uint8  `json:"cooldownGroup,omitempty"`
	CooldownMs    uint32 `json:"cooldownMs"`
	// ActionMs is the action actor's lifetime (ActionLifecycleMs, columns
	// 12 + 13): the client holds the caster's action state 2, and with it
	// every ground click, for this long (CIDecoSkill 8E0A23, 877240).
	// Omitted when either column is unpinned.
	ActionMs uint64 `json:"actionMs,omitempty"`
	// HaltsWalk marks an ordinary cast (activity 2, column 8): it stops the
	// caster's walk where it stands (InitiateSkillCast 59B5F6), so the
	// client ends its own walk at the press. Omitted for instant rows.
	HaltsWalk bool `json:"haltsWalk,omitempty"`
	// NeedsFooting marks a row whose cast gate is ao or pw: the server
	// refuses it while the caster is seated, behind a wall or riding
	// (Skill_ValidatePrerequisitesAndCost 58E0BF, 0x3009), so the client
	// stands no cooldown in for it while mounted.
	NeedsFooting bool `json:"needsFooting,omitempty"`
	// Range is the authored action range (column 21), omitted for a row
	// that takes its reach from the weapon. The server's reach adds both
	// bodies to it, so a target within Range is always in reach: the
	// client starts a cast's animation at the press only then.
	Range float64 `json:"range,omitempty"`
	// MP and MPPercent are the cast's authored MP cost (flat plus percent of
	// maximum MP), before the caster's consumption rate (parameter 0x8D):
	// the client does not stand a cooldown in for a press the caster cannot
	// pay for, which the server refuses with 0x3004 (58E2B1).
	MP        uint32 `json:"mp,omitempty"`
	MPPercent uint16 `json:"mpPercent,omitempty"`
	// Targets are the authored target groups (columns 22..33) as
	// SkillUiTarget bits. The native press (CGInterface_ExecuteSelected
	// ActionAtTarget 6FCD50) sends whatever is selected and animates only
	// on the server's answer; the client predicts a cast and stands its
	// cooldown in only for a target these groups admit.
	Targets uint16 `json:"targets,omitempty"`
	// HoldsCaster marks a cast whose WAIT the server never releases while
	// its object stands (a Force wall, pw): the client keeps the caster in
	// action state 2, rooted, until the object's retirement cancels it.
	HoldsCaster   bool                `json:"holdsCaster,omitempty"`
	Masteries     [2]SkillRequirement `json:"masteries"`
	Prerequisites [3]SkillRequirement `json:"prerequisites"`
	// Admit is what the client needs to replay Skill_ValidatePrerequisites
	// AndCost (58D8F0) for its own press: the native press animates only on
	// the server's answer (6FCD50), so the client predicts a cast and its
	// cooldown only when every gate it can read admits the press. Player
	// rows only.
	Admit *SkillUiAdmit `json:"admit,omitempty"`
}

/*
================
SkillUiAdmit

The 58D8F0 inputs of one row, in the order the server reads them. Gates
on state only the server holds (an rpkt buff 58DB22, the qest area 58DB38,
a transform mode 58DE1E, the dance selector 58DFF4, a knocked-down target
58D199, battle state for hide modes 1 and 2 58DF20) collapse into
ServerOnly: the client never predicts such a row.
================
*/
type SkillUiAdmit struct {
	// Nmf exempts the row from the frozen/asleep/stunned refusal (58DAEF).
	Nmf bool `json:"nmf,omitempty"`
	// ServerOnly marks a gate the client cannot evaluate.
	ServerOnly bool `json:"serverOnly,omitempty"`
	// Berserk refuses the row to a berserk caster (58DF20, 0x3031): a hide
	// gate without a trap.
	Berserk bool `json:"berserk,omitempty"`
	// LowHP keeps the row for a caster at or below 30 % HP (58DF8C, 0x3036).
	LowHP bool `json:"lowHp,omitempty"`
	// StealthStrike needs the press issued in stealth (58DFE0, 0x3034).
	StealthStrike bool `json:"stealthStrike,omitempty"`
	// Teleports is tele or tel3, refused while rooted (58E010, 0x3009).
	Teleports bool `json:"teleports,omitempty"`
	// WeaponKinds are +0xC7/+0xC8, compared with the primary weapon's TID4
	// when the row has no reqi pairs (58D480); 0xFF/0xFF admits anything.
	WeaponKinds [2]uint8 `json:"weaponKinds"`
	// Reqi are the row's equipment pairs (58D4E3); All is reqn.
	Reqi *SkillUiReqi `json:"reqi,omitempty"`
	// HP and HPPercent are the authored HP cost (58E1AC, 0x3013).
	HP        uint32 `json:"hp,omitempty"`
	HPPercent uint16 `json:"hpPercent,omitempty"`
	// Ammunition needs a stack of the weapon's ammunition in socket 7
	// (58E32D, 0x300E).
	Ammunition bool `json:"ammunition,omitempty"`
}

/*
================
SkillUiReqi
================
*/
type SkillUiReqi struct {
	All   bool        `json:"all,omitempty"`
	Pairs [][2]uint32 `json:"pairs"`
}

/*
================
skillUiAdmit

The row's 58D8F0 inputs for the client (SkillUiAdmit).
================
*/
func skillUiAdmit(row SkillRow) *SkillUiAdmit {
	gate, reqc := row.CastGate, row.Reqc
	admit := &SkillUiAdmit{
		Nmf: gate.Nmf,
		ServerOnly: gate.Rpkt || gate.Qest || gate.MschPresent || reqc.Dance || reqc.KnockedDown ||
			gate.HideGatePresent && (gate.HideGateMode == 1 || gate.HideGateMode == 2),
		Berserk:       gate.HideGatePresent && !gate.TrapPresent,
		LowHP:         reqc.LowHP,
		StealthStrike: reqc.Flag16,
		Teleports:     gate.Tele || gate.Tel3,
		WeaponKinds:   row.RequiredWeaponKinds,
		Ammunition:    row.Ammunition.Count != 0,
	}
	if row.Reqi.Present {
		admit.Reqi = &SkillUiReqi{All: row.Reqi.All, Pairs: make([][2]uint32, 0, row.Reqi.Count)}
		for _, pair := range row.Reqi.Pairs[:row.Reqi.Count] {
			admit.Reqi.Pairs = append(admit.Reqi.Pairs, [2]uint32{pair.Kind, pair.Value})
		}
	}
	if row.Consumption.Pinned {
		admit.HP, admit.HPPercent = row.Consumption.HP, row.Consumption.HPPercent
	}
	return admit
}

// SkillUiTarget bits of SkillUiRow.Targets.
const (
	SkillUiTargetSelf     = 1 << 0
	SkillUiTargetAnimal   = 1 << 1
	SkillUiTargetMonster  = 1 << 2 // Enemy_M
	SkillUiTargetPlayer   = 1 << 3 // Enemy_P
	SkillUiTargetAlly     = 1 << 4
	SkillUiTargetParty    = 1 << 5
	SkillUiTargetNeutral  = 1 << 6
	SkillUiTargetDeadBody = 1 << 7
)

/*
================
skillUiTargets
================
*/
func skillUiTargets(t SkillTargets) uint16 {
	var bits uint16
	for _, b := range []struct {
		set bool
		bit uint16
	}{
		{t.Self, SkillUiTargetSelf}, {t.Animal, SkillUiTargetAnimal}, {t.EnemyM, SkillUiTargetMonster},
		{t.EnemyP, SkillUiTargetPlayer}, {t.Ally, SkillUiTargetAlly}, {t.Party, SkillUiTargetParty},
		{t.Neutral, SkillUiTargetNeutral}, {t.DeadBody, SkillUiTargetDeadBody},
	} {
		if b.set {
			bits |= b.bit
		}
	}
	return bits
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
		if row.Icon != "" || playerSkillCodename(row.Codename) {
			projection.UI = &SkillUiRow{BuffSecondary: row.BuffSecondary, Name: row.Codename, SPCost: row.SPCost, Trainable: !row.ChainSub && row.SPCost > 0, TargetRequired: row.TargetRequired, TargetSelf: row.TargetRequired && row.Targets.Self, GroundTarget: row.PositionEffect.Pinned, CooldownMs: row.CoolTimeMs, CooldownGroup: row.CoolTimeGroup, Masteries: row.Masteries, Prerequisites: row.Prerequisites}
			projection.UI.BuffCancel = "" // Omitted means the native ordinary/direct branch.
			if row.VoluntaryCancelBlocked && !row.BuffCancelInstance {
				projection.UI.BuffCancel = "blocked"
			} else if row.BuffCancelConfirm {
				projection.UI.BuffCancel = "confirm"
			}
			projection.UI.BuffCancelInstance = row.BuffCancelInstance
			if lifecycle, pinned := row.ActionLifecycleMs(); pinned {
				projection.UI.ActionMs = lifecycle
			}
			projection.UI.HaltsWalk = row.HaltsWalk()
			projection.UI.NeedsFooting = row.CastGate.Ao || row.CastGate.Pw
			if row.ActionRangePinned && row.ActionRange > 0 {
				projection.UI.Range = row.ActionRange
			}
			// Only a player's own skill is pressed from a shortcut slot; a
			// monster row's cost would only grow the catalogue every client loads.
			if row.Consumption.Pinned && playerSkillCodename(row.Codename) {
				projection.UI.MP, projection.UI.MPPercent = row.Consumption.MP, row.Consumption.MPPercent
			}
			if playerSkillCodename(row.Codename) {
				projection.UI.Targets = skillUiTargets(row.Targets)
				projection.UI.Admit = skillUiAdmit(row)
			}
			projection.UI.HoldsCaster = row.Wall.Pinned
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
================
playerSkillCodename

Whether a codename names a Chinese or European player skill line.
================
*/
func playerSkillCodename(codename string) bool {
	return strings.HasPrefix(codename, "SKILL_CH_") || strings.HasPrefix(codename, "SKILL_EU_")
}
