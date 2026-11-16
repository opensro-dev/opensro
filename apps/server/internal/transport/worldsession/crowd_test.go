/*
===========================================================================

crowd_test.go - a plaza crowd over the production tick

The crowd fixture drives the production tick (NewTicker: the real
Ticker, Bridge, Hub, session queues, stream ring and writers) over N
in-world sessions of one division, all in one interest neighbourhood, W
of them walking. Each session's provider mirrors the movement lane's
(movement/sessionworld.go WorldSnapshotInto: a real WorldStore and the
same MovementCurrent capture); walkers commit real ApplyMove moves and
get their own 0xB738 through a handler bridge, as the movement lane
does. Sessions attach over in-memory connections that count what the
writer hands them (the server's own cost, no sockets), or over loopback
WebSockets read by prompt Go readers that ack the stream once a second
like the browser (network.ts). Not modelled: NPCs, monsters, chat,
combat, the ground-walk admission, the Agent.

TestCrowdTickWritesOncePerSession pins the tick's write window on it;
BenchmarkCrowd (crowd_bench_test.go) measures it.

===========================================================================
*/
package worldsession

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

const (
	// crowdDivision is the one division the crowd stands in.
	crowdDivision = "DIV_A"
	// crowdRadius is the plaza's radius: everyone stays inside one
	// 3x3 block neighbourhood of crowdCenter, so everyone sees everyone.
	crowdRadius = 120.0
	// crowdBotStride and the click pause bound the live bot crowd's walk
	// (client-next tools/perf/core/crowd-activity.mjs): a click within
	// 25 units of home every 1.5-6 s.
	crowdBotStride     = 25.0
	crowdClickPauseMs  = 1500
	crowdClickSpreadMs = 4500
	// crowdAckInterval is the browser's stream-ack cadence (network.ts).
	crowdAckInterval = time.Second
)

// crowdCenter is the plaza's centre, inside one 320-unit block.
var crowdCenter = simulation.Spawn{RegionID: 0x6B4F, X: 1120, Y: 80, Z: 480}

// The live crowd's plaza (the scratch stack's held bots): Hotan, region
// 23687, centre (1040, 690). It straddles the block corner at (960, 640),
// so walkers near a border keep crossing it, while every member stays in
// every other member's 3x3. 312 spots fill the inner disc and the rest a
// ring out to 190, as the hold's spot file does.
var hotanCenter = simulation.Spawn{RegionID: 23687, X: 1040, Y: 243.9, Z: 690}

const (
	hotanInnerSpots  = 312
	hotanInnerRadius = 100.0
	hotanOuterRadius = 190.0
)

//============================================================================
// The crowd's world provider
//============================================================================

/*
================
crowdActor

One crowd member's world provider, the movement lane's in miniature.
================
*/
type crowdActor struct {
	worlds     *simulation.WorldStore
	key        string
	char       int64
	sceneID    string
	appearance *simulation.PeerAppearance
	start      simulation.Spawn
	walker     bool
	nextMs     int64
	// npcs enables the NPC roster for this member (NpcsEnabled).
	npcs bool
}

/*
================
crowdActor.seed
================
*/
func (a *crowdActor) seed() simulation.WorldState { return simulation.DefaultWorldState(a.start) }

/*
================
crowdActor.WorldSnapshot
================
*/
func (a *crowdActor) WorldSnapshot() simulation.SessionSnapshot {
	var snap simulation.SessionSnapshot
	a.WorldSnapshotInto(&snap)
	return snap
}

/*
================
crowdActor.WorldSnapshotInto

The movement lane's fill (sessionworld.go): the world copied under the
store's lock and a MovementCurrent check that compares the captured
movement fields with the store's.
================
*/
func (a *crowdActor) WorldSnapshotInto(dst *simulation.SessionSnapshot) {
	watch := a.worlds.SnapshotIntoWatched(a.key, &dst.World, a.seed)
	worlds, key := a.worlds, a.key
	lifeRevision, spawn, movementMode := dst.World.LifeRevision, dst.World.Spawn, dst.World.MovementMode
	var segment simulation.MoveSegment
	hasSegment := dst.World.MoveSegment != nil
	if hasSegment {
		segment = *dst.World.MoveSegment
	}
	dst.DivisionID = crowdDivision
	dst.WorldInstance = 0
	dst.CharacterID = a.char
	dst.CombatEligible = true
	dst.NativeBodyStatus = 0
	dst.MovementCurrent = func() bool {
		if watch.Unchanged() {
			return true
		}
		expected := simulation.WorldState{LifeRevision: lifeRevision, Spawn: spawn, MovementMode: movementMode}
		if hasSegment {
			expected.MoveSegment = &segment
		}
		return worlds.MovementCurrent(key, expected)
	}
	dst.Appearance = a.appearance
	dst.COS = nil
	dst.Companions = nil
	dst.NpcsEnabled = a.npcs
	dst.NpcAnchor = a.start
}

//============================================================================
// In-memory connection
//============================================================================

/*
================
memConn

Discards what the writer hands it, counting transport writes and
frames, and acks the stream once a second like the browser.
================
*/
type memConn struct {
	hello    transport.Frame
	helloed  atomic.Bool
	closed   chan struct{}
	once     sync.Once
	position atomic.Uint32
	welcome  chan transport.Welcome
	welcomed atomic.Bool
	writes   atomic.Int64
	frames   atomic.Int64
	// log, while recording, keeps each transport write's frames.
	mu        sync.Mutex
	recording bool
	log       [][]transport.Frame
}

/*
================
memConn.record

Starts keeping each write's frames from now on.
================
*/
func (c *memConn) record() {
	c.mu.Lock()
	c.recording = true
	c.mu.Unlock()
}

/*
================
memConn.recorded

The writes kept so far, each as the frames it carried.
================
*/
func (c *memConn) recorded() [][]transport.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]transport.Frame(nil), c.log...)
}

/*
================
newMemConn
================
*/
func newMemConn(ticket []byte) *memConn {
	return &memConn{
		hello:   transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: ticket})},
		closed:  make(chan struct{}),
		welcome: make(chan transport.Welcome, 1),
	}
}

/*
================
memConn.ReadFrame
================
*/
func (c *memConn) ReadFrame(ctx context.Context) (transport.Frame, error) {
	if c.helloed.CompareAndSwap(false, true) {
		return c.hello, nil
	}
	timer := time.NewTimer(crowdAckInterval)
	defer timer.Stop()
	select {
	case <-timer.C:
		payload := make([]byte, 4)
		binary.LittleEndian.PutUint32(payload, c.position.Load())
		return transport.Frame{Opcode: transport.OpStreamAck, Payload: payload}, nil
	case <-c.closed:
		return transport.Frame{}, errors.New("mem: closed")
	case <-ctx.Done():
		return transport.Frame{}, ctx.Err()
	}
}

/*
================
memConn.WriteFrame
================
*/
func (c *memConn) WriteFrame(f transport.Frame) error {
	return c.WriteFrames([]transport.Frame{f})
}

/*
================
memConn.WriteFrames
================
*/
func (c *memConn) WriteFrames(frames []transport.Frame) error {
	select {
	case <-c.closed:
		return errors.New("mem: closed")
	default:
	}
	for _, f := range frames {
		if f.Opcode == transport.OpWelcome && c.welcomed.CompareAndSwap(false, true) {
			if w, err := transport.DecodeWelcome(f.Payload); err == nil {
				c.welcome <- w
			}
		}
		if transport.CountsOnStream(f.Opcode) {
			c.position.Add(1)
		}
	}
	c.mu.Lock()
	if c.recording {
		kept := make([]transport.Frame, len(frames))
		for i, f := range frames {
			kept[i] = transport.Frame{Opcode: f.Opcode, Payload: append([]byte(nil), f.Payload...)}
		}
		c.log = append(c.log, kept)
	}
	c.mu.Unlock()
	c.writes.Add(1)
	c.frames.Add(int64(len(frames)))
	return nil
}

func (c *memConn) SupportsUnreliable() bool              { return false }
func (c *memConn) WriteUnreliable(transport.Frame) error { return errors.New("mem: no datagrams") }
func (c *memConn) Kind() string                          { return "websocket" }
func (c *memConn) RemoteAddr() string                    { return "mem" }

/*
================
memConn.Close
================
*/
func (c *memConn) Close(string) error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

//============================================================================
// Loopback WebSocket client
//============================================================================

/*
================
wsReader

What one prompt reader saw: the longest wait between two messages and
whether its connection ended.
================
*/
type wsReader struct {
	maxGapNs atomic.Int64
	closed   atomic.Bool
}

/*
================
dialCrowdClient

Dials the server's WebSocket listener, presents HELLO, reads WELCOME and
leaves a reader goroutine that reads promptly, answers PING and acks the
stream once a second.
================
*/
func dialCrowdClient(tb testing.TB, srv *transport.Server, ticket []byte, reader *wsReader) (*websocket.Conn, transport.Welcome) {
	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		tb.Fatalf("ws dial: %v", err)
	}
	hello := transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: ticket})}
	if err := c.WriteMessage(websocket.BinaryMessage, hello.Encode()); err != nil {
		tb.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := c.ReadMessage()
	if err != nil {
		tb.Fatalf("welcome read: %v", err)
	}
	f, err := transport.DecodeFrame(data)
	if err != nil || f.Opcode != transport.OpWelcome {
		tb.Fatalf("first frame %v %v", f.Opcode, err)
	}
	w, err := transport.DecodeWelcome(f.Payload)
	if err != nil {
		tb.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Time{})
	go readCrowdClient(c, reader)
	return c, w
}

/*
================
readCrowdClient
================
*/
func readCrowdClient(c *websocket.Conn, reader *wsReader) {
	var position uint32
	lastAck := time.Now()
	last := time.Now()
	head := make([]byte, 2)
	for {
		_, r, err := c.NextReader()
		if err != nil {
			reader.closed.Store(true)
			return
		}
		now := time.Now()
		if gap := now.Sub(last).Nanoseconds(); gap > reader.maxGapNs.Load() {
			reader.maxGapNs.Store(gap)
		}
		last = now
		if _, err := io.ReadFull(r, head); err != nil {
			reader.closed.Store(true)
			return
		}
		opcode := binary.LittleEndian.Uint16(head)
		if opcode == transport.OpPing {
			payload, _ := io.ReadAll(r)
			_ = c.WriteMessage(websocket.BinaryMessage, transport.Frame{Opcode: transport.OpPong, Payload: payload}.Encode())
		} else {
			_, _ = io.Copy(io.Discard, r)
		}
		if transport.CountsOnStream(opcode) {
			position++
		}
		if now.Sub(lastAck) >= crowdAckInterval {
			lastAck = now
			payload := make([]byte, 4)
			binary.LittleEndian.PutUint32(payload, position)
			_ = c.WriteMessage(websocket.BinaryMessage, transport.Frame{Opcode: transport.OpStreamAck, Payload: payload}.Encode())
		}
	}
}

//============================================================================
// The crowd
//============================================================================

/*
================
crowdConfig
================
*/
type crowdConfig struct {
	ws      bool
	players int
	walkers int
	// hotan stands the crowd on the live plaza (hotanCenter) with the
	// shipped NPC roster, NPCs enabled for every member, as the scratch
	// stack's holds run it; false is the one-block plaza without NPCs.
	hotan bool
}

/*
================
crowd
================
*/
type crowd struct {
	srv      *transport.Server
	certDir  string
	hub      *transport.Hub
	ticker   *simulation.Ticker
	worlds   *simulation.WorldStore
	handlers *Bridge
	actors   []*crowdActor
	sessions []*transport.Session
	mems     []*memConn
	readers  []*wsReader
	clients  []*websocket.Conn
	rng      *rand.Rand
}

/*
================
startCrowdServer
================
*/
func startCrowdServer(tb testing.TB, certDir string) *transport.Server {
	srv, err := transport.NewServer(transport.Config{
		WTAddr:       "127.0.0.1:0",
		WSAddr:       "127.0.0.1:0",
		CertDir:      certDir,
		MaxSessions:  20000,
		HelloTimeout: 10 * time.Second,
	})
	if err != nil {
		tb.Fatal(err)
	}
	var admitted atomic.Int64
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "crowd-" + strconv.FormatInt(admitted.Add(1), 10), ShardID: "global-official"}, nil
	})
	srv.Hub.SetEnterWorldAuth(func(*transport.Session, transport.EnterWorld) (bool, uint32) { return true, 0 })
	if err := srv.Start(); err != nil {
		tb.Fatal(err)
	}
	return srv
}

/*
================
pointInDisc
================
*/
func pointInDisc(rng *rand.Rand, center simulation.Spawn, radius float64) simulation.Spawn {
	angle := rng.Float64() * 2 * math.Pi
	distance := math.Sqrt(rng.Float64()) * radius
	p := center
	p.X += math.Cos(angle) * distance
	p.Z += math.Sin(angle) * distance
	return p
}

/*
================
newCrowd

Attaches cfg.players in-world sessions around the plaza and installs
their providers; the first cfg.walkers walk. The caller owns close.
================
*/
func newCrowd(tb testing.TB, cfg crowdConfig) *crowd {
	// Not tb.TempDir: a benchmark's crowd outlives the run that built it.
	certDir, err := os.MkdirTemp("", "crowd-certs-")
	if err != nil {
		tb.Fatal(err)
	}
	srv := startCrowdServer(tb, certDir)
	c := &crowd{srv: srv, hub: srv.Hub, certDir: certDir, worlds: simulation.NewWorldStore(), rng: rand.New(rand.NewSource(7))}
	c.handlers = New(c.hub)
	for i := 0; i < cfg.players; i++ {
		ticket := []byte("crowd-ticket")
		var welcome transport.Welcome
		if cfg.ws {
			reader := &wsReader{}
			client, w := dialCrowdClient(tb, srv, ticket, reader)
			c.readers = append(c.readers, reader)
			c.clients = append(c.clients, client)
			welcome = w
		} else {
			conn := newMemConn(ticket)
			go c.hub.AcceptConn(conn)
			select {
			case welcome = <-conn.welcome:
			case <-time.After(10 * time.Second):
				tb.Fatalf("session %d: no welcome", i)
			}
			c.mems = append(c.mems, conn)
		}
		s, ok := c.hub.Session(welcome.SessionID)
		if !ok {
			tb.Fatalf("session %d missing", i)
		}
		if err := s.SendSceneReset([]transport.Frame{{Opcode: transport.OpEnterWorldResult, Payload: []byte{1}}}); err != nil {
			tb.Fatal(err)
		}
		s.FinishSceneReentry()
		name := fmt.Sprintf("crowd%04d", i)
		start := pointInDisc(c.rng, crowdCenter, crowdRadius)
		if cfg.hotan {
			start = hotanSpot(c.rng, i)
		}
		actor := &crowdActor{
			worlds:     c.worlds,
			key:        simulation.WorldKey(crowdDivision, name),
			char:       int64(i + 1),
			sceneID:    SessionSceneID(s),
			appearance: &simulation.PeerAppearance{Name: name, ActionSpeed: 100, RefObjID: 1907},
			start:      start,
			walker:     i < cfg.walkers,
			npcs:       cfg.hotan,
		}
		c.worlds.Update(actor.key, actor.seed, func(*simulation.WorldState) {})
		s.SetWorldSnapshot(crowdDivision, actor)
		c.actors = append(c.actors, actor)
		c.sessions = append(c.sessions, s)
	}
	var roster []simulation.NpcDef
	if cfg.hotan {
		roster = crowdNpcRoster(tb)
	}
	c.ticker = NewTicker(c.hub, roster)
	return c
}

/*
================
hotanSpot

The i-th member's spot on the live plaza: the inner disc first, then
the ring (area-uniform in both), heights left to the plaza's.
================
*/
func hotanSpot(rng *rand.Rand, i int) simulation.Spawn {
	if i < hotanInnerSpots {
		return pointInDisc(rng, hotanCenter, hotanInnerRadius)
	}
	angle := rng.Float64() * 2 * math.Pi
	inner, outer := hotanInnerRadius*hotanInnerRadius, hotanOuterRadius*hotanOuterRadius
	distance := math.Sqrt(inner + rng.Float64()*(outer-inner))
	p := hotanCenter
	p.X += math.Cos(angle) * distance
	p.Z += math.Sin(angle) * distance
	return p
}

/*
================
crowdNpcRoster

The shipped NPC roster and teleport gates, as the GameWorld boots them
(enterworld/devdeps.go). A tree without the game-data projection runs
the crowd without NPCs and says so.
================
*/
func crowdNpcRoster(tb testing.TB) []simulation.NpcDef {
	dir := gamedatatest.TextdataDirOrEmpty()
	if dir == "" {
		tb.Log("crowd: no game data, the plaza has no NPCs")
		return nil
	}
	roster, err := simulation.AppendTeleportGates(dir, simulation.LoadNpcWorldRoster(dir))
	if err != nil {
		tb.Fatalf("crowd: NPC roster: %v", err)
	}
	return roster
}

/*
================
crowd.click

One accepted move to goal at nowMs, as the movement lane commits it:
the world updated under the store's lock and the mover's own 0xB738
pushed through a handler bridge, outside the tick. Returns the move's
arrival time, 0 when it made no segment.
================
*/
func (c *crowd) click(a *crowdActor, goal simulation.Spawn, nowMs int64) int64 {
	var result simulation.MoveResult
	c.worlds.Update(a.key, a.seed, func(w *simulation.WorldState) {
		result = simulation.ApplyMove(w, simulation.PlayerObjectID(a.char), simulation.MovementRequest{
			Mode: simulation.MovementAckDestinationMode, RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z,
		}, simulation.RunMode, nowMs)
	})
	c.handlers.PushToSession(a.sceneID, []simulation.Frame{{Opcode: simulation.OpMovementAck, Payload: result.AckPayload}})
	if result.Segment == nil {
		return 0
	}
	return result.Segment.ArrivesAtMs
}

/*
================
crowd.walkBots

The walkers' clicks beside the tick, as the session read loops make
them: every 25 ms, each walker whose pause is over clicks a point near
its home. Returns when ctx ends; clicks counts the moves.
================
*/
func (c *crowd) walkBots(ctx context.Context, clicks *atomic.Int64) {
	rng := rand.New(rand.NewSource(11))
	clock := time.NewTicker(25 * time.Millisecond)
	defer clock.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-clock.C:
			nowMs := now.UnixMilli()
			for _, a := range c.actors {
				if !a.walker || nowMs < a.nextMs {
					continue
				}
				c.click(a, pointInDisc(rng, a.start, crowdBotStride), nowMs)
				clicks.Add(1)
				a.nextMs = nowMs + crowdClickPauseMs + rng.Int63n(crowdClickSpreadMs)
			}
		}
	}
}

/*
================
crowd.close

Ends the crowd: the clients first, then the server.
================
*/
func (c *crowd) close() {
	for _, client := range c.clients {
		_ = client.Close()
	}
	for _, conn := range c.mems {
		_ = conn.Close("done")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.srv.Shutdown(ctx)
	_ = os.RemoveAll(c.certDir)
}

//============================================================================
// The property
//============================================================================

/*
================
countingPusher

The ticker's bridge with a count of the frames each session was pushed,
so a test knows when every writer has drained. Its window is the
bridge's.
================
*/
type countingPusher struct {
	*Bridge
	mu     sync.Mutex
	pushed map[string]int
}

/*
================
countingPusher.PushToSession
================
*/
func (p *countingPusher) PushToSession(sessionID string, frames []simulation.Frame) {
	p.mu.Lock()
	p.pushed[sessionID] += len(frames)
	p.mu.Unlock()
	p.Bridge.PushToSession(sessionID, frames)
}

/*
================
countingPusher.count
================
*/
func (p *countingPusher) count(sessionID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pushed[sessionID]
}

/*
================
TestCrowdTickWritesOncePerSession

Half a small crowd walks across the plaza while the tick runs; every
session sees the walkers' live samples every tick. Each session's tick
output must leave in exactly one transport write per tick, carrying
every frame the tick pushed to it.
================
*/
func TestCrowdTickWritesOncePerSession(t *testing.T) {
	const (
		players = 24
		walkers = 12
		ticks   = 10
		tickMs  = 100
	)
	c := newCrowd(t, crowdConfig{players: players, walkers: walkers})
	t.Cleanup(c.close)
	// The port-only position stream gives every session output on every
	// tick (a live sample per walker); the native fan-out sends a walk's
	// path once, which leaves most ticks with nothing to write.
	c.ticker.PositionStream = true
	pusher := &countingPusher{Bridge: c.ticker.Push.(*Bridge), pushed: map[string]int{}}
	c.ticker.Push = pusher
	nowMs := time.Now().UnixMilli()
	for i, a := range c.actors[:walkers] {
		// Across the plaza and back out of it: a walk far longer than
		// the test, so no walker arrives (and settles) inside it.
		angle := 2 * math.Pi * float64(i) / walkers
		goal := crowdCenter
		goal.X -= math.Cos(angle) * crowdRadius
		goal.Z -= math.Sin(angle) * crowdRadius
		a.start = crowdCenter
		a.start.X += math.Cos(angle) * crowdRadius
		a.start.Z += math.Sin(angle) * crowdRadius
		c.worlds.Update(a.key, a.seed, func(w *simulation.WorldState) { w.Spawn = a.start })
		if arrives := c.click(a, goal, nowMs); arrives < nowMs+(ticks+1)*tickMs {
			t.Fatalf("walker %d arrives at +%d ms, inside the test's %d ticks", i, arrives-nowMs, ticks)
		}
	}
	// Every session drained its entry and the walkers their acks; what
	// the connections counted so far is not the tick's.
	wait.Eventually(t, 5*time.Second, "the setup frames written", func() bool {
		return c.hub.Metrics().OutboundQueueDepth == 0
	})
	writes, frames := make([]int64, players), make([]int64, players)
	for i, conn := range c.mems {
		writes[i], frames[i] = conn.writes.Load(), conn.frames.Load()
	}
	for tick := 1; tick <= ticks; tick++ {
		c.ticker.RunTick(nowMs + int64(tick*tickMs))
		wait.Eventually(t, 5*time.Second, fmt.Sprintf("tick %d's frames written", tick), func() bool {
			for i, s := range c.sessions {
				if c.mems[i].frames.Load()-frames[i] < int64(pusher.count(SessionSceneID(s))) {
					return false
				}
			}
			return true
		})
	}
	for i, s := range c.sessions {
		if got := c.mems[i].writes.Load() - writes[i]; got != ticks {
			t.Fatalf("session %d: %d transport writes over %d ticks, want one a tick", i, got, ticks)
		}
		if pushed := pusher.count(SessionSceneID(s)); pushed < ticks*(walkers-1) {
			t.Fatalf("session %d was pushed %d frames over %d ticks, want at least %d", i, pushed, ticks, ticks*(walkers-1))
		}
	}
}
