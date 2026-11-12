/*
===========================================================================

training.go - the attack-pet skill trees (design.go data)

Port-only, not native. The owner approved this PvE expansion on 2026-10-14;
petdesign.json is the design. Each of the four pets
(bear, wolf, penguin, crow) has 2 active and 3 passive rows of 3 skills.
A pet earns a skill point at level 1 and one more every PointsPerPetLevels
levels; a skill level costs one point and needs its pet level. All learned
actives of a row share one cooldown (timer, attacks, hits taken, kills or
spells); sequential trees always use the highest learned passive in each row. Gear is
three slots of real items with blue options (gear.go). Zero state is native.

All state is fixed arrays, so a CharacterCOS snapshot copy never aliases
the live pet's training.

===========================================================================
*/
package companion

import (
	"os"
	"strings"

	"opensro.online/server/internal/domain"
)

const EnvAttackPetExpansion = "SRO_ATTACK_PET_EXPANSION"

// Cooldown kinds (the design's "Trigger / cooldown kind").
const (
	TriggerTimer   = "Timer"
	TriggerAttacks = "Attacks"
	TriggerHits    = "Hits taken"
	TriggerKills   = "Kills"
	TriggerSpells  = "Spells"
)

/*
================
AttackPetExpansionEnabled
================
*/
func AttackPetExpansionEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(EnvAttackPetExpansion)), "on")
}

/*
================
Index

A skill's slot in its pet's tree (row x 3 + option - 1): the index into
domain.PetTraining.Levels.
================
*/
func (s SkillDef) Index() int {
	return s.Row*3 + s.Option - 1
}

/*
================
PetSkill

A skill of this pet by ID; false for another pet's skill.
================
*/
func PetSkill(p *domain.CharacterCOS, id uint8) (SkillDef, bool) {
	s, ok := Skill(id)
	if p.Training.TreeVersion >= 2 {
		ok = false
		for _, candidate := range D.TreeSkills {
			if candidate.ID == id {
				s, ok = candidate, true
				break
			}
		}
	}
	if !ok || s.Pet != PetKind(p.Codename) || s.Index() < 0 || s.Index() >= len(p.Training.Levels) {
		return SkillDef{}, false
	}
	return s, true
}

/*
================
Level
================
*/
func Level(p *domain.CharacterCOS, s SkillDef) uint8 {
	return p.Training.Levels[s.Index()]
}

/*
================
EffectiveLevel

A learned skill's level raised by worn "+skills" blues (D2 style, it may
pass MaxLevel); 0 stays unlearned.
================
*/
func EffectiveLevel(p *domain.CharacterCOS, s SkillDef, m Modifiers) uint8 {
	l := Level(p, s)
	if l == 0 {
		return 0
	}
	bonus := m.SkillBonus
	if p.Training.TreeVersion >= 2 {
		bonus += m.RowSkillBonus[s.Row]
	}
	return uint8(min(255, int(l)+bonus))
}

/*
================
NextLevelRequirement

The pet level the skill's next level needs; 0 at its maximum.
================
*/
func NextLevelRequirement(s SkillDef, current uint8) uint8 {
	if current >= s.MaxLevel {
		return 0
	}
	if s.TierEnd > s.UnlockLevel && s.MaxLevel > 1 {
		return s.UnlockLevel + uint8((int(current)*int(s.TierEnd-s.UnlockLevel)+int(s.MaxLevel)-2)/(int(s.MaxLevel)-1))
	}
	return uint8(min(255, int(s.UnlockLevel)+int(current)*int(D.Rules.PetLevelsPerSkillLevel)))
}

/*
================
TrainingPoints

Free points: earned by level plus operator bonus, less every learned level.
================
*/
func TrainingPoints(p *domain.CharacterCOS) int {
	spent := 0
	for _, l := range p.Training.Levels {
		spent += int(l)
	}
	return max(0, 1+int(p.Level)/int(D.Rules.PointsPerPetLevels)+int(p.Training.BonusPoints)-spent)
}

/*
================
LearnRefusal

Why the skill's next level cannot be learned now; "" when it can.
================
*/
func LearnRefusal(p *domain.CharacterCOS, id uint8) string {
	s, ok := PetSkill(p, id)
	if !ok {
		return "This pet cannot learn that skill."
	}
	current := Level(p, s)
	if p.Training.TreeVersion >= 2 && s.Option > 1 {
		previous, exists := PetSkill(p, id-1)
		if !exists || Level(p, previous) < previous.MaxLevel {
			return "Max the previous tier before learning " + s.Name + "."
		}
	}
	switch {
	case !s.Live():
		return s.Name + " is not available yet."
	case current >= s.MaxLevel:
		return s.Name + " is at its maximum level."
	case p.Level < NextLevelRequirement(s, current):
		return "The pet's level is too low for the next level of " + s.Name + "."
	case TrainingPoints(p) == 0:
		return "The pet has no free skill points."
	}
	return ""
}

/*
================
Learn

Raises one skill a level; sequential passives activate their newest learned book.
Caller owns the transaction and combat admission.
================
*/
func Learn(p *domain.CharacterCOS, id uint8) string {
	if refusal := LearnRefusal(p, id); refusal != "" {
		return refusal
	}
	s, _ := PetSkill(p, id)
	p.Training.Levels[s.Index()]++
	if !s.Active && (p.Training.TreeVersion >= 2 || p.Training.Slotted[s.Row-ActiveRowCount] == 0) {
		p.Training.Slotted[s.Row-ActiveRowCount] = uint8(s.Option)
	}
	return ""
}

/*
================
Queue

The owner activates a learned action: the pet uses it on its next attack
once its row is ready. Asking again for the queued action cancels it.
================
*/
func Queue(p *domain.CharacterCOS, id uint8, nowMs int64) string {
	s, ok := PetSkill(p, id)
	if !ok || !s.Active || Level(p, s) == 0 || !s.Live() {
		return "Only a learned action can be used."
	}
	if p.Training.Queued == id {
		p.Training.Queued = 0
		return ""
	}
	if !RowReady(&p.Training, s.Row, nowMs) {
		return s.Name + " is not ready: its row is on cooldown."
	}
	p.Training.Queued = id
	return ""
}

/*
================
Slot

Makes a learned passive its row's active passive.
================
*/
func Slot(p *domain.CharacterCOS, id uint8) string {
	if p.Training.TreeVersion >= 2 {
		return "Passive tiers activate automatically; the highest learned tier is always active."
	}
	s, ok := PetSkill(p, id)
	if !ok || s.Active || Level(p, s) == 0 {
		return "Only a learned passive can be slotted."
	}
	p.Training.Slotted[s.Row-ActiveRowCount] = uint8(s.Option)
	return ""
}

/*
================
Refund

Free retraining keeps the gear and the running row cooldowns.
================
*/
func Refund(p *domain.CharacterCOS) {
	p.Training.Levels = [15]uint8{}
	p.Training.Slotted = [3]uint8{}
	p.Training.Queued = 0
}

/*
================
SlottedPassive

The active passive of a passive row; false when the row has none.
================
*/
func SlottedPassive(p *domain.CharacterCOS, row int) (SkillDef, bool) {
	option := p.Training.Slotted[row-ActiveRowCount]
	var selected SkillDef
	for _, s := range SkillsForPet(p) {
		if s.Row == row && (p.Training.TreeVersion >= 2 || s.Option == int(option)) &&
			Level(p, s) > 0 && s.Live() && s.Option > selected.Option {
			selected = s
		}
	}
	return selected, selected.ID != 0
}

/*
================
Passive

The slotted passive of this pet with the given name and its effective
level; false when it is not slotted.
================
*/
func Passive(p *domain.CharacterCOS, name string, m Modifiers) (SkillDef, uint8, bool) {
	for row := ActiveRowCount; row < RowCount; row++ {
		if s, ok := SlottedPassive(p, row); ok && s.EffectName() == name {
			return s, EffectiveLevel(p, s, m), true
		}
	}
	return SkillDef{}, 0, false
}

//============================================================================

/*
================
RowReady

Whether an active row may fire: its timer has passed and its counter has
run out.
================
*/
func RowReady(t *domain.PetTraining, row int, nowMs int64) bool {
	r := t.Rows[row]
	if r.Capacity > 0 {
		return AdvanceCharges(r, nowMs).Charges > 0
	}
	return r.ReadyAtMs <= nowMs && r.Left == 0
}

/*
================
StartRow

Puts the whole row on the used skill's cooldown, shortened by the charm,
blue options and Arcane Haste (Modifiers.CooldownPct).
================
*/
func StartRow(t *domain.PetTraining, row int, s SkillDef, level uint8, nowMs int64, m Modifiers) {
	if s.EffectName() == BroodSkill {
		r := BroodRow(t.Rows[row], s, level, nowMs, m)
		if r.Charges > 0 {
			r.Charges--
		}
		if r.ReadyAtMs == 0 {
			r.ReadyAtMs = nowMs + r.RechargeMs
		}
		t.Rows[row] = r
		return
	}
	value := s.CooldownAt(level) * max(0, 100-m.CooldownPct) / 100
	if s.Trigger == TriggerTimer {
		t.Rows[row] = domain.PetRowCooldown{ReadyAtMs: nowMs + int64(value*1000)}
		return
	}
	t.Rows[row] = domain.PetRowCooldown{Kind: s.Trigger, Left: uint16(max(1, value+0.5))}
}

/*
================
CountEvent

One event (an attack, a hit taken, a kill) advances every row waiting on
that kind. A penguin's spells are its attacks.
================
*/
func CountEvent(t *domain.PetTraining, kind string) {
	for row := range t.Rows {
		r := &t.Rows[row]
		if r.Left > 0 && (r.Kind == kind || kind == TriggerAttacks && r.Kind == TriggerSpells) {
			r.Left--
		}
	}
}
