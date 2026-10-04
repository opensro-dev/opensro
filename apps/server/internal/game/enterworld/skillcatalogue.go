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
	// Range is the authored action range (column 21), omitted for a row
	// that takes its reach from the weapon. The server's reach adds both
	// bodies to it, so a target within Range is always in reach: the
	// client starts a cast's animation at the press only then.
	Range         float64             `json:"range,omitempty"`
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
			if lifecycle, pinned := row.ActionLifecycleMs(); pinned {
				projection.UI.ActionMs = lifecycle
			}
			projection.UI.HaltsWalk = row.HaltsWalk()
			if row.ActionRangePinned && row.ActionRange > 0 {
				projection.UI.Range = row.ActionRange
			}
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
