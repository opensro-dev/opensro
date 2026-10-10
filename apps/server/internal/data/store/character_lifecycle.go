package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"opensro.online/server/internal/domain"
)

// Character creation and deletion-reservation policy. Persistence remains
// behind the mutation doors; callers never edit authority state directly.
var nativeCharacterNameShapePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

var (
	// ErrCharacterSlotsFull is returned before mutation when an account already
	// owns the retail maximum number of live records on the requested shard.
	ErrCharacterSlotsFull = errors.New("character roster is full")
	// ErrCharacterNameInvalid identifies only native character-name validation
	// failures. Callers must not infer this class from arbitrary error text.
	ErrCharacterNameInvalid = errors.New("invalid character name")
	// ErrCharacterNameConflict identifies a case-insensitive live-name collision.
	ErrCharacterNameConflict = errors.New("character name already exists")
)

// CharacterNameShapeValid reports whether name matches the native
// character-name allowance AND the native 2..12 length bound - the same
// verdict CreateCharacter's name validation reaches. Exported for the agent
// API's pre-checks; a pre-check answering on charset alone used to say
// "name is OK" for 1- and 13-char names the create then refused.
// CreateCharacter enforces the same rules regardless.
func CharacterNameShapeValid(name string) bool {
	if strings.TrimSpace(name) == "" || !nativeCharacterNameShapePattern.MatchString(name) {
		return false
	}
	// Same bound and idiom as CreateCharacter (native 2..12 inclusive; the
	// evidence block lives there). The charset above is ASCII-only, so rune
	// count, byte length, and UTF-16 code units all coincide.
	n := utf8.RuneCountInString(name)
	return n >= domain.CharacterNameMinBytes && n <= domain.CharacterNameMaxBytes
}

// CreateCharacter allocates the division's next id (watermark, never
// reused), installs the record and commits. The error is for
// VALIDATION only (name conflicts refuse before any mutation); write
// failures follow the door's fail-open rule like every other commit.
func (s *Store) CreateCharacter(divisionID, accountID string, c *domain.Character) error {
	if c == nil {
		return fmt.Errorf("nil character")
	}
	if divisionID == "" || strings.TrimSpace(divisionID) != divisionID {
		return fmt.Errorf("division id must be non-empty and unpadded")
	}
	if !domain.AccountIDValid(accountID) {
		return fmt.Errorf("account id is empty, over %d bytes, or contains control text", domain.AccountIDMaxBytes)
	}
	if c.AccountID != "" && c.AccountID != accountID {
		return fmt.Errorf("character owner %q does not match create account %q", c.AccountID, accountID)
	}
	c.AccountID = accountID
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("%w: empty", ErrCharacterNameInvalid)
	}
	if !nativeCharacterNameShapePattern.MatchString(c.Name) {
		return fmt.Errorf("%w: %q violates the native name shape (letters, digits, underscore)", ErrCharacterNameInvalid, c.Name)
	}
	// Native name-length bound: 2..12 inclusive. Three v1.150 client sites
	// gate it with the same unsigned idiom (len - 2 > 0xa), refusing len < 2
	// or len > 12: @0x0072bef7 / @0x0072faa7 (UIO_MSG_ERROR_CHARACTER_NAME_
	// STRING) and @0x007342f0 (UIO_MSG_ERROR_CHARACTER_NAME); the shipped
	// string tells the player "Only 12 English letters are available.[Min.,
	// Max.]". The retail SERVER enforced it too (charselect SMERR vector
	// @0x00cc9e60: UIO_SMERR_NOT_ALLOWED_CHARNAME + the NAME_STRING key).
	// The client counts an edit-control length; the
	// charset gate above has already restricted the name to ASCII
	// [A-Za-z0-9_], so byte length == rune count == UTF-16 code-unit count -
	// all three notions coincide and RuneCountInString is exact. Boundary
	// matches the client: 2 and 12 are both LEGAL (len-2 > 0xa is strict).
	// The current-schema loader re-validates the same invariant on every
	// boot, so persisted and newly created identities share one rule.
	if n := utf8.RuneCountInString(c.Name); n < domain.CharacterNameMinBytes || n > domain.CharacterNameMaxBytes {
		return fmt.Errorf("%w: %q length %d out of the native 2..12 range", ErrCharacterNameInvalid, c.Name, n)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.characters[divisionID] {
		if strings.EqualFold(existing.Name, c.Name) {
			return fmt.Errorf("%w: %q in division %s", ErrCharacterNameConflict, c.Name, divisionID)
		}
	}
	owned := 0
	for _, existing := range s.characters[divisionID] {
		if existing.AccountID == accountID {
			owned++
		}
	}
	if owned >= domain.MaxCharactersPerShardAccount {
		return fmt.Errorf(
			"%w: account %q already owns %d characters in shard %q (maximum %d)",
			ErrCharacterSlotsFull,
			accountID,
			owned,
			divisionID,
			domain.MaxCharactersPerShardAccount,
		)
	}
	// v6 records carry the racial base-attack skills (the retail
	// _AddNewChar creation seed - lane6 notes section 3, superseding
	// lane4's in-repo evidence-negative). Resolved BY CODENAME from the
	// shipped skilldata; a resolution failure REFUSES the creation
	// before any mutation (a short seed persisted into a record is
	// worse than a refusal). Runtime creation and offline database
	// migration take the same posture. The
	// seed resolves before the id allocates so a refusal mutates
	// nothing.
	if len(c.Skills) == 0 {
		if s.defaultSkills == nil {
			// A permanently unseeded current-schema record is corrupt
			// data under every posture.
			return fmt.Errorf("creating %q refused: no skilldata seeder wired (Options.DefaultSkills); current-schema records require racial base skills", c.Name)
		}
		seeded, err := s.defaultSkills(domain.ResolveCharacterRaceKey(c), nil)
		if err != nil {
			return fmt.Errorf("creation skill seed for %q: %w", c.Name, err)
		}
		c.Skills = seeded
	}
	// The racial creation quest seed (the retail _RefCharDefault_Quest
	// mechanism, internal/game/quest/seed.go). OPTIONAL wiring - nil seeds
	// nothing (see the Options.DefaultQuests doc) - but a WIRED seeder
	// that fails refuses the creation before any mutation, exactly like
	// the skill seed above.
	if c.ActiveQuests == nil && s.defaultQuests != nil {
		seeded, err := s.defaultQuests(domain.ResolveCharacterRaceKey(c))
		if err != nil {
			return fmt.Errorf("creation quest seed for %q: %w", c.Name, err)
		}
		if len(seeded) > 0 {
			c.ActiveQuests = seeded
		}
	}
	// The starter items and gold of the creation choice (retail _AddNewChar
	// inserts them with the record), so the list shows the new character
	// dressed. Unwired, the first enter-world bootstrap grants the same set.
	if c.MissionInventory == nil && s.defaultInventory != nil {
		s.defaultInventory(c)
	}
	id := s.meta.NextCharID[divisionID]
	if id < 1 {
		id = s.maxCharIDLocked(divisionID) + 1
	}
	if id < 1 || id > domain.MaxCharacterID {
		return fmt.Errorf("character id space exhausted for division %s (next %d, maximum %d)", divisionID, id, domain.MaxCharacterID)
	}
	c.ID = id
	// Current records carry explicit creation-base stats. MaxHP/MaxMP are
	// deliberately NOT persisted: the maxima are DERIVED
	// (charactervitals.DerivedMaxHP/MP - level 1 with BaseStat 20/20 yields
	// exactly the retail _AddNewChar 200/200) and an absent current
	// reads as full, so a fresh record matches retail with no vestigial
	// vitals fields that could rot against the derivation.
	if c.Strength == nil {
		base := domain.BaseStat
		c.Strength = &base
	}
	if c.Intellect == nil {
		base := domain.BaseStat
		c.Intellect = &base
	}
	// Current records carry the racial mastery set (the equip gates'
	// bit 0x100 walk needs the records to EXIST).
	if len(c.Masteries) == 0 {
		c.Masteries = domain.DefaultMasteries(domain.ResolveCharacterRaceKey(c))
	}
	s.meta.NextCharID[divisionID] = id + 1
	s.characters[divisionID] = append(s.characters[divisionID], c)
	s.rebuildCharacterLookupLocked(divisionID)
	s.charDivision[c] = divisionID
	s.changes.characters[c] = true
	s.changes.nextCharID[divisionID] = true
	s.commitLocked("create-character " + c.Name)
	return nil
}

// ValidateCharacterOwners proves that every live record belongs to an
// account accepted by the current title-login configuration. It is a boot
// gate, not a migration: unknown owners refuse and no record is rewritten.
func (s *Store) ValidateCharacterOwners(accountIDs []string) error {
	valid := make(map[string]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID = strings.TrimSpace(accountID); accountID != "" {
			valid[accountID] = struct{}{}
		}
	}
	if len(valid) == 0 {
		return fmt.Errorf("no valid account ids")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for divisionID, characters := range s.characters {
		for _, c := range characters {
			if _, ok := valid[c.AccountID]; !ok {
				return fmt.Errorf("character %s/%s belongs to unknown account %q", divisionID, c.Name, c.AccountID)
			}
		}
	}
	return nil
}

// ReserveCharacterDeletion installs the seven-day deletion reservation only
// when the character is live and belongs to no guild or training camp. An
// already-pending retry succeeds idempotently without extending its timestamp.
// The membership check and character mutation share one store lock, so a
// social-plane join cannot race the reservation.
func (s *Store) ReserveCharacterDeletion(c *domain.Character, reservedAt string) bool {
	if c == nil {
		return false
	}
	if _, err := time.Parse(time.RFC3339, reservedAt); err != nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	divisionID, known := s.charDivision[c]
	if !known || s.characterHasGroupMembershipLocked(divisionID, c.ID) {
		return false
	}
	// A retry after an uncertain HTTP response is idempotent: preserve the
	// original reservation timestamp instead of silently extending the window.
	if c.DeletePending {
		return true
	}
	c.DeletePending = true
	c.DeleteReservedAt = reservedAt
	s.changes.characters[c] = true
	s.commitLocked("delete-reserve " + c.Name)
	return true
}

func (s *Store) characterByIDLocked(divisionID string, characterID int64) *domain.Character {
	for _, c := range s.characters[divisionID] {
		if c != nil && c.ID == characterID {
			return c
		}
	}
	return nil
}

func (s *Store) characterHasGroupMembershipLocked(divisionID string, characterID int64) bool {
	for _, members := range s.guildMembers[divisionID] {
		for _, member := range members {
			if member.CharID == characterID {
				return true
			}
		}
	}
	for _, members := range s.campMembers[divisionID] {
		for _, member := range members {
			if member.CharID == characterID {
				return true
			}
		}
	}
	return false
}

// maxCharIDLocked is the safety net for an unseeded division (fresh
// environments without a migration): live records only - the migration
// tool seeds the watermark for anything with history.
func (s *Store) maxCharIDLocked(divisionID string) int64 {
	max := int64(0)
	for _, c := range s.characters[divisionID] {
		if c.ID > max {
			max = c.ID
		}
	}
	return max
}

/*
================
Health

The observable write condition as last published. It takes no store lock:
the readiness probe calls it, and a probe queued behind a waiting writer
could not answer at all (#570). Safe from anywhere, Mutate closures
included.
================
*/
func (s *Store) Health() Health {
	if view := s.healthView.Load(); view != nil {
		return *view
	}
	return Health{}
}

/*
================
publishHealthLocked

Publishes a copy of s.health for Health. Every writer of s.health holds
s.mu and calls this after its change.
================
*/
func (s *Store) publishHealthLocked() {
	view := s.health
	s.healthView.Store(&view)
}

// MetaView copies the counters for boot logs and tests. Never call from
// inside a Mutate closure.
func (s *Store) MetaView() MetaView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	next := make(map[string]int64, len(s.meta.NextCharID))
	for divisionID, id := range s.meta.NextCharID {
		next[divisionID] = id
	}
	gid := s.ground.loadedGidCounter
	if s.ground.source != nil {
		gid = s.ground.source.Snapshot().GidCounter
	}
	return MetaView{GidCounter: gid, NextCharID: next}
}

// DeleteReservationWindow is how long a deletion reservation waits
// before the reaper archives the character - the same 7 days the
// character-select screen's countdown renders
// (CHARACTER_DELETE_RESERVATION_DAYS in CPSCharacterSelect.tsx).
