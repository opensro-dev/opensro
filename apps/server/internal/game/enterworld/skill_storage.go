/*
===========================================================================

skill_storage.go - compact resident storage for skill rows

===========================================================================
*/

package enterworld

import (
	"iter"
	"opensro.online/server/internal/data/recordcache"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/world/monster"
	"unique"
)

// residentSkill shares immutable descriptors while retaining detached SkillRow reads.
// TextdataSkills is the only writer, before its sync.Once publication boundary.
type residentSkill struct {
	PositionEffect          SkillPositionEffect
	Replacement             unique.Handle[statuseffect.ReplacementDescriptor]
	ReplacementPinned       bool
	ReplacementRefusal      string
	LinkedSkillID           uint32
	CancellationDeferred    bool
	InstantSelfEffectPinned bool
	OffenseRefusal          string
	Knockdown               unique.Handle[SkillKnockdown]
	Reqc                    SkillReqc
	SelectorMask            uint32
	Reqi                    SkillReqi
	Aura                    SkillAura
	BuffModifiers           SkillBuffModifiers
	Heal                    SkillHeal
	CastGate                SkillCastGate
	Knockback               unique.Handle[SkillKnockback]
	Abnormal                unique.Handle[abnormal.SkillParams]
	Threat                  unique.Handle[SkillThreat]
	ContinueBasicAttack     bool
	NameAttackContent       bool
	ProjectileSpeed         uint32
	ActionHandler           SkillActionHandler
	Concealment             SkillConcealment
	MonsterCapture          SkillMonsterCapture
	Duplicate               SkillDuplicate
	DamageCancel            SkillDamageCancel
	Ammunition              unique.Handle[SkillAmmunition]
	CriticalModifier        unique.Handle[SkillCriticalModifier]
	PassiveCritical         unique.Handle[SkillPassiveCritical]
	PassiveParameters       unique.Handle[SkillPassiveParameters]
	PassiveDefense          unique.Handle[SkillPassiveDefense]
	BodyStatus              unique.Handle[SkillBodyStatus]
	MovementModifier        unique.Handle[SkillMovementModifier]
	Summon                  unique.Handle[monster.SummonSkill]
	Imbue                   unique.Handle[SkillImbue]
	Wall                    unique.Handle[SkillWall]
	WallBypass              bool
	Ck                      bool
	Recovery                unique.Handle[SkillRecovery]
	TimedEffect             unique.Handle[SkillTimedEffect]
	MonsterSelfEffect       unique.Handle[SkillMonsterSelfEffect]
	Consumption             unique.Handle[SkillConsumption]
	DirectOffensePinned     bool
	OffensiveStagePinned    bool
	StatusCast              bool
	OffensiveArea           unique.Handle[SkillOffensiveArea]
	AlchemyStoneBonus       uint32
	AlchemyReinforceBonus   uint32
	SpawnToken              bool
	ActionKind              uint8
	SpawnStatus             bool
	EffectRider             bool
	HuntingPoint            bool
	StealthDuration         bool
	EffectDurationPresent   bool
	HideDetectionBuff       bool
	IndefiniteBuffTimer     bool
	EffectDurationMs        uint32
	ID                      uint32
	Codename                string
	NameSymbol              string
	Icon                    string
	BuffSecondary           bool
	SpeedBuff               SkillSpeedBuff
	Hide, Detect, Sight     SkillStatusLevel
	DetectRange             uint32
	Group                   uint32
	Level                   int64
	ChainNext               uint32
	ChainSub                bool
	ActionCastingTimeMs     uint32
	ActionCastingTimePinned bool
	ActionDurationMs        uint32
	ActionDurationPinned    bool
	CoolTimeMs              uint32
	CoolTimeGroup           uint8
	TimingPinned            bool
	ActionRange             float64
	ActionRangePinned       bool
	Masteries               unique.Handle[[2]SkillRequirement]
	ReqStr                  int64
	ReqInt                  int64
	Prerequisites           unique.Handle[[3]SkillRequirement]
	SPCost                  int64
	TargetRequired          bool
	Targets                 SkillTargets
	RequiredWeaponKinds     [2]uint8
	Attack                  unique.Handle[SkillAttack]
	CombatPinned            bool
	BuffCancelConfirm       bool
	BuffCancelInstance      bool
	VoluntaryCancelBlocked  bool
}

func compactSkill(row SkillRow) residentSkill {
	return residentSkill{
		PositionEffect:          row.PositionEffect,
		Replacement:             unique.Make(row.Replacement),
		ReplacementPinned:       row.ReplacementPinned,
		ReplacementRefusal:      row.ReplacementRefusal,
		LinkedSkillID:           row.LinkedSkillID,
		CancellationDeferred:    row.CancellationDeferred,
		InstantSelfEffectPinned: row.InstantSelfEffectPinned,
		OffenseRefusal:          row.OffenseRefusal,
		Knockdown:               unique.Make(row.Knockdown),
		Reqc:                    row.Reqc,
		SelectorMask:            row.SelectorMask,
		Reqi:                    row.Reqi,
		Aura:                    row.Aura,
		BuffModifiers:           row.BuffModifiers,
		Heal:                    row.Heal,
		CastGate:                row.CastGate,
		Knockback:               unique.Make(row.Knockback),
		Abnormal:                unique.Make(row.Abnormal),
		Threat:                  unique.Make(row.Threat),
		ContinueBasicAttack:     row.ContinueBasicAttack,
		NameAttackContent:       row.NameAttackContent,
		ProjectileSpeed:         row.ProjectileSpeed,
		ActionHandler:           row.ActionHandler,
		Concealment:             row.Concealment,
		MonsterCapture:          row.MonsterCapture,
		Duplicate:               row.Duplicate,
		DamageCancel:            row.DamageCancel,
		Ammunition:              unique.Make(row.Ammunition),
		CriticalModifier:        unique.Make(row.CriticalModifier),
		PassiveCritical:         unique.Make(row.PassiveCritical),
		PassiveParameters:       unique.Make(row.PassiveParameters),
		PassiveDefense:          unique.Make(row.PassiveDefense),
		BodyStatus:              unique.Make(row.BodyStatus),
		MovementModifier:        unique.Make(row.MovementModifier),
		Summon:                  unique.Make(row.Summon),
		Imbue:                   unique.Make(row.Imbue),
		Wall:                    unique.Make(row.Wall),
		WallBypass:              row.WallBypass,
		Ck:                      row.Ck,
		Recovery:                unique.Make(row.Recovery),
		TimedEffect:             unique.Make(row.TimedEffect),
		MonsterSelfEffect:       unique.Make(row.MonsterSelfEffect),
		Consumption:             unique.Make(row.Consumption),
		DirectOffensePinned:     row.DirectOffensePinned,
		OffensiveStagePinned:    row.OffensiveStagePinned,
		StatusCast:              row.StatusCast,
		OffensiveArea:           unique.Make(row.OffensiveArea),
		AlchemyStoneBonus:       row.AlchemyStoneBonus,
		AlchemyReinforceBonus:   row.AlchemyReinforceBonus,
		SpawnToken:              row.SpawnToken,
		ActionKind:              row.ActionKind,
		SpawnStatus:             row.SpawnStatus,
		EffectRider:             row.EffectRider,
		HuntingPoint:            row.HuntingPoint,
		StealthDuration:         row.StealthDuration,
		EffectDurationPresent:   row.EffectDurationPresent,
		HideDetectionBuff:       row.HideDetectionBuff,
		IndefiniteBuffTimer:     row.IndefiniteBuffTimer,
		EffectDurationMs:        row.EffectDurationMs,
		ID:                      row.ID,
		Codename:                row.Codename,
		NameSymbol:              row.NameSymbol,
		Icon:                    row.Icon,
		BuffSecondary:           row.BuffSecondary,
		SpeedBuff:               row.SpeedBuff,
		Hide:                    row.Hide,
		Detect:                  row.Detect,
		Sight:                   row.Sight,
		DetectRange:             row.DetectRange,
		Group:                   row.Group,
		Level:                   row.Level,
		ChainNext:               row.ChainNext,
		ChainSub:                row.ChainSub,
		ActionCastingTimeMs:     row.ActionCastingTimeMs,
		ActionCastingTimePinned: row.ActionCastingTimePinned,
		ActionDurationMs:        row.ActionDurationMs,
		ActionDurationPinned:    row.ActionDurationPinned,
		CoolTimeMs:              row.CoolTimeMs,
		CoolTimeGroup:           row.CoolTimeGroup,
		TimingPinned:            row.TimingPinned,
		ActionRange:             row.ActionRange,
		ActionRangePinned:       row.ActionRangePinned,
		Masteries:               unique.Make(row.Masteries),
		ReqStr:                  row.ReqStr,
		ReqInt:                  row.ReqInt,
		Prerequisites:           unique.Make(row.Prerequisites),
		SPCost:                  row.SPCost,
		TargetRequired:          row.TargetRequired,
		Targets:                 row.Targets,
		RequiredWeaponKinds:     row.RequiredWeaponKinds,
		Attack:                  unique.Make(row.Attack),
		CombatPinned:            row.CombatPinned,
		BuffCancelConfirm:       row.BuffCancelConfirm,
		BuffCancelInstance:      row.BuffCancelInstance,
		VoluntaryCancelBlocked:  row.VoluntaryCancelBlocked,
	}
}
func (r residentSkill) value() SkillRow {
	return SkillRow{
		PositionEffect:          r.PositionEffect,
		Replacement:             r.Replacement.Value(),
		ReplacementPinned:       r.ReplacementPinned,
		ReplacementRefusal:      r.ReplacementRefusal,
		LinkedSkillID:           r.LinkedSkillID,
		CancellationDeferred:    r.CancellationDeferred,
		InstantSelfEffectPinned: r.InstantSelfEffectPinned,
		OffenseRefusal:          r.OffenseRefusal,
		Knockdown:               r.Knockdown.Value(),
		Reqc:                    r.Reqc,
		SelectorMask:            r.SelectorMask,
		Reqi:                    r.Reqi,
		Aura:                    r.Aura,
		BuffModifiers:           r.BuffModifiers,
		Heal:                    r.Heal,
		CastGate:                r.CastGate,
		Knockback:               r.Knockback.Value(),
		Abnormal:                r.Abnormal.Value(),
		Threat:                  r.Threat.Value(),
		ContinueBasicAttack:     r.ContinueBasicAttack,
		NameAttackContent:       r.NameAttackContent,
		ProjectileSpeed:         r.ProjectileSpeed,
		ActionHandler:           r.ActionHandler,
		Concealment:             r.Concealment,
		MonsterCapture:          r.MonsterCapture,
		Duplicate:               r.Duplicate,
		DamageCancel:            r.DamageCancel,
		Ammunition:              r.Ammunition.Value(),
		CriticalModifier:        r.CriticalModifier.Value(),
		PassiveCritical:         r.PassiveCritical.Value(),
		PassiveParameters:       r.PassiveParameters.Value(),
		PassiveDefense:          r.PassiveDefense.Value(),
		BodyStatus:              r.BodyStatus.Value(),
		MovementModifier:        r.MovementModifier.Value(),
		Summon:                  r.Summon.Value(),
		Imbue:                   r.Imbue.Value(),
		Wall:                    r.Wall.Value(),
		WallBypass:              r.WallBypass,
		Ck:                      r.Ck,
		Recovery:                r.Recovery.Value(),
		TimedEffect:             r.TimedEffect.Value(),
		MonsterSelfEffect:       r.MonsterSelfEffect.Value(),
		Consumption:             r.Consumption.Value(),
		DirectOffensePinned:     r.DirectOffensePinned,
		OffensiveStagePinned:    r.OffensiveStagePinned,
		StatusCast:              r.StatusCast,
		OffensiveArea:           r.OffensiveArea.Value(),
		AlchemyStoneBonus:       r.AlchemyStoneBonus,
		AlchemyReinforceBonus:   r.AlchemyReinforceBonus,
		SpawnToken:              r.SpawnToken,
		ActionKind:              r.ActionKind,
		SpawnStatus:             r.SpawnStatus,
		EffectRider:             r.EffectRider,
		HuntingPoint:            r.HuntingPoint,
		StealthDuration:         r.StealthDuration,
		EffectDurationPresent:   r.EffectDurationPresent,
		HideDetectionBuff:       r.HideDetectionBuff,
		IndefiniteBuffTimer:     r.IndefiniteBuffTimer,
		EffectDurationMs:        r.EffectDurationMs,
		ID:                      r.ID,
		Codename:                r.Codename,
		NameSymbol:              r.NameSymbol,
		Icon:                    r.Icon,
		BuffSecondary:           r.BuffSecondary,
		SpeedBuff:               r.SpeedBuff,
		Hide:                    r.Hide,
		Detect:                  r.Detect,
		Sight:                   r.Sight,
		DetectRange:             r.DetectRange,
		Group:                   r.Group,
		Level:                   r.Level,
		ChainNext:               r.ChainNext,
		ChainSub:                r.ChainSub,
		ActionCastingTimeMs:     r.ActionCastingTimeMs,
		ActionCastingTimePinned: r.ActionCastingTimePinned,
		ActionDurationMs:        r.ActionDurationMs,
		ActionDurationPinned:    r.ActionDurationPinned,
		CoolTimeMs:              r.CoolTimeMs,
		CoolTimeGroup:           r.CoolTimeGroup,
		TimingPinned:            r.TimingPinned,
		ActionRange:             r.ActionRange,
		ActionRangePinned:       r.ActionRangePinned,
		Masteries:               r.Masteries.Value(),
		ReqStr:                  r.ReqStr,
		ReqInt:                  r.ReqInt,
		Prerequisites:           r.Prerequisites.Value(),
		SPCost:                  r.SPCost,
		TargetRequired:          r.TargetRequired,
		Targets:                 r.Targets,
		RequiredWeaponKinds:     r.RequiredWeaponKinds,
		Attack:                  r.Attack.Value(),
		CombatPinned:            r.CombatPinned,
		BuffCancelConfirm:       r.BuffCancelConfirm,
		BuffCancelInstance:      r.BuffCancelInstance,
		VoluntaryCancelBlocked:  r.VoluntaryCancelBlocked,
	}
}

type skillStorage struct {
	hot     map[uint32]residentSkill
	archive *recordcache.Cache[SkillRow]
}

func (s *skillStorage) set(id uint32, row SkillRow) {
	if s.hot == nil {
		s.hot = make(map[uint32]residentSkill)
	}
	s.hot[id] = compactSkill(row)
}
func (s *skillStorage) lookup(id uint32) (SkillRow, bool) {
	if s.archive != nil {
		return s.archive.Get(id)
	}
	r, ok := s.hot[id]
	if !ok {
		return SkillRow{}, false
	}
	return r.value(), true
}
func (s *skillStorage) get(id uint32) SkillRow { r, _ := s.lookup(id); return r }
func (s *skillStorage) values() iter.Seq2[uint32, SkillRow] {
	return func(yield func(uint32, SkillRow) bool) {
		if s.archive != nil {
			for _, id := range s.archive.IDs() {
				row, _ := s.archive.Get(id)
				if !yield(id, row) {
					return
				}
			}
			return
		}
		for id, r := range s.hot {
			if !yield(id, r.value()) {
				return
			}
		}
	}
}
func (s skillStorage) SkillByID(id uint32) (SkillRow, bool) { return s.lookup(id) }
func (s skillStorage) SkillByCodename(name string) (SkillRow, bool) {
	for _, r := range s.values() {
		if r.Codename == name {
			return r, true
		}
	}
	return SkillRow{}, false
}

func (s *skillStorage) len() int {
	if s.archive != nil {
		return s.archive.Len()
	}
	return len(s.hot)
}
