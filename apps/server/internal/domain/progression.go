package domain

// BaseStat is the character-creation STR/INT value.
const BaseStat int64 = 20

// BaseVitals is the character-creation HP/MP value. It is also the result of
// the runtime vitality formula at level 1 with BaseStat.
const BaseVitals int64 = 200

// CharacterMastery is one learned mastery and its trained level.
type CharacterMastery struct {
	ID    uint32 `json:"id"`
	Level int64  `json:"level"`
}

var (
	chMasteryIDs = []uint32{257, 258, 259, 273, 274, 275, 276}
	euMasteryIDs = []uint32{513, 514, 515, 516, 517, 518}
)

// MasterySeedLevel is the native creation level for every racial mastery.
const MasterySeedLevel int64 = 0

// DefaultMasteries returns the complete racial creation set.
func DefaultMasteries(raceKey string) []CharacterMastery {
	ids := euMasteryIDs
	if raceKey == RaceKeyChina {
		ids = chMasteryIDs
	}
	masteries := make([]CharacterMastery, len(ids))
	for index, id := range ids {
		masteries[index] = CharacterMastery{
			ID:    id,
			Level: MasterySeedLevel,
		}
	}
	return masteries
}

/*
================
TopMasteries

The two trained masteries a party shows for a character. The native mastery
tree is ID ordered and 5A87E0 sorts it by descending trained level without
moving equal levels (5A1890 picks the first two), so ties keep the lower ID.
Untrained masteries never qualify; a missing slot is 0. The persisted slice
is neither sorted nor mutated.
================
*/
func TopMasteries(masteries []CharacterMastery) (primary, secondary uint32) {
	var first, second CharacterMastery
	better := func(a, b CharacterMastery) bool {
		return a.Level > b.Level || a.Level == b.Level && a.ID < b.ID
	}
	for _, mastery := range masteries {
		if mastery.Level <= 0 {
			continue
		}
		if better(mastery, first) {
			second, first = first, mastery
		} else if better(mastery, second) {
			second = mastery
		}
	}
	return first.ID, second.ID
}

// SkillLearned reports whether the exact skill is learned.
func SkillLearned(character *Character, skillID uint32) bool {
	if character == nil {
		return false
	}
	for _, id := range character.Skills {
		if id == skillID {
			return true
		}
	}
	return false
}

// MasteryLevel returns a mastery level and whether the mastery exists.
func MasteryLevel(character *Character, masteryID uint32) (int64, bool) {
	if character == nil {
		return 0, false
	}
	for _, mastery := range character.Masteries {
		if mastery.ID == masteryID {
			return mastery.Level, true
		}
	}
	return 0, false
}

// CharacterStrength returns the authoritative stat with the creation fallback
// used by detached fixtures.
func CharacterStrength(character *Character) int64 {
	if character == nil ||
		character.Strength == nil ||
		*character.Strength < 0 {
		return BaseStat
	}
	return *character.Strength
}

// CharacterIntellect returns the authoritative stat with the same fallback.
func CharacterIntellect(character *Character) int64 {
	if character == nil ||
		character.Intellect == nil ||
		*character.Intellect < 0 {
		return BaseStat
	}
	return *character.Intellect
}
