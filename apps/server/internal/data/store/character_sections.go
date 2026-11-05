/*
===========================================================================

character_sections.go - per-section change detection for character rows

The commit used to learn that a dirty character had not changed by
encoding it and hashing the bytes (79% of encodes bought nothing,
#2141). A character's persisted fields fall into sections; the store
keeps a detached copy of each character as last committed, and a dirty
character whose sections all still equal that copy is skipped without
an encode or a hash. The row hash remains on the encode path as the
write gate and debug check, no longer as the way to find changes.

Sections are derived from the JSON tags: every persisted field belongs
to exactly one section, so coverage cannot drift when a field is
added - an unlisted field lands in the trailing rest section and is
still compared. The named groups only order the search, most volatile
first, so a changed row is usually convicted by its first section.
encoding/json is deterministic for plain data (declaration order,
sorted map keys) and domain has no custom marshalers, so equal section
values mean byte-identical rows: the skip cannot lose a save, and a
comparison that errs (NaN, nil-versus-empty) errs towards an encode.

===========================================================================
*/
package store

import (
	"reflect"

	"opensro.online/server/internal/domain"
)

// characterSectionFields groups domain.Character's persisted field
// indices by section, most volatile first. Fields are named, not
// indexed, so a struct edit cannot silently move a field between
// sections; a renamed or removed field simply stops matching and its
// successor is covered by the rest section (see characterSectionIndex).
var characterSectionFields = [][]string{
	// world: where the character stands; the tick's doors touch it most.
	{"World", "LastSeenUnixMs"},
	// vitals: hit/magic points and the clocks combat rewrites.
	{"CurrentHP", "CurrentMP", "BerserkPoints", "PK", "PremiumClock"},
	// progress: level, experience, points and gold.
	{"Level", "MaxLevel", "Experience", "ExperiencePercent", "SkillPoints", "StatPoints", "Strength", "Intellect", "Gold", "LevelReachedAt"},
	// cooldowns: the ticking timers.
	{"ItemGroupCooldowns", "OffensiveSkillCooldowns", "SharedSkillCooldowns", "SkillActionRecoveryUntilMs", "ItemUseCooldowns", "ItemCureCooldowns", "PetPotionCooldowns", "TimedSkillJobs", "FortressReturnUntilMs", "MercenarySummonUntilMs", "CampDisbandedAtMs"},
	// companions: summoned and captured creatures and their skills.
	{"ActiveCOS", "Mercenaries", "PetSkillWindows", "ParamJobs", "CompositeJobs"},
	// inventory: bags, avatar bag and stall dressing.
	{"MissionInventory", "AvatarInventory", "InventorySize", "InventoryExpansion", "BuybackNext", "StallDecoration"},
	// skills: masteries, learned skills and quick slots.
	{"Masteries", "Skills", "QuickSlots", "SkillExp"},
	// quests: mission state and quest bookkeeping.
	{"Mission", "CompletedQuestIds", "QuestCompletionCounts", "QuestSupplies", "EndedQuestIds", "ActiveQuests", "TrackedQuests", "EnterEventGroupIds"},
	// social: friends, blocks, guild and wanted state.
	{"Friends", "BlockedWhisperers", "GuildID", "MostWanted"},
}

// characterSections is characterSectionFields resolved to field
// indices, with the trailing rest section appended. Built once.
var characterSections = buildCharacterSections()

/*
================
ReleaseCommittedCopy

Forget one character's committed copy: called when the character's
world session ends (cmd/services sessionClosed, beside
ForgetCharacterSession), so retained copies track the online roster
instead of every row written since boot. The row's next touch simply
encodes as it did before the section skip existed; a missing copy
never reads as "unchanged".
================
*/
func (s *Store) ReleaseCommittedCopy(divisionID string, id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.committedRows, committedCharacterKey{division: divisionID, id: id})
}

/*
================
buildCharacterSections

Resolve the section table against domain.Character's JSON tags. A
field whose JSON tag is "-" is not persisted and never compared (its
changes must not force an encode). Every persisted field the table
does not name joins the rest section.
================
*/
func buildCharacterSections() [][]int {
	characterType := reflect.TypeOf(domain.Character{})
	persisted := map[string]int{}
	for i := 0; i < characterType.NumField(); i++ {
		field := characterType.Field(i)
		if field.Tag.Get("json") == "-" {
			continue
		}
		persisted[field.Name] = i
	}
	var sections [][]int
	claimed := map[int]bool{}
	for _, group := range characterSectionFields {
		var indices []int
		for _, name := range group {
			if index, ok := persisted[name]; ok {
				indices = append(indices, index)
				claimed[index] = true
			}
		}
		if len(indices) > 0 {
			sections = append(sections, indices)
		}
	}
	var rest []int
	for _, index := range persisted {
		if !claimed[index] {
			rest = append(rest, index)
		}
	}
	if len(rest) > 0 {
		sections = append(sections, rest)
	}
	return sections
}

/*
================
characterSectionsChanged

The sections in which current differs from the committed copy, most
volatile first; nil when the two records are value-equal and would
encode to the same row. A section stops at its first unequal field.
================
*/
func characterSectionsChanged(current, committed *domain.Character) []int {
	if current == committed {
		return nil
	}
	if current == nil || committed == nil {
		return []int{0}
	}
	currentValue := reflect.ValueOf(current).Elem()
	committedValue := reflect.ValueOf(committed).Elem()
	var changed []int
	for section, fields := range characterSections {
		for _, field := range fields {
			if !equalPersistedValues(currentValue.Field(field), committedValue.Field(field)) {
				changed = append(changed, section)
				break
			}
		}
	}
	return changed
}

/*
================
equalPersistedValues

Deep equality over plain persisted data, walked with reflect.Value so
no field value is boxed. Kinds outside the plain-data set (a future
func or channel field) report unequal: the caller encodes, which is
the safe direction, and never a comparison panic.
================
*/
func equalPersistedValues(a, b reflect.Value) bool {
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return equalPersistedValues(a.Elem(), b.Elem())
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !equalPersistedValues(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return false
		}
		if a.Kind() == reflect.Slice && a.IsNil() != b.IsNil() {
			// A nil slice and an empty one can encode differently
			// (no omitempty), so they are not value-equal here; when
			// they do encode the same, the digest gate still skips
			// the write and the caller refreshes its copy.
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !equalPersistedValues(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.Len() != b.Len() || a.IsNil() != b.IsNil() {
			return false
		}
		iter := a.MapRange()
		for iter.Next() {
			other := b.MapIndex(iter.Key())
			if !other.IsValid() || !equalPersistedValues(iter.Value(), other) {
				return false
			}
		}
		return true
	case reflect.String:
		return a.String() == b.String()
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		// NaN compares unequal to itself: an encode follows, the
		// safe direction (reflect.DeepEqual agrees).
		return a.Float() == b.Float()
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return equalPersistedValues(a.Elem(), b.Elem())
	default:
		return false
	}
}
