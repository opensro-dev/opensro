package store

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

// RenameCharacterOffline is an operator-only maintenance door. The caller must
// open the stopped authority exclusively, before attaching runtime owners.
// Character ID/account ownership never change. Copy/commit/publish keeps a
// failed transaction from exposing a partially renamed social graph.
// This is an operator operation, not a claimed retail rename-item protocol.
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
	if s.commitFail != nil {
		return s.commitFail
	}
	updates := map[*domain.Character]domain.Character{}
	for _, c := range s.characters[division] {
		next := *c
		changed := c == selected
		if changed {
			next.Name = after
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
	tx, err := s.db.Begin()
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
	if err := replaceGroundTx(tx, ground); err != nil {
		return err
	}
	if err := upsertMetaTx(tx, metaKeyUpdatedAtMs, fmt.Sprint(s.now().UnixMilli())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for c, next := range updates {
		c.Name = next.Name
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
	s.recordWriteSuccessLocked()
	return nil
}
