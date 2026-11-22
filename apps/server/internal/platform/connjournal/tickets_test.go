/*
===========================================================================

tickets_test.go - world tickets per account per hour

Outcomes fold into one bucket per account and UTC hour; a read sees
counts the writer has not flushed yet; a flush survives a reopen and
adds to the hour instead of replacing it; the held buckets are bounded
and the overflow is counted; buckets past the retention are pruned.

===========================================================================
*/
package connjournal

import (
	"path/filepath"
	"testing"
	"time"
)

/*
================
TestTicketHoursFoldPerAccountAndHour
================
*/
func TestTicketHoursFoldPerAccountAndHour(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "connections.sqlite")
	journal := openTestJournal(t, path)
	base := time.Date(2026, 11, 22, 10, 59, 0, 0, time.UTC)
	journal.now = func() time.Time { return base }
	for range 3 {
		journal.RecordTicket("alice", TicketCounts{TransportMinted: 1})
	}
	journal.RecordTicket("alice", TicketCounts{EnterWorldPaced: 1})
	journal.RecordTicket("bob", TicketCounts{TransportMinted: 1})

	// Unflushed counts are already visible.
	hours, err := journal.TicketHours("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 1 || hours[0].TransportMinted != 3 || hours[0].EnterWorldPaced != 1 {
		t.Fatalf("before the flush %+v", hours)
	}
	if err := journal.checkpoint(); err != nil {
		t.Fatal(err)
	}
	// The next hour is its own bucket; the same hour adds, never replaces.
	journal.RecordTicket("alice", TicketCounts{TransportMinted: 1})
	journal.now = func() time.Time { return base.Add(2 * time.Minute) }
	journal.RecordTicket("alice", TicketCounts{TransportPaced: 2})
	if err := journal.checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestJournal(t, path)
	hours, err = reopened.TicketHours("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	hour := base.Truncate(time.Hour).UnixMilli()
	want := []TicketHour{
		{HourMs: hour, TicketCounts: TicketCounts{TransportMinted: 4, EnterWorldPaced: 1}},
		{HourMs: hour + ticketHourMs, TicketCounts: TicketCounts{TransportPaced: 2}},
	}
	if len(hours) != len(want) || hours[0] != want[0] || hours[1] != want[1] {
		t.Fatalf("after a reopen %+v, want %+v", hours, want)
	}
	if bob, _ := reopened.TicketHours("bob", 0); len(bob) != 1 || bob[0].TransportMinted != 1 {
		t.Fatalf("another account's bucket %+v", bob)
	}
}

/*
================
TestTicketBucketsAreBounded

Past maxTicketBuckets held between flushes, a new account-hour is
dropped and counted in Health; an account already held keeps counting.
================
*/
func TestTicketBucketsAreBounded(t *testing.T) {
	t.Parallel()

	journal := openTestJournal(t, filepath.Join(t.TempDir(), "connections.sqlite"))
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return base }
	journal.ticketMu.Lock()
	for i := 0; i < maxTicketBuckets; i++ {
		journal.tickets[ticketKey{hourMs: int64(i), account: "filler"}] = TicketCounts{TransportMinted: 1}
	}
	journal.ticketMu.Unlock()
	if journal.RecordTicket("late", TicketCounts{TransportMinted: 1}) {
		t.Fatal("a bucket past the bound was kept")
	}
	if dropped := journal.Health()["ticketsDropped"]; dropped != uint64(1) {
		t.Fatalf("ticketsDropped = %v", dropped)
	}
}

/*
================
TestTicketHoursArePrunedAtRetention
================
*/
func TestTicketHoursArePrunedAtRetention(t *testing.T) {
	t.Parallel()

	journal := openTestJournal(t, filepath.Join(t.TempDir(), "connections.sqlite"))
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return base }
	journal.RecordTicket("alice", TicketCounts{TransportMinted: 1})
	if err := journal.checkpoint(); err != nil {
		t.Fatal(err)
	}
	journal.now = func() time.Time { return base.Add((retentionDays + 1) * 24 * time.Hour) }
	if err := journal.checkpoint(); err != nil {
		t.Fatal(err)
	}
	if hours, _ := journal.TicketHours("alice", 0); len(hours) != 0 {
		t.Fatalf("a bucket past the retention was kept: %+v", hours)
	}
}

/*
================
BenchmarkRecordTicket

What one mint costs the census: a map write under the bucket lock, the
same account-hour every time (a loop's steady state).
================
*/
func BenchmarkRecordTicket(b *testing.B) {
	journal, err := Open(filepath.Join(b.TempDir(), "connections.sqlite"), "global-official", testKey)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	b.ReportAllocs()
	for b.Loop() {
		journal.RecordTicket("looper", TicketCounts{TransportMinted: 1})
	}
}
