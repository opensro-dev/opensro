/*
===========================================================================

character_access.go - exposes store snapshots and character authority operations

===========================================================================
*/
package store

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

// Character access and mutation doors. These methods own lock acquisition,
// copy boundaries, dirty tracking, and the one-commit-per-operation contract.
/*
================
Characters
================
*/
func (s *Store) Characters() domain.CharacterSource {
	return storeCharacterSource{s: s}
}

/*
================
storeCharacterSource
================
*/
type storeCharacterSource struct{ s *Store }

// ReadState is the allocation-free authority door for callers that already
// hold a character identity. fn may read fields only while this lock is held,
// must not mutate them, and must not re-enter Store methods.
/*
================
ReadState
================
*/
func (s *Store) ReadState(fn func()) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn()
}

// ReadCharacters is the READ door, symmetric with the commit door: fn
// runs under the store lock with the division's records, so field reads
// cannot race a Mutate closure's writes. fn must be short and must not
// call store methods (the mutex does not re-enter).
//
// Aliasing contract: the slice fn receives is a fresh COPY of the live
// backing array carrying the SAME *Character pointers (the process-wide
// pointer-identity contract - never deep-copied). The deletion reaper
// compacts the live array IN PLACE (ReapMaturedDeletions rewrites
// survivors through records[:0]) and CreateCharacter appends to it, so
// handing out the live slice would let a retained header shrink,
// reorder or go stale under its holder with no synchronization; the
// copy makes retention merely STALE (a later reap or create does not
// rewrite it), the same guarantee CharactersForDivision gives. FIELD
// reads are still only synchronized inside fn - no field read may be
// trusted after fn returns; read fields back under a door.
/*
================
ReadCharacters
================
*/
func (s *Store) ReadCharacters(divisionID string, fn func(characters []*domain.Character)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	live := s.characters[divisionID]
	out := make([]*domain.Character, len(live))
	copy(out, live)
	fn(out)
}

// CharactersForDivision returns the division's live records (fresh slice,
// same pointers - append-safe against concurrent CreateCharacter).
/*
================
CharactersForDivision
================
*/
func (src storeCharacterSource) CharactersForDivision(divisionID string) []*domain.Character {
	src.s.mu.RLock()
	defer src.s.mu.RUnlock()
	live := src.s.characters[divisionID]
	out := make([]*domain.Character, len(live))
	copy(out, live)
	return out
}

// GroundSnapshotSource is the store-owned port for a live ground registry.
// The gameplay implementation owns locking and entity lifecycle; the store
// only observes revisions and immutable value snapshots at its commit door.
/*
================
GroundSnapshotSource
================
*/
type GroundSnapshotSource interface {
	Revision() uint64
	Snapshot() domain.GroundSnapshot
}

// AttachGround installs the live ground snapshot source for later commits.
// Wire it after action constructs the registry and before transport starts.
/*
================
AttachGround
================
*/
func (s *Store) AttachGround(source GroundSnapshotSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ground.source = source
}

// GroundSnapshotForRestore rebuilds the loaded ground state in the
// registry's snapshot shape; boot passes it to Registry.Restore (original
// DroppedAt - the TTL deadline continues across the restart).
/*
================
GroundSnapshotForRestore
================
*/
func (s *Store) GroundSnapshotForRestore() domain.GroundSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	divisions := make(map[string][]domain.GroundItemRecord, len(s.ground.loadedRecords))
	for divisionID, rows := range s.ground.loadedRecords {
		out := make([]domain.GroundItemRecord, len(rows))
		copy(out, rows)
		for i := range out {
			out[i].MagicOptions = append([]uint64(nil), out[i].MagicOptions...)
			out[i].Summon = domain.CloneCOS(out[i].Summon)
		}
		divisions[divisionID] = out
	}
	return domain.GroundSnapshot{Version: domain.GroundSnapshotVersion, GidCounter: s.ground.loadedGidCounter, Divisions: divisions}
}

// DeletedCharactersSnapshot returns byte-preserved soft-deleted records for
// offline inspection. Gameplay never decodes or mutates these archival
// records.
/*
================
DeletedCharactersSnapshot
================
*/
func (s *Store) DeletedCharactersSnapshot() map[string][]json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]json.RawMessage, len(s.deleted))
	for divisionID, records := range s.deleted {
		copied := make([]json.RawMessage, len(records))
		for index, record := range records {
			copied[index] = append(json.RawMessage(nil), record...)
		}
		out[divisionID] = copied
	}
	return out
}

// Mutate is the general commit door: fn runs every in-memory mutation
// of one operation under the store lock, then the operation commits in
// one transaction. Write failure is fail-open and loud: fn's effects
// stand, the caller acks, Health degrades, and the retained dirty flags
// make the next successful commit heal everything that lagged.
//
// This unscoped door does not know WHICH records fn touched, so it
// conservatively persists all of them. Gameplay paths use MutateCharacter
// for O(delta).
/*
================
Mutate
================
*/
func (s *Store) Mutate(label string, fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fn != nil {
		fn()
	}
	s.changes.all = true
	s.commitLocked(label)
}

// MutateCharacter is the scoped commit door: fn's mutations are declared
// to touch ONLY the given character (plus the ground registry, which the
// commit detects by revision). This is the gameplay hot path - the
// commit transaction carries one character row instead of the world.
// A nil character declares a ground-only operation (TTL sweeps).
/*
================
MutateCharacter
================
*/
func (s *Store) MutateCharacter(c *domain.Character, label string, fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fn != nil {
		fn()
	}
	if c != nil {
		if _, known := s.charDivision[c]; known {
			s.changes.characters[c] = true
		} else {
			// A record the store does not own (detached fixture or a
			// wiring bug): persisting everything is the lossless answer,
			// and the log makes the wiring bug findable.
			s.changes.all = true
			log.Warnf("store: MutateCharacter(%q) received an unknown character record %q; persisting the whole world for this commit", label, c.Name)
		}
	}
	s.commitLocked(label)
}

// UpdateCharacter is the conditional character commit door. The update runs
// against the live record while the authority write lock is held and returns
// whether it changed authoritative state. A refusal returns false and performs
// no dirty tracking and no database transaction.
//
// Gameplay handlers use this door when validation depends on mutable character
// state. Keeping the decision and the write in one critical section prevents a
// stale snapshot from overwriting a concurrent quest reward, deletion
// reservation, or movement update.
/*
================
UpdateCharacter
================
*/
func (s *Store) UpdateCharacter(c *domain.Character, label string, update func() bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c == nil || update == nil {
		return false
	}
	if _, known := s.charDivision[c]; !known {
		log.Warnf("store: UpdateCharacter(%q) refused an unknown character record %q", label, c.Name)
		return false
	}
	if !update() {
		return false
	}
	s.changes.characters[c] = true
	s.commitLocked(label)
	return true
}

// MutateCharacters is the multi-record commit door: fn's mutations are
// declared to touch ONLY the given characters (plus the ground registry,
// which the commit detects by revision), and the whole operation commits
// in ONE transaction under ONE lock hold. This is what makes a MUTUAL
// edge pair atomic - a friend add writes BOTH owners' lists, and two
// sequential MutateCharacter doors commit twice, so a process death
// between the commits leaves a one-sided edge on disk that no soft
// refusal unwinds (the CreateGuild rationale, for character records).
// The label is the operation name in the shared door vocabulary
// ("friend-add", "friend-delete", ...), exactly as the single-record
// door takes it.
//
// fn runs UNCONDITIONALLY (the door contract: refusal decisions belong
// BEFORE the door - a door that refuses fn silently drops the gameplay
// mutation itself). The character list only DECLARES the dirty scope:
// a nil entry is skipped, a repeated pointer marks once (the dirty set
// is a set), and a record the store does not own escalates to the
// whole-world commit exactly like MutateCharacter. Write failure
// follows the door's fail-open-loud rule: every dirty flag is RETAINED,
// so the next successful commit heals both records together - never
// one without the other.
/*
================
MutateCharacters
================
*/
func (s *Store) MutateCharacters(cs []*domain.Character, label string, fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fn != nil {
		fn()
	}
	for _, c := range cs {
		if c == nil {
			continue
		}
		if _, known := s.charDivision[c]; known {
			s.changes.characters[c] = true
		} else {
			// The MutateCharacter posture for a record the store does
			// not own (detached fixture or a wiring bug): persisting
			// everything is the lossless answer, and the log makes the
			// wiring bug findable.
			s.changes.all = true
			log.Warnf("store: MutateCharacters(%q) received an unknown character record %q; persisting the whole world for this commit", label, c.Name)
		}
	}
	s.commitLocked(label)
}

// UpdateCharacters is the conditional multi-record door. Every declared
// record must still belong to this store before the callback runs. A callback
// refusal returns false without dirty state or a database transaction.
/*
================
UpdateCharacters
================
*/
func (s *Store) UpdateCharacters(cs []*domain.Character, label string, update func() bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(cs) == 0 || update == nil {
		return false
	}
	for _, character := range cs {
		if character == nil {
			return false
		}
		if _, known := s.charDivision[character]; !known {
			log.Warnf("store: UpdateCharacters(%q) refused an unknown character record %q", label, character.Name)
			return false
		}
	}
	if !update() {
		return false
	}
	for _, character := range cs {
		s.changes.characters[character] = true
	}
	s.commitLocked(label)
	return true
}

// mailboxKey identifies one character's letter mailbox in the dirty set.
/*
================
mailboxKey
================
*/
type mailboxKey struct {
	division string
	charID   int64
}

// guildKey identifies one guild - the row plus its whole member set, the
// guild plane's dirty unit - in the dirty set.
/*
================
guildKey
================
*/
type guildKey struct {
	division string
	guildID  int64
}

// campKey identifies one training camp - the row plus its whole member
// set, the camp plane's dirty unit - in the dirty set (guildKey's twin).
/*
================
campKey
================
*/
type campKey struct {
	division string
	campID   int64
}

// Letters returns the letter-mailbox door (domain.LetterStore over the
// memos table). Same lifetime contract as Characters().
/*
================
Letters
================
*/
func (s *Store) Letters() domain.LetterStore {
	return storeLetterDoor{s: s}
}

/*
================
storeLetterDoor
================
*/
type storeLetterDoor struct{ s *Store }

// Mailbox returns a copy of the character's persisted mailbox in list
// order (empty when the character has no rows).
/*
================
Mailbox
================
*/
func (door storeLetterDoor) Mailbox(divisionID string, characterID int64) []domain.LetterRecord {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	live := door.s.mailboxes[divisionID][characterID]
	out := make([]domain.LetterRecord, len(live))
	copy(out, live)
	return out
}

/*
================
DeliverLetter
================
*/
func (door storeLetterDoor) DeliverLetter(
	divisionID string,
	senderID, receiverID int64,
	maxCount int,
	letter domain.LetterRecord,
) bool {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	sender := s.characterByIDLocked(divisionID, senderID)
	receiver := s.characterByIDLocked(divisionID, receiverID)
	if sender == nil || receiver == nil || sender.DeletePending || receiver.DeletePending ||
		letter.Sender != sender.Name || validateLetterRecord(letter) != nil {
		return false
	}
	live := s.mailboxes[divisionID][receiverID]
	if maxCount < 1 || maxCount > domain.LetterMailboxMaxCount ||
		len(live) >= maxCount || len(live) >= domain.LetterMailboxMaxCount {
		return false
	}
	next := make([]domain.LetterRecord, len(live), len(live)+1)
	copy(next, live)
	next = append(next, letter)
	if s.mailboxes[divisionID] == nil {
		s.mailboxes[divisionID] = map[int64][]domain.LetterRecord{}
	}
	s.mailboxes[divisionID][receiverID] = next
	s.changes.mailboxes[mailboxKey{division: divisionID, charID: receiverID}] = true
	s.commitLocked("letter-send")
	return true
}

/*
================
UpdateMailbox
================
*/
func (door storeLetterDoor) UpdateMailbox(
	divisionID string,
	characterID int64,
	label string,
	fn func(mailbox []domain.LetterRecord) ([]domain.LetterRecord, bool),
) bool {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	character := s.characterByIDLocked(divisionID, characterID)
	if character == nil || character.DeletePending || fn == nil {
		return false
	}
	live := s.mailboxes[divisionID][characterID]
	snapshot := make([]domain.LetterRecord, len(live))
	copy(snapshot, live)
	next, changed := fn(snapshot)
	if !changed {
		return false
	}
	if len(next) > domain.LetterMailboxMaxCount {
		return false
	}
	for _, letter := range next {
		if validateLetterRecord(letter) != nil {
			return false
		}
	}
	if s.mailboxes[divisionID] == nil {
		s.mailboxes[divisionID] = map[int64][]domain.LetterRecord{}
	}
	s.mailboxes[divisionID][characterID] = next
	s.changes.mailboxes[mailboxKey{division: divisionID, charID: characterID}] = true
	s.commitLocked(label)
	return true
}

// nativeCharacterNameShapePattern mirrors the native client's character-name
// allowance (the launcher reference's pattern of the same name): letters,
// digits, and underscore. The underscore is load-bearing - the native
// abusefilter.txt allows 0x5F, so a stricter [A-Za-z0-9]+ shape would refuse
// names the retail client accepts.
