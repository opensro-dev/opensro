/*
===========================================================================

character_rename.go - one rename body, two doors

Port-only, not native. The body (renameCharacterLocked) rewrites the
character row and every persisted name copy in one copy/commit/publish
transaction. The operator door (RenameCharacterOffline, driven by
cmd/operations/sro-rename-character) admits it against a stopped
authority, by name. The name-change scroll's door (QueueCharacterRename
+ ApplyPendingRenames, #1689) queues a rename at use and applies it at
the next login, before the character list is answered, holding the old
name in name_reservations for 90 days.

===========================================================================
*/
package store

import (
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

// Rename timing (owner settings, #1689): one queued rename per 30 days
// per character; the old name is held from reuse for 90 days.
const (
	renameCooldownMs    = int64(30) * 24 * 60 * 60 * 1000
	nameReservationMs   = int64(90) * 24 * 60 * 60 * 1000
	RenameCooldownDays  = 30
	NameReservationDays = 90
)

var (
	// ErrRenamePending reports a second scroll used while a rename is
	// already queued for the next login.
	ErrRenamePending = errors.New("a name change is already queued")
	// ErrRenameCooldown reports a rename queued inside the 30-day
	// cooldown from the last applied rename.
	ErrRenameCooldown = errors.New("name change is on cooldown")
	// ErrNameReserved reports a target name held in name_reservations
	// for a different character.
	ErrNameReserved = errors.New("character name is reserved")
)

// PendingRenameOutcome is one queued rename's fate at login: applied
// (OldName -> NewName) or cancelled with the machine reason the
// character-select answer carries.
/*
================
PendingRenameOutcome
================
*/
type PendingRenameOutcome struct {
	CharacterID int64
	OldName     string
	NewName     string
	Applied     bool
	Reason      string
}

// Apply-time cancellation reasons (PendingRenameOutcome.Reason).
const (
	RenameRefusedTaken    = "taken"
	RenameRefusedReserved = "reserved"
	RenameRefusedGuildWar = "guild-war"
	RenameRefusedInvalid  = "invalid"
	RenameRefusedDeleted  = "deleted"
)

/*
================
renameCharacterLocked

The rename body both doors share. The caller holds s.mu and has
admitted the rename (selected is the live record, after its new name).
Writes the character row and every persisted name copy - friend
rosters, block lists, guild member rows, letter senders, dropped-item
names - rebuilds the division's name index, and, when reservation is
non-nil, holds the old name in the same transaction. stamp adjusts the
renamed character's own next record (the queued-rename door stamps the
cooldown and clears the queue). Copy/commit/publish keeps a failed
transaction from exposing a partially renamed social graph.
================
*/
func (s *Store) renameCharacterLocked(division string, selected *domain.Character, after string, stamp func(next *domain.Character), reservation *NameReservation) error {
	if s.commitFail != nil {
		return s.commitFail
	}
	before := selected.Name
	updates := map[*domain.Character]domain.Character{}
	for _, c := range s.characters[division] {
		next := *c
		changed := c == selected
		if changed {
			next.Name = after
			if stamp != nil {
				stamp(&next)
			}
		}
		next.Friends = domain.FriendsView(c)
		for i := range next.Friends {
			if next.Friends[i].ID == selected.ID {
				next.Friends[i].Name = after
				changed = true
			}
		}
		next.BlockedWhisperers = append([]string(nil), c.BlockedWhisperers...)
		for i, name := range next.BlockedWhisperers {
			if strings.EqualFold(name, before) {
				next.BlockedWhisperers[i] = after
				changed = true
			}
		}
		if changed {
			updates[c] = next
		}
	}
	guilds := map[int64][]domain.GuildMemberRecord{}
	for id, members := range s.guildMembers[division] {
		next := append([]domain.GuildMemberRecord(nil), members...)
		for i := range next {
			if next[i].CharID == selected.ID {
				next[i].Name = after
				guilds[id] = next
			}
		}
	}
	mailboxes := map[int64][]domain.LetterRecord{}
	for id, letters := range s.mailboxes[division] {
		next := append([]domain.LetterRecord(nil), letters...)
		for i := range next {
			if strings.EqualFold(next[i].Sender, before) {
				next[i].Sender = after
				mailboxes[id] = next
			}
		}
	}
	ground := make(map[string][]domain.GroundItemRecord, len(s.ground.loadedRecords))
	for id, rows := range s.ground.loadedRecords {
		ground[id] = rows
	}
	ground[division] = append([]domain.GroundItemRecord(nil), ground[division]...)
	for i := range ground[division] {
		if strings.EqualFold(ground[division][i].DroppedBy, before) {
			ground[division][i].DroppedBy = after
		}
	}
	tx, err := s.beginTxLocked()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, next := range updates {
		if err := upsertCharacterTx(tx, division, &next); err != nil {
			return err
		}
	}
	for id, members := range guilds {
		if err := replaceGuildTx(tx, division, id, s.guilds[division][id], members); err != nil {
			return err
		}
	}
	for id, letters := range mailboxes {
		if err := replaceMailboxTx(tx, division, id, letters); err != nil {
			return err
		}
	}
	if err := replaceGroundTx(tx, &s.encodeScratch, ground); err != nil {
		return err
	}
	if reservation != nil {
		if err := insertNameReservationTx(tx, *reservation); err != nil {
			return err
		}
	}
	if err := upsertMetaTx(tx, metaKeyUpdatedAtMs, fmt.Sprint(s.now().UnixMilli())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for c, next := range updates {
		c.Name = next.Name
		c.PendingRenameName = next.PendingRenameName
		c.PendingRenameQueuedMs = next.PendingRenameQueuedMs
		c.LastRenameMs = next.LastRenameMs
		c.RenameNoticeFrom = next.RenameNoticeFrom
		domain.SwapFriends(c, next.Friends)
		c.BlockedWhisperers = next.BlockedWhisperers
	}
	s.rebuildCharacterLookupLocked(division)
	for id, members := range guilds {
		s.guildMembers[division][id] = members
	}
	for id, letters := range mailboxes {
		s.mailboxes[division][id] = letters
	}
	s.ground.loadedRecords = ground
	// The rename rewrote ground rows outside the registry's change
	// stamps (loadedRecords' DroppedBy): the next ground write diffs the
	// whole plane against them, exactly as before stamps existed (#2040).
	s.ground.revisionRecorded = false
	if reservation != nil {
		if s.nameReservations[division] == nil {
			s.nameReservations[division] = map[string]NameReservation{}
		}
		s.nameReservations[division][reservation.NameLower] = *reservation
	}
	s.recordWriteSuccessLocked()
	return nil
}

// RenameCharacterOffline is an operator-only maintenance door. The caller must
// open the stopped authority exclusively, before attaching runtime owners.
// Character ID/account ownership never change.
// This is an operator operation, not a claimed retail rename-item protocol.
/*
================
RenameCharacterOffline
================
*/
func (s *Store) RenameCharacterOffline(division, before, after string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ground.source != nil || !s.changes.empty() || s.db == nil {
		return fmt.Errorf("rename requires a clean offline authority")
	}
	if !persistedCharacterNameValid(after) {
		return ErrCharacterNameInvalid
	}
	var selected *domain.Character
	for _, c := range s.characters[division] {
		if c.Name == before {
			selected = c
		}
		if strings.EqualFold(c.Name, after) && c.Name != before {
			return ErrCharacterNameConflict
		}
	}
	if selected == nil || selected.DeletePending {
		return fmt.Errorf("active character %q not found", before)
	}
	if before == after {
		return nil
	}
	return s.renameCharacterLocked(division, selected, after, nil, nil)
}

/*
================
QueueCharacterRename

Queues a name-change scroll's rename for the character's next login.
The gameplay layer has admitted the use (the switch is on and the
session refusal states are clear); this door re-checks everything the
store owns - the name shape, no rename already queued, the 30-day
cooldown, a target no live character holds and nobody else's
reservation covers - and only then writes the pending fields. The
rename itself applies in ApplyPendingRenames, never mid-session.
================
*/
func (s *Store) QueueCharacterRename(division string, character *domain.Character, after string, nowMs int64) error {
	if !persistedCharacterNameValid(after) {
		return ErrCharacterNameInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if character == nil || s.charDivision[character] != division {
		return fmt.Errorf("rename queue: character is not a live record of division %s", division)
	}
	if character.DeletePending {
		return fmt.Errorf("rename queue: character %q is being deleted", character.Name)
	}
	if character.PendingRenameName != "" {
		return ErrRenamePending
	}
	if character.LastRenameMs != 0 && nowMs-character.LastRenameMs < renameCooldownMs {
		return ErrRenameCooldown
	}
	if strings.EqualFold(character.Name, after) {
		return fmt.Errorf("%w: %q", ErrCharacterNameConflict, after)
	}
	for _, c := range s.characters[division] {
		if c != character && strings.EqualFold(c.Name, after) {
			return fmt.Errorf("%w: %q", ErrCharacterNameConflict, after)
		}
	}
	if s.nameReservedLocked(division, after, nowMs, character.ID) {
		return fmt.Errorf("%w: %q", ErrNameReserved, after)
	}
	character.PendingRenameName = after
	character.PendingRenameQueuedMs = nowMs
	s.changes.characters[character] = true
	s.finishDoorLocked("queue-character-rename", false, []*domain.Character{character})
	return nil
}

/*
================
ApplyPendingRenames

Applies every rename the account's characters in division have
queued, in roster order. The login path calls it before the character
list is answered, so the list simply shows the outcome: the new name,
or the old name with a cancellation reason in the returned outcomes.
A rename whose checks no longer hold is cancelled (the queue clears;
the scroll is spent); a commit failure leaves the rename queued for
the next login instead of cancelling it. nameAllowed is the native
name filter's screen (nil admits every name): it runs again here
because the list a name was queued under can have changed by login.
================
*/
func (s *Store) ApplyPendingRenames(division, accountID string, nowMs int64, nameAllowed func(string) bool) []PendingRenameOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	var outcomes []PendingRenameOutcome
	for _, c := range s.characters[division] {
		if c.AccountID != accountID || c.PendingRenameName == "" {
			continue
		}
		if outcome, decided := s.applyPendingRenameLocked(division, c, nowMs, nameAllowed); decided {
			outcomes = append(outcomes, outcome)
		}
	}
	return outcomes
}

/*
================
applyPendingRenameLocked

One queued rename's fate. decided is false only when the commit
failed: the rename stays queued and no outcome is reported.
================
*/
func (s *Store) applyPendingRenameLocked(division string, c *domain.Character, nowMs int64, nameAllowed func(string) bool) (PendingRenameOutcome, bool) {
	target := c.PendingRenameName
	outcome := PendingRenameOutcome{CharacterID: c.ID, OldName: c.Name, NewName: target}
	cancel := func(reason string) (PendingRenameOutcome, bool) {
		c.PendingRenameName = ""
		c.PendingRenameQueuedMs = 0
		s.changes.characters[c] = true
		s.finishDoorLocked("cancel-pending-rename", false, []*domain.Character{c})
		outcome.Reason = reason
		return outcome, true
	}
	if c.DeletePending {
		return cancel(RenameRefusedDeleted)
	}
	if !persistedCharacterNameValid(target) {
		return cancel(RenameRefusedInvalid)
	}
	if nameAllowed != nil && !nameAllowed(target) {
		return cancel(RenameRefusedInvalid)
	}
	for _, other := range s.characters[division] {
		if other != c && strings.EqualFold(other.Name, target) {
			return cancel(RenameRefusedTaken)
		}
	}
	if s.nameReservedLocked(division, target, nowMs, c.ID) {
		return cancel(RenameRefusedReserved)
	}
	if c.GuildID != nil && s.db != nil {
		if wars, err := readGuildWars(s.db, division); err == nil {
			for _, war := range wars {
				active := war.EndMs == 0 || war.EndMs > nowMs
				if active && (war.Guilds[0] == *c.GuildID || war.Guilds[1] == *c.GuildID) {
					return cancel(RenameRefusedGuildWar)
				}
			}
		}
	}
	reservation := NameReservation{
		Division:     division,
		NameLower:    strings.ToLower(c.Name),
		ExpiresMs:    nowMs + nameReservationMs,
		FormerCharID: c.ID,
	}
	err := s.renameCharacterLocked(division, c, target, func(next *domain.Character) {
		next.PendingRenameName = ""
		next.PendingRenameQueuedMs = 0
		next.LastRenameMs = nowMs
		next.RenameNoticeFrom = c.Name
	}, &reservation)
	if err != nil {
		log.Errorf("store: applying queued rename for character %d in %s failed (left queued): %v", c.ID, division, err)
		return outcome, false
	}
	outcome.Applied = true
	return outcome, true
}
