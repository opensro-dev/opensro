package match

// Registry mutation tests: register -> appears in the listing, modify ->
// updates in place (same entry id), delete -> disappears, paging
// boundaries, the own-row-first policy the client's row-0 split depends
// on, and division isolation.

import (
	"fmt"
	"testing"
)

const boardDivision = "global-official"

func ownerOf(name string) string {
	return ownerKey(boardDivision, name)
}

func TestPartyRegisterAppearsInListingWithOwnRowFirst(t *testing.T) {
	board := NewBoard()

	hero, ok := board.RegisterParty(boardDivision, ownerOf("Hero"), PartyEntry{MasterName: "Hero", Title: "own"})
	if !ok || hero.EntryID == 0 {
		t.Fatalf("register = (%+v, %v), want an assigned id", hero, ok)
	}
	alice, ok := board.RegisterParty(boardDivision, ownerOf("Alice"), PartyEntry{MasterName: "Alice", Title: "foreign"})
	if !ok {
		t.Fatal("second owner refused")
	}
	if alice.EntryID == hero.EntryID {
		t.Fatalf("entry ids collide at %d", alice.EntryID)
	}

	// Hero's view: own row first, then Alice.
	curPage, pageCount, rows := board.PartyPage(boardDivision, ownerOf("Hero"), 1)
	if curPage != 1 || pageCount != 1 || len(rows) != 2 {
		t.Fatalf("page = (%d/%d, %d rows), want (1/1, 2 rows)", curPage, pageCount, len(rows))
	}
	if rows[0].MasterName != "Hero" || rows[1].MasterName != "Alice" {
		t.Fatalf("row order = [%s, %s], want the own row first", rows[0].MasterName, rows[1].MasterName)
	}

	// Alice's view: her own row first, then Hero.
	_, _, rows = board.PartyPage(boardDivision, ownerOf("Alice"), 1)
	if rows[0].MasterName != "Alice" || rows[1].MasterName != "Hero" {
		t.Fatalf("row order = [%s, %s], want Alice first", rows[0].MasterName, rows[1].MasterName)
	}

	// A third character without a registration sees registration order.
	_, _, rows = board.PartyPage(boardDivision, ownerOf("Bob"), 1)
	if rows[0].MasterName != "Hero" || rows[1].MasterName != "Alice" {
		t.Fatalf("row order = [%s, %s], want registration order", rows[0].MasterName, rows[1].MasterName)
	}
}

func TestHasRegistrationSpansBothBoards(t *testing.T) {
	board := NewBoard()
	if board.HasRegistration(ownerOf("Hero")) {
		t.Fatal("empty board reports a registration")
	}
	if _, ok := board.RegisterMentor(boardDivision, ownerOf("Hero"), MentorEntry{Requester: "Hero"}); !ok {
		t.Fatal("mentor register refused")
	}
	if !board.HasRegistration(ownerOf("Hero")) {
		t.Fatal("a mentor registration does not read as a standing")
	}
	if board.HasRegistration(ownerOf("Alice")) {
		t.Fatal("another owner reads as registered")
	}
	if !board.DeleteMentor(ownerOf("Hero"), 1) {
		t.Fatal("mentor delete refused")
	}
	if board.HasRegistration(ownerOf("Hero")) {
		t.Fatal("a deleted registration still reads as a standing")
	}
}

func TestPartyRegisterRefusesADuplicateOwner(t *testing.T) {
	board := NewBoard()
	if _, ok := board.RegisterParty(boardDivision, ownerOf("Hero"), PartyEntry{MasterName: "Hero"}); !ok {
		t.Fatal("first register refused")
	}
	if _, ok := board.RegisterParty(boardDivision, ownerOf("Hero"), PartyEntry{MasterName: "Hero"}); ok {
		t.Fatal("duplicate register accepted")
	}
	if _, _, rows := board.PartyPage(boardDivision, ownerOf("Hero"), 1); len(rows) != 1 {
		t.Fatalf("board holds %d rows after the refused duplicate, want 1", len(rows))
	}
}

func TestPartyModifyUpdatesInPlaceKeepingTheEntryID(t *testing.T) {
	board := NewBoard()
	registered, _ := board.RegisterParty(boardDivision, ownerOf("Hero"), PartyEntry{MasterName: "Hero", Title: "before", MinLevel: 1})

	modified, ok := board.ModifyParty(ownerOf("Hero"), PartyEntry{MasterName: "Hero", Title: "after", MinLevel: 40})
	if !ok {
		t.Fatal("modify refused")
	}
	if modified.EntryID != registered.EntryID {
		t.Fatalf("modify reassigned the id: %d -> %d", registered.EntryID, modified.EntryID)
	}
	_, _, rows := board.PartyPage(boardDivision, ownerOf("Hero"), 1)
	if len(rows) != 1 || rows[0].Title != "after" || rows[0].MinLevel != 40 {
		t.Fatalf("listing after modify = %+v, want the updated row", rows)
	}

	if _, ok := board.ModifyParty(ownerOf("Nobody"), PartyEntry{}); ok {
		t.Fatal("modify without a registration accepted")
	}
}

func TestPartyDeleteRemovesOnlyTheOwnersEntry(t *testing.T) {
	board := NewBoard()
	hero, _ := board.RegisterParty(boardDivision, ownerOf("Hero"), PartyEntry{MasterName: "Hero"})
	alice, _ := board.RegisterParty(boardDivision, ownerOf("Alice"), PartyEntry{MasterName: "Alice"})

	// A foreign id refuses: Hero cannot delete Alice's row.
	if board.DeleteParty(ownerOf("Hero"), alice.EntryID) {
		t.Fatal("foreign delete accepted")
	}
	// A wrong id refuses even for the owner.
	if board.DeleteParty(ownerOf("Hero"), hero.EntryID+100) {
		t.Fatal("unknown id accepted")
	}
	if !board.DeleteParty(ownerOf("Hero"), hero.EntryID) {
		t.Fatal("own delete refused")
	}
	_, _, rows := board.PartyPage(boardDivision, ownerOf("Hero"), 1)
	if len(rows) != 1 || rows[0].MasterName != "Alice" {
		t.Fatalf("listing after delete = %+v, want only Alice", rows)
	}
	// Re-register after delete works and hands out a fresh id.
	again, ok := board.RegisterParty(boardDivision, ownerOf("Hero"), PartyEntry{MasterName: "Hero"})
	if !ok || again.EntryID == hero.EntryID {
		t.Fatalf("re-register = (%+v, %v), want a fresh id", again, ok)
	}
}

func TestPartyPagingBoundaries(t *testing.T) {
	board := NewBoard()
	// 25 foreign rows -> 3 pages (12 + 12 + 1) for a viewer without an
	// own registration.
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("Char%02d", i)
		if _, ok := board.RegisterParty(boardDivision, ownerOf(name), PartyEntry{MasterName: name}); !ok {
			t.Fatalf("register %s refused", name)
		}
	}

	curPage, pageCount, rows := board.PartyPage(boardDivision, ownerOf("Viewer"), 1)
	if curPage != 1 || pageCount != 3 || len(rows) != PageRows {
		t.Fatalf("page 1 = (%d/%d, %d rows), want (1/3, %d)", curPage, pageCount, len(rows), PageRows)
	}
	if rows[0].MasterName != "Char00" {
		t.Fatalf("page 1 row 0 = %s, want Char00", rows[0].MasterName)
	}

	curPage, pageCount, rows = board.PartyPage(boardDivision, ownerOf("Viewer"), 3)
	if curPage != 3 || pageCount != 3 || len(rows) != 1 {
		t.Fatalf("page 3 = (%d/%d, %d rows), want (3/3, 1)", curPage, pageCount, len(rows))
	}
	if rows[0].MasterName != "Char24" {
		t.Fatalf("page 3 row 0 = %s, want Char24", rows[0].MasterName)
	}

	// Page 0 clamps to 1; an overshoot clamps to the last page.
	if curPage, _, _ = board.PartyPage(boardDivision, ownerOf("Viewer"), 0); curPage != 1 {
		t.Fatalf("page 0 clamped to %d, want 1", curPage)
	}
	if curPage, _, rows = board.PartyPage(boardDivision, ownerOf("Viewer"), 9); curPage != 3 || len(rows) != 1 {
		t.Fatalf("page 9 clamped to (%d, %d rows), want (3, 1)", curPage, len(rows))
	}

	// A registered viewer's own row rides page 1 row 0 and shifts the
	// split: 26 total rows still fit 3 pages (12+12+2).
	if _, ok := board.RegisterParty(boardDivision, ownerOf("Char10"), PartyEntry{MasterName: "dup"}); ok {
		t.Fatal("duplicate Char10 accepted")
	}
	_, _, rows = board.PartyPage(boardDivision, ownerOf("Char10"), 1)
	if rows[0].MasterName != "Char10" {
		t.Fatalf("registered viewer's page 1 row 0 = %s, want the own row", rows[0].MasterName)
	}
}

func TestPartyPageEmptyBoardAnswersOnePageOfNothing(t *testing.T) {
	board := NewBoard()
	curPage, pageCount, rows := board.PartyPage(boardDivision, ownerOf("Hero"), 1)
	if curPage != 1 || pageCount != 1 || len(rows) != 0 {
		t.Fatalf("empty board page = (%d/%d, %d rows), want (1/1, 0)", curPage, pageCount, len(rows))
	}
}

func TestPartyListingIsDivisionScoped(t *testing.T) {
	board := NewBoard()
	board.RegisterParty("division-a", "division-a:hero", PartyEntry{MasterName: "Hero"})
	board.RegisterParty("division-b", "division-b:alice", PartyEntry{MasterName: "Alice"})

	_, _, rows := board.PartyPage("division-a", "division-a:hero", 1)
	if len(rows) != 1 || rows[0].MasterName != "Hero" {
		t.Fatalf("division-a listing = %+v, want only Hero", rows)
	}
	_, _, rows = board.PartyPage("division-b", "division-b:viewer", 1)
	if len(rows) != 1 || rows[0].MasterName != "Alice" {
		t.Fatalf("division-b listing = %+v, want only Alice", rows)
	}
}

func TestMentorBoardMirrorsThePartyLifecycle(t *testing.T) {
	board := NewBoard()

	hero, ok := board.RegisterMentor(boardDivision, ownerOf("Hero"), MentorEntry{Requester: "Hero", Detail: "my camp", Kind: 1})
	if !ok || hero.EntryID == 0 {
		t.Fatalf("mentor register = (%+v, %v), want an assigned id", hero, ok)
	}
	if _, ok := board.RegisterMentor(boardDivision, ownerOf("Hero"), MentorEntry{Requester: "Hero"}); ok {
		t.Fatal("duplicate mentor register accepted")
	}
	alice, _ := board.RegisterMentor(boardDivision, ownerOf("Alice"), MentorEntry{Requester: "Alice", Detail: "join us", Kind: 2})

	// Party and mentor ids share one counter - never colliding.
	party, _ := board.RegisterParty(boardDivision, ownerOf("Hero"), PartyEntry{MasterName: "Hero"})
	if party.EntryID == hero.EntryID || party.EntryID == alice.EntryID {
		t.Fatalf("party id %d collides with a mentor id", party.EntryID)
	}

	modified, ok := board.ModifyMentor(ownerOf("Hero"), MentorEntry{Requester: "Hero", Detail: "edited", Kind: 2})
	if !ok || modified.EntryID != hero.EntryID {
		t.Fatalf("mentor modify = (%+v, %v), want the id kept", modified, ok)
	}
	_, _, rows := board.MentorPage(boardDivision, ownerOf("Hero"), 1)
	if len(rows) != 2 || rows[0].Detail != "edited" || rows[1].Requester != "Alice" {
		t.Fatalf("mentor listing = %+v, want the edited own row first", rows)
	}

	if board.DeleteMentor(ownerOf("Hero"), alice.EntryID) {
		t.Fatal("foreign mentor delete accepted")
	}
	if !board.DeleteMentor(ownerOf("Hero"), hero.EntryID) {
		t.Fatal("own mentor delete refused")
	}
	_, _, rows = board.MentorPage(boardDivision, ownerOf("Hero"), 1)
	if len(rows) != 1 || rows[0].Requester != "Alice" {
		t.Fatalf("mentor listing after delete = %+v, want only Alice", rows)
	}
	// The party row survived the mentor delete - separate registries.
	if _, _, partyRows := board.PartyPage(boardDivision, ownerOf("Hero"), 1); len(partyRows) != 1 {
		t.Fatalf("party listing = %d rows after mentor delete, want 1", len(partyRows))
	}
}
