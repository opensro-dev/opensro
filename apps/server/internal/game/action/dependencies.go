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

// characterDoors is one lane's pair of character commit doors: the
// immediate doors a packet handler answers through (its answer follows
// the commit), or the deferred doors only the simulation tick reaches
// (the commit joins the tick's batch, store tickbatch.go). A function
// both lanes reach takes the pair from its caller, so the lane - never
// the call site - chooses the commit timing (#2036). Handler entries
// pass handlerDoors, tick entries tickDoors.
/*
================
characterDoors
================
*/
type characterDoors struct {
	update     func(*enterworld.Character, string, func() bool) bool
	updateMany func([]*enterworld.Character, string, func() bool) bool
}

/*
================
handlerDoors

The packet-handler lane: every answer follows its commit.
================
*/
func (rt *Runtime) handlerDoors() characterDoors {
	return characterDoors{update: rt.deps.Update, updateMany: rt.deps.UpdateMany}
}

/*
================
tickDoors

The simulation-tick lane: commits join the tick's one batched
transaction.
================
*/
func (rt *Runtime) tickDoors() characterDoors {
	return characterDoors{update: rt.deps.UpdateDeferred, updateMany: rt.deps.UpdateManyDeferred}
}

// The tick lane's remaining immediate character commits, each listed
// with its reason (#3099; every other tick-path write takes tickDoors
// or a deferred door, converted under #3106 and this issue):
//
//   - relocateCharacter, its rollback and its return-scroll
//     retirement (relocation.go): one call interleaves the record
//     commit with the world-membership move, the population-session
//     and companion restores on failure, and the reentry publication
//     that renders them, so the record commits as the call answers.
//     The tick reaches it only through fortress-war expulsions and
//     instance-close returns - events, never per-player-per-tick
//     work - and splitting its commit timing by lane is a change of
//     its own when someone profiles it hot.

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
	// HalloweenAccount is the account's daily Halloween box count
	// (port-only, #2547): the live record, read or mutated only inside
	// a kill settlement's door.
	HalloweenAccount(accountID string) *domain.HalloweenBoxAccount
	// SookArenaAccount is the account's survival arena daily entry
	// count (port-only, #3322): the live record, read or mutated only
	// inside the arena entry's door.
	SookArenaAccount(accountID string) *domain.SookArenaAccount
	CloseWeek(divisionID string, week int64, label string) bool
	WeekRankings(divisionID string) domain.JobRankings
	Read(divisionID string, fn func())
	// WriteEpoch is the record's authority write epoch (enterworld
	// Deps.WriteEpoch): the tick snapshot cache shares a copy only
	// while the epoch stands. Unwired reports false and nothing is
	// shared.
	WriteEpoch(character *domain.Character) (uint64, bool)
	// AcquireRead holds the read door as a value (enterworld
	// Deps.AcquireRead): the per-tick pet judges read their facts
	// through it without a door closure per call (#2030).
	AcquireRead(divisionID string) enterworld.ReadGuard
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
