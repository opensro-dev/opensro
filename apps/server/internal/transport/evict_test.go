package transport

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

// evictedPair builds the eviction fixture: victim and winner fighting
// over one bind key, with the VICTIM'S writes gated so its BYE can never
// flush — the eviction drain window deterministically stays open for the
// whole test instead of racing the writeLoop's drain-close. The victim's
// conn also claims datagram support so the unreliable refusal paths are
// exercised.
func evictedPair(t *testing.T) (*Hub, *Session, *fakeConn, *Session, *fakeConn) {
	t.Helper()
	hub := newHub(testCfg())
	newSess := func(gated, dgram bool) (*Session, *fakeConn) {
		s, err := hub.createSession()
		if err != nil {
			t.Fatal(err)
		}
		fc := newFakeConn(gated)
		fc.supportsDgram = dgram
		if err := s.attach(fc, false); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { hub.closeSession(s, nil) })
		return s, fc
	}
	victim, victimConn := newSess(true, true)
	winner, winnerConn := newSess(false, false)

	if _, replaced := hub.BindExclusive("d1:cg", victim); replaced {
		t.Fatal("first bind replaced something")
	}
	old, replaced := hub.BindExclusive("d1:cg", winner)
	if !replaced || old != victim {
		t.Fatalf("second bind: replaced=%v old=%v, want eviction of the victim", replaced, old)
	}
	if !victim.Evicted() {
		t.Fatal("victim not flagged evicted after replacement")
	}
	if winner.Evicted() {
		t.Fatal("winner wrongly flagged evicted")
	}
	return hub, victim, victimConn, winner, winnerConn
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	wait.Eventually(t, 3*time.Second, what, cond)
}

// TestSingleBindEvicteeInboundGameOpcodesDropped covers inbound eviction:
// during the BYE-drain window the evictee's game opcodes (a move, even a
// fresh EnterWorld) must never reach handlers, while the winner's dispatch
// is untouched and the evictee's control lane (BYE) still works.
//
// On the pre-fix code the victim's readLoop kept dispatching until final
// teardown, so the victim's 0x7738 below reached the handler and the
// no-extra-dispatch check failed.
func TestSingleBindEvicteeInboundGameOpcodesDropped(t *testing.T) {
	hub, victim, victimConn, winner, winnerConn := evictedPair(t)

	calls := make(chan uint64, 8)
	record := func(s *Session, _ uint16, _ []byte) { calls <- s.ID }
	hub.Handle(0x7738, record)
	hub.Handle(OpEnterWorld, record)

	// The evictee, mid-drain, tries to keep driving the character — and to
	// steal the bind back with a fresh EnterWorld.
	victimConn.inbound <- Frame{Opcode: 0x7738, Payload: []byte{0x01}}
	victimConn.inbound <- Frame{Opcode: OpEnterWorld, Payload: EncodeEnterWorld(EnterWorld{
		Division: "d1", CharName: "cg", AuthToken: []byte("evicted-session-token"),
	})}

	// Both frames consumed by the victim's readLoop...
	waitUntil(t, "victim readLoop to consume both frames", func() bool {
		return len(victimConn.inbound) == 0
	})
	// Give any wrongly-started dispatch time to land before checking.
	wait.Consistently(t, 50*time.Millisecond, "no handler dispatch during the drain window", func() bool {
		return len(calls) == 0
	})
	select {
	case id := <-calls:
		t.Fatalf("handler ran for session %d during the drain window", id)
	default:
	}

	// ...while the winner still dispatches normally.
	winnerConn.inbound <- Frame{Opcode: 0x7738, Payload: []byte{0x02}}
	select {
	case id := <-calls:
		if id != winner.ID {
			t.Fatalf("handler ran for session %d, want winner %d", id, winner.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("winner's 0x7738 never dispatched")
	}

	// Control frames still flow inbound: the evictee's BYE ack tears the
	// session down without waiting for the 2s drain cap.
	victimConn.inbound <- Frame{Opcode: OpBye, Payload: []byte{ByeReasonNormal}}
	waitUntil(t, "victim teardown on BYE ack", func() bool {
		_, ok := hub.Session(victim.ID)
		return !ok
	})
}

// TestSingleBindEvicteeOutboundGameSendsRefused covers outbound eviction: once
// evicted, every game-frame send to the victim is refused on every lane
// (reliable, unreliable, keyed-unreliable), so tick pushes and division
// broadcasts can no longer reach the replaced client; its drain flushes
// only session-internal control and then closes. The winner's pushes are
// unaffected.
//
// On the pre-fix code all three sends returned nil (the 0x30E3 even left as
// a datagram immediately), so the ErrSessionEvicted assertions failed.
func TestSingleBindEvicteeOutboundGameSendsRefused(t *testing.T) {
	hub, victim, victimConn, winner, winnerConn := evictedPair(t)

	if err := victim.Send(0x3126, []byte{0xAA}); !errors.Is(err, ErrSessionEvicted) {
		t.Fatalf("evictee reliable game Send err = %v, want ErrSessionEvicted", err)
	}
	if err := victim.SendUnreliable(OpObjectSourceMove, []byte{0x01}); !errors.Is(err, ErrSessionEvicted) {
		t.Fatalf("evictee SendUnreliable err = %v, want ErrSessionEvicted", err)
	}
	if err := victim.SendUnreliableKeyed(OpObjectSourceCorrection, 7, []byte{0x02}); !errors.Is(err, ErrSessionEvicted) {
		t.Fatalf("evictee SendUnreliableKeyed err = %v, want ErrSessionEvicted", err)
	}
	if got := victimConn.datagrams(); len(got) != 0 {
		t.Fatalf("%d datagrams reached the evictee after eviction", len(got))
	}
	// Session-internal control still queues: the drain needs PONG/BYE.
	if err := victim.Send(OpPong, []byte{0x77}); err != nil {
		t.Fatalf("evictee control Send err = %v, want nil", err)
	}

	// The winner's push lane is untouched.
	if err := winner.Send(0x3126, []byte{0xBB}); err != nil {
		t.Fatalf("winner Send err = %v", err)
	}
	frames := waitWritten(t, winnerConn, 2) // WELCOME + push
	if last := frames[len(frames)-1]; last.Opcode != 0x3126 {
		t.Fatalf("winner's last frame = 0x%04X, want 0x3126", last.Opcode)
	}

	// Open the victim's gate: the drain must flush ONLY control frames
	// (WELCOME, BYE(Replaced), PONG) and then close the session.
	victimConn.openGate()
	waitUntil(t, "victim drain close", func() bool {
		_, ok := hub.Session(victim.ID)
		return !ok
	})
	var sawReplacedBye bool
	for _, f := range victimConn.written() {
		if f.Opcode > maxControlOpcode {
			t.Fatalf("game frame 0x%04X leaked to the evictee during drain", f.Opcode)
		}
		if f.Opcode == OpBye && len(f.Payload) == 1 && f.Payload[0] == ByeReasonReplaced {
			sawReplacedBye = true
		}
	}
	if !sawReplacedBye {
		t.Fatal("drain flushed without the BYE(Replaced)")
	}
}

// TestSingleBindEvictedSessionCannotRebind closes the steal-back hole: an
// EnterWorld handler of the LOSER that was already in flight when the
// eviction landed must not re-claim the key (or any key) via BindExclusive.
//
// On the pre-fix code the loser's rebind evicted the winner right back —
// the first assertion failed with replaced=true.
func TestSingleBindEvictedSessionCannotRebind(t *testing.T) {
	hub, victim, _, winner, _ := evictedPair(t)

	if old, replaced := hub.BindExclusive("d1:cg", victim); replaced {
		t.Fatalf("evicted session stole the key back, evicting %d", old.ID)
	}
	if bound, ok := hub.BoundSession("d1:cg"); !ok || bound != winner {
		t.Fatalf("bound session = %v (ok=%v), want the winner %d", bound, ok, winner.ID)
	}
	if winner.Evicted() {
		t.Fatal("winner got lame-ducked by the loser's rebind attempt")
	}
	if _, replaced := hub.BindExclusive("d1:fresh", victim); replaced {
		t.Fatal("evicted session evicted someone on a fresh key")
	}
	if _, ok := hub.BoundSession("d1:fresh"); ok {
		t.Fatal("evicted session claimed a fresh key during its drain")
	}
}

func TestBindingControlLeaseEvictsOldBindingAndBlocksNewBind(t *testing.T) {
	hub := newHub(testCfg())
	oldSession, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	oldConnection := newFakeConn(false)
	if err := oldSession.attach(oldConnection, false); err != nil {
		t.Fatal(err)
	}
	if _, replaced := hub.BindExclusive("d1:cg", oldSession); replaced {
		t.Fatal("initial binding replaced another session")
	}

	if lease, acquired := hub.AcquireBindingControl("d1:cg"); acquired || lease != nil {
		t.Fatal("control lease acquired before the old binding finished eviction")
	}
	waitUntil(t, "old binding teardown", func() bool {
		_, bound := hub.BoundSession("d1:cg")
		return !bound
	})
	lease, acquired := hub.AcquireBindingControl("d1:cg")
	if !acquired || lease == nil {
		t.Fatal("control lease unavailable after old binding teardown")
	}

	contender, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	if _, replaced := hub.BindExclusive("d1:cg", contender); replaced {
		t.Fatal("control lease reported a gameplay replacement")
	}
	if !contender.Evicted() {
		t.Fatal("world bind was not refused while control lease was held")
	}
	if _, bound := hub.BoundSession("d1:cg"); bound {
		t.Fatal("world bind became visible during control transaction")
	}
	lease.Release()
}

func TestBindingControlLeaseReleaseIsGenerationChecked(t *testing.T) {
	hub := newHub(testCfg())
	first, acquired := hub.AcquireBindingControl("d1:cg")
	if !acquired {
		t.Fatal("first control lease was refused")
	}
	stale := &BindingControlLease{
		hub:        hub,
		key:        first.key,
		generation: first.generation,
	}
	first.Release()
	second, acquired := hub.AcquireBindingControl("d1:cg")
	if !acquired {
		t.Fatal("second control lease was refused")
	}
	stale.Release()
	if third, acquired := hub.AcquireBindingControl("d1:cg"); acquired || third != nil {
		t.Fatal("stale release deleted the newer control lease")
	}
	second.Release()
}

// TestSingleBindEvictionStressRace is the -race exerciser:
// many goroutines bind the same key while sessions keep sending and
// receiving game frames. Semantics are pinned by the deterministic tests
// above; this one hammers the evicted latch, dispatch, and Send paths
// concurrently and asserts the end state: exactly one live key holder,
// never flagged, and every loser latched and refusing game sends.
func TestSingleBindEvictionStressRace(t *testing.T) {
	hub := newHub(testCfg())
	var dispatched atomic.Uint64
	hub.Handle(0x7738, func(*Session, uint16, []byte) { dispatched.Add(1) })

	const goroutines, perG = 8, 20
	var mu sync.Mutex
	all := make([]*Session, 0, goroutines*perG)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				s, err := hub.createSession()
				if err != nil {
					t.Error(err)
					return
				}
				fc := newFakeConn(false)
				if err := s.attach(fc, false); err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				all = append(all, s)
				mu.Unlock()
				hub.BindExclusive("d1:cg", s)
				s.Send(0x3126, []byte{0x01})
				fc.inbound <- Frame{Opcode: 0x7738, Payload: []byte{0x02}}
			}
		}()
	}
	wg.Wait()
	defer func() {
		for _, s := range all {
			hub.closeSession(s, nil)
		}
		// A drained session's own writeLoop may win the close and still be
		// logging "session closed" when this returns; that line then landed
		// in a later test's captured log (a -race report). Each close's hook
		// completion follows its log line, so wait for all of them.
		hub.closeHooks.Wait()
	}()

	bound, ok := hub.BoundSession("d1:cg")
	if !ok {
		t.Fatal("no session holds the key after the storm")
	}
	if bound.Evicted() {
		t.Fatal("final key holder is flagged evicted")
	}
	if err := bound.Send(0x3126, []byte{0x03}); err != nil {
		t.Fatalf("winner Send err = %v", err)
	}
	for _, s := range all {
		if s == bound {
			continue
		}
		if !s.Evicted() {
			t.Fatalf("replaced session %d never got the evicted latch", s.ID)
		}
		if err := s.Send(0x3126, []byte{0x04}); err == nil {
			t.Fatalf("replaced session %d still accepts game sends", s.ID)
		}
	}
}
