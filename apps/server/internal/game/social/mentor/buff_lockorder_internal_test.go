/*
===========================================================================

buff_lockorder_internal_test.go - the buff flush never waits on the store
holding the ledger lock

CampKillGrant takes the ledger lock while the kill path already holds
the character door, which is the store lock. A flush that held the
ledger lock while it waited for the same store lock would close an
AB-BA cycle and stop every store caller. The fake authority below shares
one mutex between the "character door" and WriteMemberBuffs, which is
how the store wires them.

===========================================================================
*/

package mentor

import (
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
lockOrderStore

A camp door whose WriteMemberBuffs waits on the shared store lock, as
the real store's door does. Only the method the flush calls is
implemented; the embedded interface stays nil.
================
*/
type lockOrderStore struct {
	domain.TrainingCampStore
	storeMu *sync.Mutex
	entered chan struct{}
}

func (s *lockOrderStore) WriteMemberBuffs(divisionID string, campID int64, states map[int64]domain.TrainingCampMemberBuffState) bool {
	close(s.entered)
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	return true
}

/*
================
lockOrderDeps
================
*/
type lockOrderDeps struct {
	Dependencies
	camp *lockOrderStore
}

func (d lockOrderDeps) TrainingCampAuthority() domain.TrainingCampStore { return d.camp }

/*
================
TestBuffFlushNeverHoldsTheLedgerAcrossTheStore

A kill holds the store lock (the character door) and calls
CampKillGrant while a flush is already waiting for that lock inside
WriteMemberBuffs. The kill must finish; then the flush lands.
================
*/
func TestBuffFlushNeverHoldsTheLedgerAcrossTheStore(t *testing.T) {
	const t0 = int64(1_762_934_400_000)
	var storeMu sync.Mutex
	camp := &lockOrderStore{storeMu: &storeMu, entered: make(chan struct{})}
	rt := &InviteRuntime{deps: lockOrderDeps{camp: camp}}
	rt.hydrateBuffCamp(buffTestDivision, 7, []domain.TrainingCampMemberRecord{
		{CharID: 10, Kind: MemberKindMaster},
		{CharID: 12, Kind: MemberKindStudent},
	})
	student := buffTestCharacter(12, 20)
	rt.CampKillGrant(buffTestDivision, student, 1000, 500, 1000, t0)

	// The kill path's character door.
	storeMu.Lock()
	flushed := make(chan struct{})
	go func() {
		rt.flushBuffCamp(buffTestDivision, 7)
		close(flushed)
	}()
	<-camp.entered

	killed := make(chan struct{})
	go func() {
		rt.CampKillGrant(buffTestDivision, student, 1000, 500, 1000, t0)
		close(killed)
	}()
	select {
	case <-killed:
	case <-time.After(5 * time.Second):
		t.Fatal("CampKillGrant blocked behind a flush waiting on the store lock: ledger-then-store and store-then-ledger deadlock")
	}
	storeMu.Unlock()
	select {
	case <-flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("flush never landed after the store lock was released")
	}

	// The kill that landed mid-flush stays dirty for the next flush.
	rt.buffMu.Lock()
	entry := rt.buffLedger[buffLedgerKey{divisionID: buffTestDivision, campID: 7, charID: 12}]
	dirty, value := entry.dirty, entry.state.Value
	rt.buffMu.Unlock()
	if !dirty || value != 20 {
		t.Fatalf("student entry after the mid-flush kill = dirty %v value %d, want dirty with 20 points", dirty, value)
	}
}
