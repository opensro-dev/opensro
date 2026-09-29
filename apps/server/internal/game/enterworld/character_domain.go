package enterworld

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/domain/charactervitals"
)

// Persisted records are owned by the neutral domain package. These aliases
// preserve bootstrap's established wire-facing API without adapters or
// copies: gameplay lanes and the authority store share the exact same record
// types and *Character identity.

const (
	RaceEurope = domain.RaceEurope
	RaceChina  = domain.RaceChina

	GenderMale   = domain.GenderMale
	GenderFemale = domain.GenderFemale

	RaceKeyEurope = domain.RaceKeyEurope
	RaceKeyChina  = domain.RaceKeyChina

	BaseStat             = domain.BaseStat
	BaseVitals           = domain.BaseVitals
	MasterySeedLevel     = domain.MasterySeedLevel
	FriendMaxCount       = domain.FriendMaxCount
	BeginnerMarkMaxLevel = domain.BeginnerMarkMaxLevel
	VisualFlagBeginner   = domain.VisualFlagBeginner
	VisualFlagEffect     = domain.VisualFlagEffect
	VisualFlagsKnownMask = domain.VisualFlagsKnownMask
)

type InventoryRow = domain.InventoryRow
type WorldSpawn = domain.WorldSpawn
type CharacterWorld = domain.CharacterWorld
type Character = domain.Character
type CharacterCOS = domain.CharacterCOS
type AvatarInventory = domain.AvatarInventory
type ActiveQuestRecord = domain.ActiveQuestRecord
type ActiveQuestContentsNode = domain.ActiveQuestContentsNode
type TrackedQuestRecord = domain.TrackedQuestRecord
type FriendRecord = domain.FriendRecord
type MissionRuntime = domain.MissionRuntime
type CharacterMastery = domain.CharacterMastery
type QuickSlotBinding = domain.QuickSlotBinding
type StartProfileSpec = domain.StartProfileSpec
type CharacterSource = domain.CharacterSource

func FriendsView(c *Character) []FriendRecord {
	return domain.FriendsView(c)
}

func SwapFriends(c *Character, next []FriendRecord) {
	domain.SwapFriends(c, next)
}

func ResolveEventGuideStateMask(c *Character) uint32 {
	return domain.ResolveEventGuideStateMask(c)
}

func ResolveVisualFlags(c *Character) uint8 {
	return domain.ResolveVisualFlags(c)
}

func ResolveCharacterRaceKey(c *Character) string {
	return domain.ResolveCharacterRaceKey(c)
}

func ResolveCharacterRaceIndex(c *Character) int64 {
	return domain.ResolveCharacterRaceIndex(c)
}

func ResolveCharacterGenderIndex(c *Character) int64 {
	return domain.ResolveCharacterGenderIndex(c)
}

func DefaultMasteries(raceKey string) []CharacterMastery {
	return domain.DefaultMasteries(raceKey)
}

func SkillLearned(c *Character, skillID uint32) bool {
	return domain.SkillLearned(c, skillID)
}

func QuickSlotKindValid(kind uint8) bool {
	return domain.QuickSlotKindValid(kind)
}

func MasteryLevel(c *Character, masteryID uint32) (int64, bool) {
	return domain.MasteryLevel(c, masteryID)
}

func CharacterStrength(c *Character) int64 {
	return domain.CharacterStrength(c)
}

func CharacterIntellect(c *Character) int64 {
	return domain.CharacterIntellect(c)
}

func NativeSexSelector1AC(c *Character) int {
	return domain.NativeSexSelector1AC(c)
}

func NativeCountryByte9C(c *Character) int {
	return domain.NativeCountryByte9C(c)
}

func RaceGenderKey(c *Character, modelCodename string) string {
	return domain.RaceGenderKey(c, modelCodename)
}

func ResolveCharacterHeightScale(c *Character) float64 {
	return domain.ResolveCharacterHeightScale(c)
}

func ResolveCharacterVolumeScale(c *Character) float64 {
	return domain.ResolveCharacterVolumeScale(c)
}

func ResolveCharacterCameraHeight(c *Character) float64 {
	return domain.ResolveCharacterCameraHeight(c)
}

func StartProfileForRace(raceKey string) StartProfileSpec {
	return domain.StartProfileForRace(raceKey)
}

func DefaultModelRefForRaceGender(raceKey string, gender int64) uint32 {
	return domain.DefaultModelRefForRaceGender(raceKey, gender)
}

func DerivedMaxHP(c *Character) int64 {
	return charactervitals.DerivedMaxHP(c)
}

func DerivedMaxMP(c *Character) int64 {
	return charactervitals.DerivedMaxMP(c)
}

func CurrentHP(c *Character) int64 {
	return charactervitals.CurrentHP(c)
}

func CurrentMP(c *Character) int64 {
	return charactervitals.CurrentMP(c)
}

func CharacterAlive(c *Character) bool {
	return charactervitals.Alive(c)
}

// The bootstrap wire builders use these coercers as package-private layout
// helpers. Persisted record ownership now lives in domain; keeping the wire
// coercion vocabulary here prevents the neutral package from exposing encoder
// internals.
func coerceInt(value *int64, min, max, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	v := *value
	if v < min {
		v = min
	}
	if v > max {
		v = max
	}
	return v
}

func coerceOptionalInt(value *int64, min, max int64) (int64, bool) {
	if value == nil {
		return 0, false
	}
	v := *value
	if v < min {
		v = min
	}
	if v > max {
		v = max
	}
	return v, true
}

func clampFloat(value, min, max float64) float64 {
	if value != value {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func derivedVitalMax(level, stat int64) int64 {
	return charactervitals.DerivedVitalMax(level, stat)
}
