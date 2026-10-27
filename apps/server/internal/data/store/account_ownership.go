package store

import (
	"fmt"
	"sort"

	"opensro.online/server/internal/domain"
)

/*
================================================================================
Account ownership operations

Ownership is part of the character aggregate. Operator transfers therefore use
the same store lock and SQLite transaction boundary as gameplay mutations; an
offline tool never edits the record JSON or database tables behind the store.
================================================================================
*/

// CharacterOwnership is the immutable operator view of one live character.
type CharacterOwnership struct {
	DivisionID    string
	CharacterID   int64
	CharacterName string
	AccountID     string
}

// CharacterOwnerships returns every live character owner in stable order.
func (s *Store) CharacterOwnerships() []CharacterOwnership {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ownerships := make([]CharacterOwnership, 0)
	for divisionID, characters := range s.characters {
		for _, character := range characters {
			ownerships = append(ownerships, CharacterOwnership{
				DivisionID:    divisionID,
				CharacterID:   character.ID,
				CharacterName: character.Name,
				AccountID:     character.AccountID,
			})
		}
	}
	sortOwnerships(ownerships)
	return ownerships
}

// TransferCharacterOwnership atomically moves every live character owned by
// fromAccountID to toAccountID. It is intended for an offline operator process:
// a live gateway owns the authority lock and prevents that process from opening
// the store.
//
// Unlike gameplay's fail-open mutation doors, this administrative operation
// returns the commit error and restores the in-memory owners on failure. It
// also refuses a store carrying retained dirty state from an earlier gameplay
// write failure, because folding unrelated recovery state into an ownership
// operation would make its result ambiguous.
func (s *Store) TransferCharacterOwnership(fromAccountID, toAccountID string) ([]CharacterOwnership, error) {
	if !domain.AccountIDValid(fromAccountID) {
		return nil, fmt.Errorf("source account id is invalid")
	}
	if !domain.AccountIDValid(toAccountID) {
		return nil, fmt.Errorf("target account id is invalid")
	}
	if fromAccountID == toAccountID {
		return nil, fmt.Errorf("source and target account ids are identical")
	}
	if toAccountID == domain.ReservedAccountID {
		return nil, fmt.Errorf("target account %q is reserved", toAccountID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.changes.empty() {
		return nil, fmt.Errorf("authority has pending uncommitted gameplay changes; restart cleanly before transferring ownership")
	}

	type changedOwner struct {
		character *domain.Character
		division  string
	}
	changed := make([]changedOwner, 0)
	for divisionID, characters := range s.characters {
		for _, character := range characters {
			if character.AccountID == fromAccountID {
				changed = append(changed, changedOwner{
					character: character,
					division:  divisionID,
				})
			}
		}
	}
	if len(changed) == 0 {
		return nil, fmt.Errorf("no live characters belong to source account %q", fromAccountID)
	}

	// The failure path below drops the dirty set, so the rows the tick's
	// batch deferred are written first.
	if err := s.flushDeferredLocked(); err != nil {
		return nil, fmt.Errorf("commit deferred rows before ownership transfer: %w", err)
	}
	for _, owner := range changed {
		owner.character.AccountID = toAccountID
		s.changes.characters[owner.character] = true
	}
	if err := s.commitOnceLocked("account-ownership"); err != nil {
		for _, owner := range changed {
			owner.character.AccountID = fromAccountID
		}
		s.changes = newChangeSet()
		return nil, fmt.Errorf("commit ownership transfer: %w", err)
	}
	s.changes = newChangeSet()
	s.recordWriteSuccessLocked()

	ownerships := make([]CharacterOwnership, 0, len(changed))
	for _, owner := range changed {
		ownerships = append(ownerships, CharacterOwnership{
			DivisionID:    owner.division,
			CharacterID:   owner.character.ID,
			CharacterName: owner.character.Name,
			AccountID:     owner.character.AccountID,
		})
	}
	sortOwnerships(ownerships)
	return ownerships, nil
}

func sortOwnerships(ownerships []CharacterOwnership) {
	sort.Slice(ownerships, func(i, j int) bool {
		left, right := ownerships[i], ownerships[j]
		if left.DivisionID != right.DivisionID {
			return left.DivisionID < right.DivisionID
		}
		if left.CharacterID != right.CharacterID {
			return left.CharacterID < right.CharacterID
		}
		return left.CharacterName < right.CharacterName
	})
}
