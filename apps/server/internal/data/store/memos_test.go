package store

// Letter-mailbox persistence tests: the LetterStore door round-trips through
// commits and reopens, and the deletion reaper drops the victim's rows in the
// same transaction that archives the character.

import (
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

func memoTestCharacter(name string) *enterworld.Character {
	return &enterworld.Character{
		Name:          name,
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     int64Ptr(enterworld.RaceChina),
		Gender:        int64Ptr(enterworld.GenderMale),
	}
}

func TestLetterMailboxPersistsAcrossReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	if err := s.CreateCharacter(testDivision, "test-account", memoTestCharacter("postbox")); err != nil {
		t.Fatal(err)
	}
	var charID int64
	s.ReadCharacters(testDivision, func(characters []*enterworld.Character) {
		charID = characters[0].ID
	})

	letters := s.Letters()
	first := enterworld.LetterRecord{Sender: "Ash", SenderModelRefID: 1907, PackedReceiveTime: 0x00c8_739a, ReadFlag: 0, Body: "first body"}
	second := enterworld.LetterRecord{Sender: "Cale", SenderModelRefID: 14875, PackedReceiveTime: 0x00c8_739b, ReadFlag: 0, Body: "second body"}
	letters.UpdateMailbox(testDivision, charID, "letter-send", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
		return append(mailbox, first), true
	})
	letters.UpdateMailbox(testDivision, charID, "letter-send", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
		return append(mailbox, second), true
	})

	// The Mailbox read is a copy: mutating it must not leak into the
	// live plane.
	leak := letters.Mailbox(testDivision, charID)
	leak[0].Body = "tampered"
	if got := letters.Mailbox(testDivision, charID); got[0].Body != "first body" {
		t.Fatal("Mailbox returned the live backing array")
	}

	s.Close()
	s2 := openTest(t, dir, clock)
	restored := s2.Letters().Mailbox(testDivision, charID)
	if len(restored) != 2 || restored[0] != first || restored[1] != second {
		t.Fatalf("restored mailbox = %+v, want [%+v %+v]", restored, first, second)
	}

	// Flip the read flag and drop the first letter; the next reopen must
	// carry exactly the compacted, flipped state.
	s2.Letters().UpdateMailbox(testDivision, charID, "letter-read", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
		mailbox[1].ReadFlag = 1
		return mailbox, true
	})
	s2.Letters().UpdateMailbox(testDivision, charID, "letter-delete", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
		return mailbox[1:], true
	})
	s2.Close()

	s3 := openTest(t, dir, clock)
	final := s3.Letters().Mailbox(testDivision, charID)
	if len(final) != 1 || final[0].Sender != "Cale" || final[0].ReadFlag != 1 || final[0].Body != "second body" {
		t.Fatalf("final mailbox = %+v, want the flipped Cale letter only", final)
	}
}

func TestLetterDoorRefusesRecordsOutsidePersistedWireBounds(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()
	sender := memoTestCharacter("Sender")
	receiver := memoTestCharacter("Receiver")
	for _, character := range []*enterworld.Character{sender, receiver} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}

	oversized := enterworld.LetterRecord{
		Sender: sender.Name,
		Body:   strings.Repeat("x", domain.LetterBodyMaxBytes+1),
	}
	if got := s.Letters().DeliverLetter(testDivision, sender.ID, receiver.ID, domain.LetterMailboxMaxCount, oversized); got != domain.LetterDeliveryRefused {
		t.Fatalf("oversized letter body verdict = %v, want refused", got)
	}

	valid := enterworld.LetterRecord{Sender: sender.Name, Body: "valid"}
	if got := s.Letters().DeliverLetter(testDivision, sender.ID, receiver.ID, domain.LetterMailboxMaxCount, valid); got != domain.LetterDeliveryDone {
		t.Fatalf("valid letter verdict = %v, want delivered", got)
	}
}

func TestLetterDoorReportsMailboxFull(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()
	sender := memoTestCharacter("Sender")
	receiver := memoTestCharacter("Receiver")
	for _, character := range []*enterworld.Character{sender, receiver} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}

	letter := enterworld.LetterRecord{Sender: sender.Name, Body: "valid"}
	for i := 0; i < domain.LetterMailboxMaxCount; i++ {
		if got := s.Letters().DeliverLetter(testDivision, sender.ID, receiver.ID, domain.LetterMailboxMaxCount, letter); got != domain.LetterDeliveryDone {
			t.Fatalf("delivery %d verdict = %v, want delivered", i, got)
		}
	}
	if got := s.Letters().DeliverLetter(testDivision, sender.ID, receiver.ID, domain.LetterMailboxMaxCount, letter); got != domain.LetterDeliveryMailboxFull {
		t.Fatalf("over-cap delivery verdict = %v, want mailbox full", got)
	}
	if s.Letters().UpdateMailbox(testDivision, receiver.ID, "inject-invalid-letter", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
		mailbox[0].ReadFlag = 2
		return mailbox, true
	}) {
		t.Fatal("invalid letter update crossed the store boundary")
	}
}

// TestReapDropsMailbox proves a reaped character's memos rows die with
// the archive commit - on the live plane immediately and on disk across
// the reopen (no orphan rows under a recycled-id successor... ids are
// never reused, but orphan bytes still must not accrete).
func TestReapDropsMailbox(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	if err := s.CreateCharacter(testDivision, "test-account", memoTestCharacter("doomedbox")); err != nil {
		t.Fatal(err)
	}
	var doomed *enterworld.Character
	s.ReadCharacters(testDivision, func(characters []*enterworld.Character) {
		doomed = characters[0]
	})
	s.Letters().UpdateMailbox(testDivision, doomed.ID, "letter-send", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
		return append(mailbox, enterworld.LetterRecord{Sender: "Ash", Body: "unclaimed"}), true
	})
	s.MutateCharacter(doomed, "delete-reserve", func() {
		doomed.DeletePending = true
		doomed.DeleteReservedAt = clock.Now().UTC().Format(time.RFC3339)
	})

	clock.Advance(DeleteReservationWindow + time.Hour)
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v, want the doomed character", reaped)
	}
	if got := s.Letters().Mailbox(testDivision, doomed.ID); len(got) != 0 {
		t.Fatalf("live mailbox after reap = %d letter(s), want none", len(got))
	}
	s.Close()

	s2 := openTest(t, dir, clock)
	if got := s2.Letters().Mailbox(testDivision, doomed.ID); len(got) != 0 {
		t.Fatalf("reopened mailbox after reap = %d letter(s), want none", len(got))
	}
}
