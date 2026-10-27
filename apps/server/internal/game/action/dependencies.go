/*
===========================================================================

dependencies.go - what the action runtime consumes from its composition

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// Dependencies is the character and itemdata surface consumed by action.
/*
================
Dependencies
================
*/
type Dependencies interface {
	domain.CharacterSource
	CharacterBodyRadius(character *domain.Character) (float64, bool)
	CharacterKnockdown(character *domain.Character) (flags, recoveryMs uint32, ok bool)
	Mutate(character *domain.Character, label string, fn func())
	Update(character *domain.Character, label string, update func() bool) bool
	UpdateMany(characters []*domain.Character, label string, update func() bool) bool
	// The deferred doors join the tick's batched commit (enterworld
	// Deps.MutateDeferred); only tick-only paths call them.
	MutateDeferred(character *domain.Character, label string, fn func())
	UpdateDeferred(character *domain.Character, label string, update func() bool) bool
	UpdateManyDeferred(characters []*domain.Character, label string, update func() bool) bool
	SettleTrade(characters []*domain.Character, label string, update func(*domain.TradeRewardPool) bool) bool
	CloseWeek(divisionID string, week int64, label string) bool
	WeekRankings(divisionID string) domain.JobRankings
	Read(divisionID string, fn func())
	ItemReferences() enterworld.ItemRefSource
	LevelData() enterworld.LevelDataSource
	SkillData() enterworld.SkillDataSource
	MagicOptionDefinitions() enterworld.MagicOptionSource
	SetCatalogue() enterworld.SetCatalogueSource
	GuildAuthority() enterworld.GuildStore
	// QueueCharacterRename queues a name-change scroll's rename for the
	// next login (port-only, #1689). It takes the authority store's own
	// lock, so it is called outside every character commit door.
	QueueCharacterRename(divisionID string, character *domain.Character, name string, nowMs int64) (enterworld.RenameQueueOutcome, error)
	ReentryPackets(divisionID, characterName string) ([]enterworld.Packet, bool)
	PrepareReentry(divisionID string, character *domain.Character) (enterworld.PreparedReentry, bool)
}
