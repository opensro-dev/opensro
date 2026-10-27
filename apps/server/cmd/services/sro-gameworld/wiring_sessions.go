/*
===========================================================================

wiring_sessions.go - admitted character and session teardown ordering

Exclusive binding precedes session-owned gameplay and public chat replay.
Close hooks release transient state without deleting durable authority.

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/social/guild"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/transport"
)

/*
================
installSessionLifecycle
================
*/
func (game *gameplayPlane) installSessionLifecycle(hub *transport.Hub, authorityStore *store.Store) {
	// Port-only: the economy ledger's committed gold per reason.
	hub.SetEconomyMetrics(authorityStore.EconomyCounters)
	hub.SetStoreLockMetrics(func() any { return authorityStore.LockTiming() })
	// Attack pet fights that ended on a dead or departed target (petdiagnostics.go).
	hub.SetPetCombatMetrics(game.items.PetCombatCounters)
	// Players a non-walk path placed inside a closed object (placementguard.go).
	hub.SetPlacementMetrics(game.items.PlacementCounters)
	game.deps.LockPublication = game.items.LockPublication
	game.deps.OnWorldBound = func(
		session *transport.Session,
		divisionID string,
		character *enterworld.Character,
	) {
		game.worldBound(hub, authorityStore, session, divisionID, character)
	}
	// The entry batch carries the 0x32C4 seed: the guild lane records the
	// block in the same order as the send (guild/baseline.go).
	game.deps.PublishEntry = guild.EntryPublisher(func() domain.GuildStore { return game.deps.Guilds })
	hub.OnSessionClose(func(session *transport.Session, cause error) {
		game.sessionClosed(session, cause)
		guild.ForgetSession(session)
	})
}

/*
================
worldBound

Publish exclusive identity before opening gameplay owners; recheck deletion
after binding because its reservation is protected by a different lock.
================
*/
func (game *gameplayPlane) worldBound(
	hub *transport.Hub,
	authorityStore *store.Store,
	session *transport.Session,
	divisionID string,
	character *enterworld.Character,
) {
	key := divisionID + ":" + strings.ToLower(character.Name)
	if previous, replaced := hub.BindExclusive(key, session); replaced {
		previous.ClearGameplayContext()
		log.Infof(
			"transport: session %d replaced session %d as %s",
			session.ID,
			previous.ID,
			key,
		)
	}
	if session.Evicted() {
		log.Infof(
			"transport: session %d evicted during world bind of %s; skipping session state",
			session.ID,
			key,
		)
		return
	}

	// Delete reservation and world bind publish under different locks. Once
	// the bind is visible, re-read durable state so at least one side observes
	// the other and a delete-pending character can never remain playable.
	deletePending := false
	authorityStore.ReadState(func() {
		deletePending = character.DeletePending
	})
	if deletePending {
		session.ClearGameplayContext()
		session.CloseWhenDrained(transport.ByeReasonNormal)
		log.Warnf(
			"transport: session %d bound delete-pending character %s; closing",
			session.ID,
			key,
		)
		return
	}

	game.items.BeginCommerceSession(divisionID, character, session.ID)
	game.items.BindPetSession(divisionID, character, session.ID)
	game.items.BindRecoverySession(divisionID, character, session.ID)
	game.movement.WorldBound(session, divisionID, character)
	community.FriendWorldBound(game.deps, game.presence, divisionID, character)
	game.parties.WorldBound(session, divisionID, character)
	game.matches.WorldBound(divisionID, character)
	game.guildInvites.WorldBound(divisionID, character)
	game.unions.DropPendingInvite(divisionID, character.Name)
	game.guildWars.DropPendingInvite(divisionID, character.Name)
	game.items.AbandonExchange(divisionID, character.Name)
	game.items.AbandonStall(divisionID, character.Name)
	game.items.DropInstanceInvite(divisionID, character.Name)
	game.mentorInvites.WorldBound(session, divisionID, character)
	var guildID int64
	var cooldown []wire.Frame
	authorityStore.ReadState(func() {
		if character.GuildID != nil {
			guildID = *character.GuildID
		}
		cooldown = action.FortressReturnCooldownFrames(character, game.items.Now().UnixMilli())
		cooldown = append(cooldown, action.MercenaryCooldownFrames(character, game.items.Now().UnixMilli())...)
	})
	game.siege.WorldBound(session, guildID)
	cooldown = append(cooldown, game.items.FortressHolderFrames(divisionID, character)...)
	cooldown = append(cooldown, game.items.FortressBattleFrames(divisionID, character)...)
	cooldown = append(cooldown, game.items.FortressRosterFrames(divisionID, character)...)
	for _, frame := range cooldown {
		_ = session.Send(frame.Opcode, frame.Payload)
	}
	game.chat.WorldBound(session, divisionID)
}

/*
================
sessionClosed

cause is the transport's close cause: a server shutdown is no player's
disconnect, so the port-only trader cargo rule skips it.
================
*/
func (game *gameplayPlane) sessionClosed(session *transport.Session, cause error) {
	community.FriendSessionClosed(game.deps, game.presence, session)
	if game.friends != nil {
		game.friends.SessionClosed(session)
	}
	game.parties.SessionClosed(session)
	game.matches.SessionClosed(session)
	game.guildInvites.SessionClosed(session)
	game.unions.SessionClosed(session)
	game.guildWars.SessionClosed(session)
	game.mentorInvites.SessionClosed(session)

	character, divisionID, bound := enterworld.SessionCharacter(game.deps, session)
	if bound {
		game.items.EndCommerceSession(divisionID, character, session.ID)
		game.items.AbandonExchange(divisionID, character.Name)
		game.items.AbandonStall(divisionID, character.Name)
		game.items.DropInstanceInvite(divisionID, character.Name)
		game.items.DropDisconnectedCargo(divisionID, character.Name, session.ID, cause)
		game.items.ForgetCharacterSession(divisionID, character.Name, session.ID)
	}
}
