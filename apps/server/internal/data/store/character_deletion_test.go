package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

// Mature-deletion and read-boundary regression tests.
func TestReapMaturedDeletions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	reservationStamp := func(age time.Duration) string {
		return clock.Now().Add(-age).UTC().Format("2006-01-02T15:04:05.000Z")
	}
	reserve := func(authority *Store, c *enterworld.Character, age time.Duration) {
		authority.MutateCharacter(c, "delete-reserve "+c.Name, func() {
			c.DeletePending = true
			c.DeleteReservedAt = reservationStamp(age)
		})
	}

	doomed := &enterworld.Character{Name: "doomed"}
	fresh := &enterworld.Character{Name: "freshreserve"}
	garbage := &enterworld.Character{Name: "garbagestamp"}
	for _, c := range []*enterworld.Character{doomed, fresh, garbage} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}
	reserve(s, doomed, 8*24*time.Hour) // past the window
	reserve(s, fresh, 24*time.Hour)    // 6 days remain
	s.MutateCharacter(garbage, "delete-reserve garbagestamp", func() {
		garbage.DeletePending = true
		garbage.DeleteReservedAt = "not a timestamp"
	})

	names := s.ReapMaturedDeletions()
	if len(names) != 1 || names[0] != testDivision+"/doomed" {
		t.Fatalf("reaped %v, want exactly the matured doomed", names)
	}
	live := s.Characters().CharactersForDivision(testDivision)
	if len(live) != 2 {
		t.Fatalf("%d live characters after reap, want 2 (fresh + garbage kept)", len(live))
	}
	ghosts := s.deleted[testDivision]
	if len(ghosts) != 1 || !strings.Contains(string(ghosts[0]), `"doomed"`) {
		t.Fatalf("archive after reap = %v, want doomed's final bytes", ghosts)
	}
	s.Close()

	// The archive and the survivors persist across a restart, and the id
	// watermark never hands out doomed's id again.
	s2 := openTest(t, dir, clock)
	if len(s2.Characters().CharactersForDivision(testDivision)) != 2 {
		t.Fatal("reap did not persist across restart")
	}
	if ghosts := s2.deleted[testDivision]; len(ghosts) != 1 || !strings.Contains(string(ghosts[0]), `"doomed"`) {
		t.Fatalf("archived record lost across restart: %v", ghosts)
	}
	next := &enterworld.Character{Name: "successor"}
	if err := s2.CreateCharacter(testDivision, "test-account", next); err != nil {
		t.Fatal(err)
	}
	if next.ID != 4 {
		t.Fatalf("post-reap allocation = %d, want 4 (ids of archived rows never reuse)", next.ID)
	}

	// Fail-closed: an outage leaves a matured reservation live; healing
	// retries it.
	reserve(s2, next, 8*24*time.Hour)
	s2.commitFail = errors.New("disk on fire")
	if reaped := s2.ReapMaturedDeletions(); len(reaped) != 0 {
		t.Fatalf("outage reap archived %v, want fail-closed nothing", reaped)
	}
	if len(s2.Characters().CharactersForDivision(testDivision)) != 3 {
		t.Fatal("fail-closed reap must leave the character live")
	}
	s2.commitFail = nil
	if reaped := s2.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("healed reap = %v, want the retried successor", reaped)
	}
}

func TestReapCascadesFriendEdgesAtomically(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	characterNamed := func(authority *Store, name string) *enterworld.Character {
		for _, character := range authority.Characters().CharactersForDivision(testDivision) {
			if character.Name == name {
				return character
			}
		}
		return nil
	}
	survivor := &enterworld.Character{Name: "friendstay"}
	doomed := &enterworld.Character{Name: "friendgone"}
	for _, character := range []*enterworld.Character{survivor, doomed} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}

	// A current-schema load must refuse a one-sided graph.
	s.MutateCharacter(survivor, "inject-one-sided-friend", func() {
		enterworld.SwapFriends(survivor, []enterworld.FriendRecord{{ID: doomed.ID, Name: doomed.Name}})
	})
	if _, err := loadDB(s.syncDBLocked(), CurrentVersion, CurrentLayoutVersion); err == nil ||
		!strings.Contains(err.Error(), "no reciprocal edge") {
		t.Fatalf("one-sided friend load error = %v", err)
	}

	// Install the reciprocal half, then mature the target's reservation.
	s.MutateCharacter(doomed, "heal-mutual-friend", func() {
		enterworld.SwapFriends(doomed, []enterworld.FriendRecord{{ID: survivor.ID, Name: survivor.Name}})
	})
	stamp := clock.Now().Add(-DeleteReservationWindow - time.Hour).UTC().Format(time.RFC3339)
	if !s.ReserveCharacterDeletion(doomed, stamp) {
		t.Fatal("friend target deletion reservation refused")
	}

	// The archive and inbound-edge removal are one fail-closed transaction.
	s.FailCommits(errors.New("friend cascade outage"))
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 0 {
		t.Fatalf("failed transaction reaped %v", reaped)
	}
	if len(enterworld.FriendsView(survivor)) != 1 {
		t.Fatal("failed archive removed the inbound friend edge in memory")
	}
	if characterNamed(s, doomed.Name) == nil {
		t.Fatal("failed archive removed the target")
	}

	s.FailCommits(nil)
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("healed transaction reaped %v, want one target", reaped)
	}
	if len(enterworld.FriendsView(survivor)) != 0 {
		t.Fatal("successful archive left the inbound friend edge")
	}
	s.Close()

	reopened := openTest(t, dir, clock)
	if characterNamed(reopened, doomed.Name) != nil {
		t.Fatal("archived friend target returned after restart")
	}
	reloadedSurvivor := characterNamed(reopened, survivor.Name)
	if reloadedSurvivor == nil || len(enterworld.FriendsView(reloadedSurvivor)) != 0 {
		t.Fatalf("reopened survivor friend list = %+v", enterworld.FriendsView(reloadedSurvivor))
	}
}

// TestReapSurvivesArchiveSeqGap: the archive seq for a new victim comes
// from MAX(seq)+1 inside the transaction, never from the in-memory
// archive count. A hand-edited deleted_characters table (rows removed
// below a surviving higher seq) makes the two disagree; the count answer
// would collide with the survivor on the (division, seq) primary key
// and fail-close every reap until an operator intervenes.
func TestReapSurvivesArchiveSeqGap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	doomed := &enterworld.Character{Name: "doomed"}
	if err := s.CreateCharacter(testDivision, "test-account", doomed); err != nil {
		t.Fatal(err)
	}
	s.MutateCharacter(doomed, "delete-reserve doomed", func() {
		doomed.DeletePending = true
		doomed.DeleteReservedAt = clock.Now().Add(-8 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	})

	// Hand-edit the archive: one surviving row at seq 1 with nothing
	// below it (as if an operator deleted the seq-0 row). After a reload
	// the in-memory archive count (1) equals the survivor's seq - the
	// exact collision shape a len()-derived next seq walks into.
	if _, err := s.syncDBLocked().Exec("INSERT INTO deleted_characters (division, seq, record) VALUES (?, 1, ?)", testDivision, `{"id":99,"name":"handedit","deleted":true}`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2 := openTest(t, dir, clock)
	if got := len(s2.deleted[testDivision]); got != 1 {
		t.Fatalf("loaded %d archived record(s), want the 1 hand-edited survivor", got)
	}
	names := s2.ReapMaturedDeletions()
	if len(names) != 1 || names[0] != testDivision+"/doomed" {
		t.Fatalf("reaped %v, want exactly doomed (a seq collision fail-closes the reap)", names)
	}
	if health := s2.Health(); health.FailedWrites != 0 {
		t.Fatalf("reap must not degrade health on a gapped archive: %+v", health)
	}

	// Both records survive a restart, ordered by seq: the hand-edited
	// survivor first, doomed appended after it.
	s2.Close()
	s3 := openTest(t, dir, clock)
	ghosts := s3.deleted[testDivision]
	if len(ghosts) != 2 {
		t.Fatalf("archive after restart = %d record(s), want 2 (survivor + doomed)", len(ghosts))
	}
	if !strings.Contains(string(ghosts[0]), `"handedit"`) || !strings.Contains(string(ghosts[1]), `"doomed"`) {
		t.Fatalf("archive order/content wrong after gapped-seq reap: %s | %s", ghosts[0], ghosts[1])
	}
}

// TestReadCharactersHandsOutACopy: the READ door's slice must not alias
// the store's live backing array - the reaper compacts that array IN
// PLACE (records[:0]) and CreateCharacter appends to it, so a retained
// live header would shrink or reorder under its holder with no
// synchronization. The *Character POINTERS themselves stay identical
// (never deep-copied).
func TestReadCharactersHandsOutACopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	doomed := &enterworld.Character{Name: "aliasdoomed"}
	survivor := &enterworld.Character{Name: "aliaskeeper"}
	for _, c := range []*enterworld.Character{doomed, survivor} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}

	var captured []*enterworld.Character
	s.ReadCharacters(testDivision, func(characters []*enterworld.Character) {
		captured = characters
	})
	if len(captured) != 2 || captured[0] != doomed || captured[1] != survivor {
		t.Fatalf("captured = %v, want the two live records by pointer identity", captured)
	}
	// Direct aliasing witness: the callback slice's backing array must
	// not be the store's live one.
	if live := s.characters[testDivision]; &live[0] == &captured[0] {
		t.Fatal("ReadCharacters handed out the live backing array (a retained slice would race the reaper's in-place compaction)")
	}

	// Behavioral witness: the reaper compacts the LIVE array in place;
	// an aliased retained slice would see doomed overwritten by the
	// survivor, a copy keeps its (stale but stable) view.
	s.MutateCharacter(doomed, "delete-reserve", func() {
		doomed.DeletePending = true
		doomed.DeleteReservedAt = clock.Now().UTC().Format(time.RFC3339)
	})
	clock.Advance(DeleteReservationWindow + time.Hour)
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v, want exactly the doomed record", reaped)
	}
	if captured[0] != doomed || captured[1] != survivor {
		t.Fatalf("the retained slice was rewritten by the reap's compaction: %v", captured)
	}
}
