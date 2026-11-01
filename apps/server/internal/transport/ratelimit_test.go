package transport

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rlCfg is testCfg with an explicit rate-limit budget (the default 25/75
// spelled out so the assertions below stay pinned to the numbers the
// budget rationale in ratelimit.go derives).
func rlCfg(perSec, burst int) Config {
	cfg := testCfg()
	cfg.RateLimitPerSec = perSec
	cfg.RateLimitBurst = burst
	cfg.applyDefaults()
	return cfg
}

// frozenClock pins a session's limiter to a manual clock so token refill is
// deterministic. Safe here because the tests drive dispatch sequentially
// from one goroutine before/while reading it.
type frozenClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFrozenClock() *frozenClock {
	return &frozenClock{t: time.Unix(1_000_000, 0)}
}

func (c *frozenClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *frozenClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// limitedSession builds a hub with the given budget and one bare session
// whose limiter runs on a frozen clock.
func limitedSession(t *testing.T, cfg Config) (*Hub, *Session, *frozenClock) {
	t.Helper()
	hub := newHub(cfg)
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(s, nil) })
	clk := newFrozenClock()
	if s.limiter != nil {
		s.limiter.now = clk.now
	}
	return hub, s, clk
}

// TestRateLimitFloodClamped is the lane's core guarantee: a hostile client
// flooding an empty-payload progression opcode (0x727A "+1 STR") is shed at
// the dispatch boundary. With the clock frozen the arithmetic is exact:
// precisely the burst allowance reaches the handler, everything after is
// dropped and counted in the hub metrics, and the session stays alive
// (drop-not-kill).
//
// WITNESSED RED (recorded in the lane READY): with the admitFrame gate
// removed from Hub.dispatch, all 1000 frames reach the handler and the
// handled != burst assertion below fails.
func TestRateLimitFloodClamped(t *testing.T) {
	hub, s, _ := limitedSession(t, rlCfg(25, 75))

	var handled atomic.Uint64
	hub.Handle(0x727A, func(*Session, uint16, []byte) { handled.Add(1) })

	const flood = 1000
	for i := 0; i < flood; i++ {
		hub.dispatch(s, Frame{Opcode: 0x727A})
	}

	if got := handled.Load(); got != 75 {
		t.Fatalf("flood: %d frames reached the handler, want exactly the burst allowance 75", got)
	}
	if got := hub.Metrics().RateLimitedFrames; got != flood-75 {
		t.Fatalf("metrics rate_limited_frames = %d, want %d", got, flood-75)
	}
	if _, ok := hub.Session(s.ID); !ok {
		t.Fatal("clamped session was closed; the limiter must drop frames, never the session")
	}
}

func TestRateLimitChargesRequestBytes(t *testing.T) {
	hub, s, _ := limitedSession(t, rlCfg(25, 75))

	var handled atomic.Uint64
	hub.Handle(0x727A, func(*Session, uint16, []byte) { handled.Add(1) })

	// 32 KiB plus the opcode costs 33 tokens. A burst-75 bucket therefore
	// admits two, not an arbitrary number of allocation-heavy requests.
	frame := Frame{Opcode: 0x727A, Payload: make([]byte, 32<<10)}
	for i := 0; i < 3; i++ {
		hub.dispatch(s, frame)
	}
	if got := handled.Load(); got != 2 {
		t.Fatalf("large requests handled = %d, want 2 under byte-weighted budget", got)
	}
	if got := hub.Metrics().RateLimitedFrames; got != 1 {
		t.Fatalf("large-request drops = %d, want 1", got)
	}
}

// TestRateLimitNormalPlayNeverClamped replays the WORST plausible legit
// minute against the default 25/s+75 budget, built from the traffic
// inventory in ratelimit.go: 10 ground clicks/s of 0x7738 (faster than any
// human sustains), the worst-case 2s 0x72CD manual-movement cancel/recovery
// frame (CGInterface_CancelSkillForKeyboardMove, 0x693190), 2 progression
// clicks/s (0x727A), an item move each second
// (0x706D) and a chat line every 2s (0x7025, unhandled here — unknown
// opcodes still cost a token by design). ~13.5 frames/s sustained for 60s:
// not one frame may be shed.
func TestRateLimitNormalPlayNeverClamped(t *testing.T) {
	hub, s, clk := limitedSession(t, rlCfg(25, 75))

	var handled atomic.Uint64
	count := func(*Session, uint16, []byte) { handled.Add(1) }
	for _, op := range []uint16{0x7738, 0x72CD, 0x727A, 0x706D} {
		hub.Handle(op, count)
	}

	var sent uint64
	send := func(op uint16) {
		hub.dispatch(s, Frame{Opcode: op})
		sent++
	}
	const tick = 100 * time.Millisecond // 10 slices per simulated second
	for sec := 0; sec < 60; sec++ {
		for slice := 0; slice < 10; slice++ {
			send(0x7738) // click-spam movement: every slice
			switch slice {
			case 0, 5:
				send(0x727A) // stat-dump clicking: 2/s
			case 3:
				send(0x706D) // item shuffling: 1/s
			}
			if slice == 7 && sec%2 == 0 {
				send(0x72CD) // movement cancel/recovery: at most 1 per 2s
				send(0x7025) // chat line: 1 per 2s, unregistered opcode
			}
			clk.advance(tick)
		}
	}

	dropped := hub.Metrics().RateLimitedFrames
	if dropped != 0 {
		t.Fatalf("normal play shed %d of %d frames; the budget must never clamp legit traffic", dropped, sent)
	}
	// The 0x7025 chat lines have no handler (falls to nil default) but must
	// still have been admitted; everything registered reached its handler.
	wantHandled := sent - 30 // 60s / 2s = 30 chat frames without a handler
	if got := handled.Load(); got != wantHandled {
		t.Fatalf("handled %d frames, want %d (sent %d incl. 30 unregistered chat)", got, wantHandled, sent)
	}
}

// TestRateLimitPerSessionIsolation: one abuser must not consume another
// session's budget. The flooder is clamped at its own burst; the neighbor's
// normally-paced frames all pass, interleaved WHILE the flood runs.
func TestRateLimitPerSessionIsolation(t *testing.T) {
	hub := newHub(rlCfg(25, 75))
	abuser, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	victim, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(abuser, nil); hub.closeSession(victim, nil) })
	clk := newFrozenClock()
	abuser.limiter.now = clk.now
	victim.limiter.now = clk.now

	handledBy := make(map[uint64]uint64)
	var mu sync.Mutex
	hub.Handle(0x727A, func(s *Session, _ uint16, _ []byte) {
		mu.Lock()
		handledBy[s.ID]++
		mu.Unlock()
	})

	// 500 flood frames with one victim frame woven in after every tenth,
	// all inside one frozen instant so the abuser gets zero refill.
	for i := 0; i < 500; i++ {
		hub.dispatch(abuser, Frame{Opcode: 0x727A})
		if i%10 == 9 {
			hub.dispatch(victim, Frame{Opcode: 0x727A})
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if got := handledBy[abuser.ID]; got != 75 {
		t.Fatalf("abuser got %d frames through, want its burst 75", got)
	}
	if got := handledBy[victim.ID]; got != 50 {
		t.Fatalf("victim got %d of its 50 frames through; a flooding neighbor must not eat its budget", got)
	}
}

// TestRateLimitClampWarnThrottled pins the log-flood defense on the
// frameLimiter itself: during a continuous clamp the warn signal fires on
// the FIRST refusal and then at most once per clampLogInterval, each time
// carrying the count of frames silently dropped since the previous warn.
func TestRateLimitClampWarnThrottled(t *testing.T) {
	l := newFrameLimiter(1, 1)
	clk := newFrozenClock()
	l.now = clk.now

	if ok, _, _ := l.admit(); !ok {
		t.Fatal("first frame must pass on a full bucket")
	}
	ok, dropped, warn := l.admit()
	if ok || !warn || dropped != 1 {
		t.Fatalf("first refusal: ok=%v warn=%v dropped=%d, want refusal warning of 1", ok, warn, dropped)
	}
	// Five more refusals inside the same throttle window: silent.
	for i := 0; i < 5; i++ {
		if ok, _, warn := l.admit(); ok || warn {
			t.Fatalf("refusal %d inside the throttle window: ok=%v warn=%v, want silent drop", i+2, ok, warn)
		}
	}
	// One full interval later the bucket has refilled exactly one token
	// (perSec=1, capped at burst=1): one frame passes, the next refusal
	// warns again and reports everything dropped meanwhile.
	clk.advance(clampLogInterval)
	if ok, _, _ := l.admit(); !ok {
		t.Fatal("refilled token not granted after the interval")
	}
	ok, dropped, warn = l.admit()
	if ok || !warn || dropped != 6 {
		t.Fatalf("post-interval refusal: ok=%v warn=%v dropped=%d, want warning of the 5 silent + this one", ok, warn, dropped)
	}
}

// TestRateLimitKeepaliveUnaffected proves that clamping PING/PONG does not
// create a false idle timeout: readLoop refreshes liveness before admission,
// while refusing to provide an unlimited echo path.
func TestRateLimitKeepaliveUnaffected(t *testing.T) {
	hub := newHub(rlCfg(25, 75))
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeConn(false)
	if err := s.attach(fc, false, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(s, nil) })
	clk := newFrozenClock()
	s.limiter.now = clk.now

	// Exhaust the budget completely at one frozen instant.
	for i := 0; i < 200; i++ {
		hub.dispatch(s, Frame{Opcode: 0x727A})
	}
	if hub.Metrics().RateLimitedFrames == 0 {
		t.Fatal("budget not exhausted; test setup broken")
	}

	s.mu.Lock()
	before := time.Now().Add(-time.Minute)
	s.lastRecv = before
	s.mu.Unlock()

	fc.inbound <- Frame{Opcode: OpPong}
	waitUntil(t, "liveness refresh despite exhausted budget", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.lastRecv.After(before)
	})
	if _, ok := hub.Session(s.ID); !ok {
		t.Fatal("rate-limited keepalive closed the session")
	}
}

// TestRateLimitStateDiesWithSession is the churn-leak guard: limiter state
// is a Session field, so once the hub drops a closed session no per-session
// limiter residue can survive anywhere. Churn 200 clamped sessions and
// assert every hub-side registry is empty again.
func TestRateLimitStateDiesWithSession(t *testing.T) {
	hub := newHub(rlCfg(25, 75))
	hub.Handle(0x727A, func(*Session, uint16, []byte) {})
	for i := 0; i < 200; i++ {
		s, err := hub.createSession()
		if err != nil {
			t.Fatal(err)
		}
		s.limiter.now = newFrozenClock().now
		for j := 0; j < 100; j++ { // past the burst: this session got clamped
			hub.dispatch(s, Frame{Opcode: 0x727A})
		}
		hub.closeSession(s, nil)
	}

	if got := hub.Metrics().LiveSessions; got != 0 {
		t.Fatalf("%d sessions survive the churn", got)
	}
	hub.mu.RLock()
	sessions, tokens, keys, binds := len(hub.sessions), len(hub.byToken), len(hub.bindingKeys), len(hub.bindings)
	hub.mu.RUnlock()
	if sessions+tokens+keys+binds != 0 {
		t.Fatalf("hub retains state after churn: sessions=%d tokens=%d bindingKeys=%d bindings=%d",
			sessions, tokens, keys, binds)
	}
}

// TestRateLimitConcurrentFloodRace is the -race exerciser: many goroutines
// hammer two sessions' limiters through dispatch simultaneously (including
// the zombie-replacement window where two readLoops of one session can
// briefly overlap — the reason the limiter carries its own mutex). The
// invariant is exact bookkeeping: every frame either reached a handler or
// was counted dropped.
func TestRateLimitConcurrentFloodRace(t *testing.T) {
	hub := newHub(rlCfg(25, 75))
	s1, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(s1, nil); hub.closeSession(s2, nil) })

	var handled atomic.Uint64
	hub.Handle(0x727A, func(*Session, uint16, []byte) { handled.Add(1) })

	const goroutines, perG = 8, 500
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		target := s1
		if g%2 == 1 {
			target = s2
		}
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				hub.dispatch(s, Frame{Opcode: 0x727A})
			}
		}(target)
	}
	wg.Wait()

	total := uint64(goroutines * perG)
	if got := handled.Load() + hub.Metrics().RateLimitedFrames; got != total {
		t.Fatalf("bookkeeping leak: handled+dropped = %d, want %d", got, total)
	}
	if handled.Load() == total {
		t.Fatal("nothing was clamped; flood assertion has no teeth")
	}
}
