/*
===========================================================================

sookarena_account_test.go - the survival arena's daily entries per account

The three entries a UTC day count per account, not per character
(#3322): every alt of an account enters against the one record, and
the record is spent inside the entry's relocation door.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/sookarena"
)

/*
================
sookAccountFixture

Characters of the given accounts, standing at So-ok, on a clock one
minute before a UTC midnight so a test can cross the day boundary.
================
*/
func sookAccountFixture(t *testing.T, accounts ...string) (*Runtime, *fakeClock, []*enterworld.Character) {
	t.Helper()
	characters := make([]*enterworld.Character, len(accounts))
	for i, account := range accounts {
		characters[i] = instanceWorldTestCharacter(int64(11+i), "Survivor"+string(rune('A'+i)))
		characters[i].AccountID = account
	}
	rt, clock := sookFixture(t, characters...)
	clock.now = time.Date(2026, 11, 12, 23, 59, 0, 0, time.UTC)
	return rt, clock, characters
}

/*
================
sookEnterAndLeave

One full entry: select So-ok, enter, and end the run at once. Reports
whether the entry was admitted and the frames it answered.
================
*/
func sookEnterAndLeave(rt *Runtime, c *enterworld.Character) (bool, []wire.Frame) {
	rt.Selected.Set(testDivision, c.Name, 333)
	frames := rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	if run == nil {
		return false, frames
	}
	rt.endSookArena(run, c, false)
	return true, frames
}

/*
================
refusedAsSpent
================
*/
func refusedAsSpent(frames []wire.Frame) bool {
	return len(frames) > 0 && bytes.Equal(frames[0].Payload, sookBanner(sookEntriesSpentText).Payload)
}

/*
================
TestSookAltIsRefusedOnceTheAccountIsSpent

A second character of an account that has used its three entries
today is refused at So-ok with the account wording, while a character
of another account still enters.
================
*/
func TestSookAltIsRefusedOnceTheAccountIsSpent(t *testing.T) {
	rt, _, characters := sookAccountFixture(t, "account-a", "account-a", "account-b")
	main, alt, other := characters[0], characters[1], characters[2]
	for i := range domain.SookArenaDailyEntries {
		if admitted, _ := sookEnterAndLeave(rt, main); !admitted {
			t.Fatalf("entry %d of the account refused", i+1)
		}
	}
	admitted, frames := sookEnterAndLeave(rt, alt)
	if admitted || !refusedAsSpent(frames) {
		t.Fatalf("alt of a spent account: admitted %v, frames %v", admitted, frames)
	}
	if alt.SookArenaActive || domain.CharacterWorldInstance(alt) != domain.DefaultWorldInstance || sookEntries(rt, alt) != domain.SookArenaDailyEntries {
		t.Fatal("the refused alt changed state")
	}
	if admitted, _ := sookEnterAndLeave(rt, other); !admitted || sookEntries(rt, other) != 1 {
		t.Fatal("another account was charged for account-a's entries")
	}
}

/*
================
TestSookAltsShareTheRemainingEntries

Characters of one account entering in the same tick take at most the
entries left: with one already spent, two of three alts get in and
the third is refused.
================
*/
func TestSookAltsShareTheRemainingEntries(t *testing.T) {
	rt, clock, characters := sookAccountFixture(t, "account-a", "account-a", "account-a")
	account := rt.deps.SookArenaAccount("account-a")
	account.Day, account.Entries = sookarena.Day(clock.Now()), 1
	admitted := 0
	for _, c := range characters {
		rt.Selected.Set(testDivision, c.Name, 333)
		rt.enterSookArena(testDivision, c)
		if rt.sookRun(testDivision, c) != nil {
			admitted++
		}
	}
	if admitted != 2 || account.Entries != domain.SookArenaDailyEntries {
		t.Fatalf("admitted %d alts with entries %d, want 2 and %d", admitted, account.Entries, domain.SookArenaDailyEntries)
	}
}

/*
================
sookRacingEntry

Spends the account's last entry right after the given character's
early check passed, standing in for another character of the account
whose entry commits in between. The entry's own door must then refuse.
================
*/
type sookRacingEntry struct {
	Dependencies
	racer *enterworld.Character
}

func (d *sookRacingEntry) Update(c *enterworld.Character, label string, update func() bool) bool {
	changed := d.Dependencies.Update(c, label, update)
	if label == "sook-entry-check" && c == d.racer {
		account := d.SookArenaAccount(c.AccountID)
		account.Entries = domain.SookArenaDailyEntries
	}
	return changed
}

/*
================
TestSookEntryDoorRechecksTheAccount

The early check only saves opening a world: the entry is spent inside
the relocation door, so an entry that loses the last slot to a
concurrent alt is refused there, spends nothing and leaves no world.
================
*/
func TestSookEntryDoorRechecksTheAccount(t *testing.T) {
	rt, clock, characters := sookAccountFixture(t, "account-a")
	c := characters[0]
	account := rt.deps.SookArenaAccount("account-a")
	account.Day, account.Entries = sookarena.Day(clock.Now()), domain.SookArenaDailyEntries-1
	rt.deps = &sookRacingEntry{Dependencies: rt.deps, racer: c}
	frames := rt.enterSookArena(testDivision, c)
	if rt.sookRun(testDivision, c) != nil || !refusedAsSpent(frames) {
		t.Fatalf("the losing entry was admitted or misreported: %v", frames)
	}
	if account.Entries != domain.SookArenaDailyEntries || c.SookArenaActive || domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance {
		t.Fatalf("the losing entry changed state: entries %d active %v", account.Entries, c.SookArenaActive)
	}
}

/*
================
sookFailedReentry

Fails the relocation after its door committed, so the arena's own
rollback has to give the spent entry back.
================
*/
type sookFailedReentry struct{ Dependencies }

func (sookFailedReentry) ReentryPackets(string, string) ([]enterworld.Packet, bool) {
	return nil, false
}

/*
================
TestSookFailedRelocationRefundsTheAccount

An entry whose relocation fails after the admission spent the entry
refunds exactly that entry: the account keeps the count it had, and
the character is not left marked inside an arena.
================
*/
func TestSookFailedRelocationRefundsTheAccount(t *testing.T) {
	rt, clock, characters := sookAccountFixture(t, "account-a")
	c := characters[0]
	account := rt.deps.SookArenaAccount("account-a")
	account.Day, account.Entries = sookarena.Day(clock.Now()), 1
	rt.deps = sookFailedReentry{rt.deps}
	rt.enterSookArena(testDivision, c)
	if rt.sookRun(testDivision, c) != nil || account.Entries != 1 || c.SookArenaActive {
		t.Fatalf("failed relocation: entries %d active %v, want 1 and false", account.Entries, c.SookArenaActive)
	}
}

/*
================
TestSookAccountResetsAtUTCMidnight

The UTC day rollover resets the account: an alt refused at 23:59
enters at 00:00 and spends the new day's first entry.
================
*/
func TestSookAccountResetsAtUTCMidnight(t *testing.T) {
	rt, clock, characters := sookAccountFixture(t, "account-a", "account-a")
	main, alt := characters[0], characters[1]
	for range domain.SookArenaDailyEntries {
		sookEnterAndLeave(rt, main)
	}
	if admitted, _ := sookEnterAndLeave(rt, alt); admitted {
		t.Fatal("alt entered a spent account before midnight")
	}
	clock.now = clock.now.Add(time.Minute)
	if admitted, _ := sookEnterAndLeave(rt, alt); !admitted {
		t.Fatal("alt refused after the UTC rollover")
	}
	account := rt.deps.SookArenaAccount("account-a")
	if account.Day != sookarena.Day(clock.Now()) || account.Entries != 1 {
		t.Fatalf("account after rollover = %+v, want today's first entry", *account)
	}
}
