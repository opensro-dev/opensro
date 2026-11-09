/*
===========================================================================

commit_writer_test.go - the asynchronous writer keeps the disk off the lock

The flag-on twin of lock_timing_test.go and the writer's own contract:
a slow SQLite commit no longer holds the store lock, a door answers
before its batch is written, a direct read drains the queue first, Close
writes every queued batch, and a full queue makes the next commit wait.

===========================================================================
*/

package store

import (
	"strings"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/wait"
)

const (
	// asyncWriterParked bounds the waits for the writer to reach the
	// parked commit; nothing here sleeps on a fixed delay except the
	// queue-cap check's negative window.
	asyncWriterParked = 5 * time.Second
	asyncQueueBlocked = 50 * time.Millisecond
)

/*
================
parkedCommit

A beforeSQLiteCommit seam that parks the writer inside the first commit
it runs until release; later commits pass straight through.
================
*/
type parkedCommit struct {
	inCommit chan struct{}
	release  chan struct{}
	once     sync.Once
	freed    sync.Once
}

func newParkedCommit(t *testing.T) *parkedCommit {
	p := &parkedCommit{inCommit: make(chan struct{}), release: make(chan struct{})}
	// A failed assertion must not leave the writer parked: Close's drain
	// would wait on it forever.
	t.Cleanup(p.free)
	return p
}

func (p *parkedCommit) seam() {
	first := false
	p.once.Do(func() { first = true })
	if first {
		close(p.inCommit)
		<-p.release
	}
}

func (p *parkedCommit) free() { p.freed.Do(func() { close(p.release) }) }

/*
================
asyncStoreWithCharacter
================
*/
func asyncStoreWithCharacter(t *testing.T, dir string, name string) (*Store, *enterworld.Character) {
	t.Helper()
	s := openTestCommit(t, dir, newTestClock(), true)
	c := &enterworld.Character{Name: name, Gold: int64Ptr(1)}
	if err := s.CreateCharacter(testDivision, "async-account", c); err != nil {
		t.Fatal(err)
	}
	return s, c
}

/*
================
TestAsyncCommitKeepsASlowCommitOutOfTheLock

The writer is parked inside a commit's SQLite phase. With the flag off the
door and every reader wait for it (TestSlowCommitShowsInAConcurrentReadersWait);
with it on the door has already answered and a reader enters at once. The
write still lands once the disk is free.
================
*/
func TestAsyncCommitKeepsASlowCommitOutOfTheLock(t *testing.T) {
	t.Parallel()
	s, c := asyncStoreWithCharacter(t, t.TempDir(), "asyncslow")
	park := newParkedCommit(t)
	s.mu.Lock()
	s.beforeSQLiteCommit = park.seam
	s.mu.Unlock()

	s.MutateCharacter(c, "async-slow", func() { c.Gold = int64Ptr(500) })
	<-park.inCommit
	// The door returned with its batch parked on the disk; the lock is free.
	read := make(chan struct{})
	go func() {
		defer close(read)
		s.ReadState(func() {})
	}()
	select {
	case <-read:
	case <-time.After(asyncWriterParked):
		t.Fatal("a reader waited on the parked commit: the SQLite phase still holds the store lock")
	}
	park.free()
	if raw := readCharacterRecordRaw(t, s, testDivision, "asyncslow"); !strings.Contains(raw, "500") {
		t.Fatalf("the parked commit's write is not on disk after the drain: %s", raw)
	}
}

/*
================
TestAsyncCommitCloseWritesEveryQueuedBatch

Commits queued behind a parked writer are all on disk after Close, in
order: the reopened store holds the last value.
================
*/
func TestAsyncCommitCloseWritesEveryQueuedBatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, c := asyncStoreWithCharacter(t, dir, "asyncclose")
	park := newParkedCommit(t)
	s.mu.Lock()
	s.beforeSQLiteCommit = park.seam
	s.mu.Unlock()

	const commits = 10
	for i := 1; i <= commits; i++ {
		gold := int64(i * 100)
		s.MutateCharacter(c, "async-queued", func() { c.Gold = &gold })
	}
	<-park.inCommit
	park.free()
	s.Close()

	reopened := openTestSync(t, dir, newTestClock())
	if raw := readCharacterRecordRaw(t, reopened, testDivision, "asyncclose"); !strings.Contains(raw, `"gold":1000`) {
		t.Fatalf("the reopened store does not hold the last queued commit: %s", raw)
	}
}

/*
================
TestAsyncCommitDirectReadSeesQueuedWrites

A statement outside the batch (syncDBLocked) waits for the queue: the
read after a door sees the door's write even while the disk was busy.
================
*/
func TestAsyncCommitDirectReadSeesQueuedWrites(t *testing.T) {
	t.Parallel()
	s, c := asyncStoreWithCharacter(t, t.TempDir(), "asyncread")
	park := newParkedCommit(t)
	s.mu.Lock()
	s.beforeSQLiteCommit = park.seam
	s.mu.Unlock()

	s.MutateCharacter(c, "async-first", func() { c.Gold = int64Ptr(7) })
	s.MutateCharacter(c, "async-second", func() { c.Gold = int64Ptr(8) })
	<-park.inCommit
	got := make(chan string)
	go func() { got <- readCharacterRecordRaw(t, s, testDivision, "asyncread") }()
	select {
	case raw := <-got:
		t.Fatalf("a direct read ran past a parked queue: %s", raw)
	case <-time.After(asyncQueueBlocked):
	}
	park.free()
	if raw := <-got; !strings.Contains(raw, `"gold":8`) {
		t.Fatalf("the drained read does not see the second door's write: %s", raw)
	}
}

/*
================
TestCommitWriterFullQueueWaits

At maxQueuedCommits the next enqueue waits for a slot: a stalled disk
turns the store back into the synchronous door instead of growing the
queue. The parked batch counts against the cap until it commits.
================
*/
func TestCommitWriterFullQueueWaits(t *testing.T) {
	t.Parallel()
	s := openTestSync(t, t.TempDir(), newTestClock())
	w := startCommitWriter(s.db, time.Now)
	park := newParkedCommit(t)
	t.Cleanup(w.stop)

	w.enqueue(&commitBatch{}, park.seam)
	<-park.inCommit
	for i := 1; i < maxQueuedCommits; i++ {
		w.enqueue(&commitBatch{}, nil)
	}
	queued := make(chan uint64)
	go func() { queued <- w.enqueue(&commitBatch{}, nil) }()
	select {
	case seq := <-queued:
		t.Fatalf("enqueue %d returned past a full queue", seq)
	case <-time.After(asyncQueueBlocked):
	}
	park.free()
	seq := <-queued
	if seq != maxQueuedCommits+1 {
		t.Fatalf("the waiting batch got sequence %d, want %d", seq, maxQueuedCommits+1)
	}
	w.waitApplied(seq)
	wait.Eventually(t, asyncWriterParked, "the queue empties", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return len(w.queue) == 0 && w.applied == seq
	})
}

/*
================
TestAsyncCreateCharacterAnswersOnlyOnceItsBatchIsOnDisk

A durable door (durableTicket): with the writer parked inside the
creation's commit, CreateCharacter has not answered, yet the store lock
is already free for readers. Once the disk is free the door answers and
the new row is on disk.
================
*/
func TestAsyncCreateCharacterAnswersOnlyOnceItsBatchIsOnDisk(t *testing.T) {
	t.Parallel()
	s, _ := asyncStoreWithCharacter(t, t.TempDir(), "asyncfirst")
	park := newParkedCommit(t)
	s.mu.Lock()
	s.beforeSQLiteCommit = park.seam
	s.mu.Unlock()

	created := make(chan error, 1)
	go func() {
		created <- s.CreateCharacter(testDivision, "async-account", &enterworld.Character{Name: "asyncdurable", Gold: int64Ptr(7)})
	}()
	<-park.inCommit
	select {
	case err := <-created:
		t.Fatalf("CreateCharacter answered (%v) while its batch was parked before the disk", err)
	case <-time.After(asyncQueueBlocked):
	}
	read := make(chan struct{})
	go func() {
		defer close(read)
		s.ReadState(func() {})
	}()
	select {
	case <-read:
	case <-time.After(asyncWriterParked):
		t.Fatal("a reader waited on the durable door: its wait still holds the store lock")
	}
	park.free()
	select {
	case err := <-created:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(asyncWriterParked):
		t.Fatal("CreateCharacter never answered after the disk was free")
	}
	if raw := readCharacterRecordRaw(t, s, testDivision, "asyncdurable"); raw == "" {
		t.Fatal("the created character is not on disk when the door answered")
	}
}

/*
================
TestDurableTicketIsEmptyWithTheFlagOff

Off, the commit applied under the lock, so the ticket holds nothing and
wait returns at once.
================
*/
func TestDurableTicketIsEmptyWithTheFlagOff(t *testing.T) {
	t.Parallel()
	s := openTestCommit(t, t.TempDir(), newTestClock(), false)
	var ticket durableTicket
	s.mu.Lock()
	ticket.captureLocked(s)
	s.mu.Unlock()
	if ticket.writer != nil || ticket.seq != 0 {
		t.Fatalf("flag off, the ticket holds writer=%v seq=%d", ticket.writer != nil, ticket.seq)
	}
	ticket.wait()
}
