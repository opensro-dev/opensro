/*
===========================================================================

bridge.go - the transport hub to simulation tick bridge

Package worldsession adapts the transport Hub to the simulation tick's
SessionSource and Pusher contracts
(internal/game/world/simulation/tick.go), so the 100ms server push actually
reaches connected clients.

Wiring (in main, after transport.NewServerFromViper and before Start):

	bridge := worldsession.New(ts.Hub)
	ticker := simulation.NewTicker(bridge, bridge)
	go ticker.Run(ctx)

The movement lane makes a session visible to the tick after EnterWorld:

	sess.SetWorldSnapshot(divisionID, provider)

Delivery honors the transport reliability classes: loss-tolerant frames
(0x30E3/0xB2F5) in movement-only batches ride SendUnreliableKeyed with
the entity gid as coalesce key. Mixed action batches use one reliable
enqueue, preserving movement preludes and result/HP/life order.
Neither path blocks on network I/O, which is the Pusher contract.

===========================================================================
*/
package worldsession

import (
	"encoding/binary"
	"strconv"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
==================
SnapshotProvider

SnapshotProvider yields the session's tick snapshot. Implementations
must return value copies taken under their own lock (CloneWorldState for
the world) per the SessionSnapshot concurrency contract; the bridge
overwrites SessionID with the canonical transport id.
==================
*/
type SnapshotProvider interface {
	WorldSnapshot() simulation.SessionSnapshot
}

/*
==================
ViewProvider

An optional, cheaper SnapshotProvider face: the session's identity and
world without its peer presentation (simulation.SessionView). A provider
without it is viewed through its full WorldSnapshot.
==================
*/
type ViewProvider interface {
	WorldView() simulation.SessionView
}

/*
==================
SnapshotIntoProvider

An optional SnapshotProvider face that fills a retained snapshot slot
in place (#2989), the fill-in-place pattern of the monster leg
(#2984/#2985): every field of the slot is written, so the bridge can
publish from the same slots each tick instead of allocating a fresh
snapshot of every session. The bridge-owned fields (SessionID,
Population, PublishedObjects) stay the bridge's to write. A provider
without it is read through WorldSnapshot and copied into the slot.
==================
*/
type SnapshotIntoProvider interface {
	WorldSnapshotInto(dst *simulation.SessionSnapshot)
}

// SessionIDString is the canonical mapping from a transport session ID to
// the string ids the simulation package speaks.
func SessionIDString(id uint64) string { return strconv.FormatUint(id, 10) }

// Bridge implements simulation.SessionSource and simulation.Pusher on a Hub.
type Bridge struct {
	hub *transport.Hub
	// Installed at composition before ticking. The action owner supplies the
	// authenticated membership lease; a wire world ID cannot manufacture it.
	PopulationLease func(division, name string, session uint64) (instance.Lease, bool)
	// resumed is the one-shot registry of resumed scene identities
	// (resume.go), written by the hub hook and drained by the ticker.
	resumeMu sync.Mutex
	resumed  map[string]struct{}
	// tickViews is the light face of the most recent full snapshot walk,
	// shared read-only by that tick's view hooks (SnapshotSessionViews).
	// The next full walk replaces it wholesale; it is never mutated.
	//
	// snapshots is the full face of that same walk, retained with it
	// (#2989): every reader of the full walk is tick-scoped (the tick's
	// prepare and the legs and hooks it feeds - the lifetime the monster
	// leg snapshot already relies on), so the next walk fills the same
	// slots in place instead of allocating a fresh snapshot of every
	// session each tick. snapIDs keeps each scene's canonical id string
	// across walks: a scene's id changes only with its revision, so the
	// string built for it is built once. viewsMu guards all three.
	viewsMu   sync.Mutex
	tickViews []simulation.SessionView
	snapshots []simulation.SessionSnapshot
	snapIDs   map[sceneIDKey]string
}

// sceneIDKey identifies one scene admission: the transport session and
// its scene revision, the two inputs of sceneSessionID.
type sceneIDKey struct {
	id, revision uint64
}

// New builds the bridge. One per Hub is enough.
func New(hub *transport.Hub) *Bridge { return &Bridge{hub: hub} }

/*
==================
NewTicker

NewTicker is the boot-time composition: bridge + simulation Ticker on the
default 100ms cadence, with an explicitly owned NPC roster and the given
tick hooks (the ground-item TTL sweep rides here so it shares the tick
clock). The roster is copied so bootstrap configuration cannot mutate a
running ticker. Pass nil deliberately when NPC ticking is disabled. The
caller starts it:

	ticker := worldsession.NewTicker(ts.Hub, npcRoster, actions.TickHook())
	go ticker.Run(ctx)

It also wires the tick-duration seam (Hub.RecordTickDuration, surfaced
on /transport/metrics) as a final TickHook, so the production
composition records tick health with no extra wiring in main. The hook
is appended AFTER the caller's hooks to include their cost; hooks the
owner appends to ticker.Hooks after this call run behind the sample.
==================
*/
func NewTicker(
	hub *transport.Hub,
	roster []simulation.NpcDef,
	hooks ...simulation.NamedTickHook,
) *simulation.Ticker {
	bridge := New(hub)
	// The ticker bridge owns the resume registry (resume.go): a resumed
	// session is marked here and re-pushed by the ticker's next tick. Only
	// this composition registers the hook - the ad-hoc bridges other lanes
	// build with New (public API, observatory, notices) never drain one.
	// A nil hub is a test composition without transport; it never resumes.
	if hub != nil {
		hub.OnSessionResumed(bridge.noteSessionResumed)
	}
	ticker := simulation.NewTicker(bridge, bridge)
	ticker.Roster = append([]simulation.NpcDef(nil), roster...)
	ticker.Hooks = append(ticker.Hooks, hooks...)
	ticker.Hooks = append(ticker.Hooks, simulation.NamedTickHook{Name: "tick-duration", Hook: tickDurationHook(hub, ticker)})
	ticker.PhaseObserver = tickPhaseObserver(hub)
	// Exact per-zone tick totals (#2824), always on: the budget read
	// for #2150 diffs two /transport/metrics snapshots of tick_zones.
	ticker.Zones = simulation.NewZoneLedger()
	if hub != nil {
		hub.SetTickZoneMetrics(tickZoneMetrics(ticker.Zones))
	}
	// Slow-tick zone timelines (#2301): armed only by SRO_TICK_TRACE,
	// otherwise every zone point answers the shared no-op and the
	// tick pays the nil checks. The sink hands each slow tick's
	// rendered trace to the hub's writer (directory enabled at boot
	// in wiring.go, beside the spike journal).
	if transport.TickTraceEnabled() {
		ticker.Trace = simulation.NewTickTracer()
		ticker.TraceSink = func(trace simulation.TickTrace) {
			if hub != nil {
				hub.RecordTickTrace(trace.JSON)
			}
		}
	}
	return ticker
}

/*
==================
tickZoneMetrics

The tick_zones provider: the ledger's rows keyed "<kind>:<name>", in
milliseconds.
==================
*/
func tickZoneMetrics(ledger *simulation.ZoneLedger) func() map[string]transport.TickZone {
	return func() map[string]transport.TickZone {
		rows := ledger.Snapshot()
		out := make(map[string]transport.TickZone, len(rows))
		for _, row := range rows {
			out[row.Kind+":"+row.Name] = transport.TickZone{
				Count:   row.Count,
				TotalMs: float64(row.Total) / float64(time.Millisecond),
			}
		}
		return out
	}
}

/*
==================
tickDurationHook

tickDurationHook measures each tick from its nowMs start stamp (the
tick's own clock, millisecond-truncated — coarse but honest against a
100ms budget) and records it on the hub. Returns no frames; costs one
time.Now per tick.
==================
*/
func tickDurationHook(hub *transport.Hub, ticker *simulation.Ticker) simulation.TickHook {
	return func(nowMs int64) []simulation.DivisionFrames {
		interval := ticker.Interval
		if interval <= 0 {
			interval = simulation.DefaultTickInterval
		}
		hub.RecordTickDuration(time.Since(time.UnixMilli(nowMs)), interval)
		return nil
	}
}

/*
==================
tickPhaseObserver

Hands each tick's phase durations and slow hooks to the hub's histograms
(transport/timing.go), converting between the two packages' plain types.
==================
*/
func tickPhaseObserver(hub *transport.Hub) func(simulation.TickTiming) {
	return func(timing simulation.TickTiming) {
		phases := transport.TickPhases{
			BeforeHooks: timing.BeforeHooks, Divisions: timing.Divisions, Hooks: timing.Hooks, Total: timing.Total,
			Commit: transport.TickCommit{Deferred: timing.Commit.Deferred, Rows: timing.Commit.Rows,
				Elapsed: timing.Commit.Elapsed, Flushed: timing.Commit.Flushed, Failed: timing.Commit.Failed},
		}
		for _, hook := range timing.SlowHooks {
			phases.SlowHooks = append(phases.SlowHooks, transport.SlowHook{Name: hook.Name, Elapsed: hook.Elapsed})
		}
		for _, division := range timing.SlowDivisions {
			phases.SlowDivisions = append(phases.SlowDivisions, transport.SlowHook{Name: division.Name, Elapsed: division.Elapsed})
		}
		hub.RecordTickPhases(phases)
	}
}

/*
==================
SnapshotSessions

Every in-world session's full snapshot for one tick, in hub order.
==================
*/
func (b *Bridge) SnapshotSessions() []simulation.SessionSnapshot {
	sessions := b.hub.Sessions()
	b.viewsMu.Lock()
	defer b.viewsMu.Unlock()
	// Scene ids outlive their scenes in the cache (a closed session's id
	// is never asked again); bound it by the live hub size and rebuild
	// lazily rather than tracking closures.
	if len(b.snapIDs) > 4*len(sessions)+64 {
		clear(b.snapIDs)
	}
	snaps := b.snapshots
	n := 0
	for _, s := range sessions {
		if n == len(snaps) {
			snaps = append(snaps, simulation.SessionSnapshot{})
		}
		if b.sessionSnapshotInto(&snaps[n], s, false) {
			n++
		}
	}
	snaps = snaps[:n]
	b.snapshots = snaps
	// The tick's view hooks read the light face of this same walk
	// (SnapshotSessionViews), so no hook walks the hub again this tick.
	// The views stay a fresh slice per walk as before: out-of-tick
	// readers (the public API's online checks) hold them across walks,
	// so only their elements' world pointees are shared with the slots,
	// and those readers never read the world.
	views := make([]simulation.SessionView, len(snaps))
	for i := range snaps {
		views[i] = snaps[i].View()
	}
	b.tickViews = views
	return snaps
}

/*
==================
SnapshotSessionViews

The same sessions as SnapshotSessions, without building their peer
presentation (action speed, spawn skills, guild, stall, companions): the
tick hooks that read only identity, population, publication and world.
Each view is taken straight from its session's snapshot, so no full
snapshot slice is built just to be narrowed.

Within a tick, the full walk (SnapshotSessions) runs first and leaves
its light face behind; the hooks then share that one slice instead of
each walking the hub again. The shared views are read-only: no reader
may modify the slice or its elements. Before the first full walk of a
bridge (callers outside a tick) a fresh light walk is built and not
kept.
==================
*/
func (b *Bridge) SnapshotSessionViews() []simulation.SessionView {
	b.viewsMu.Lock()
	defer b.viewsMu.Unlock()
	if views := b.tickViews; views != nil {
		return views
	}
	sessions := b.hub.Sessions()
	views := make([]simulation.SessionView, 0, len(sessions))
	for _, s := range sessions {
		var snap simulation.SessionSnapshot
		if b.sessionSnapshotInto(&snap, s, true) {
			views = append(views, snap.View())
		}
	}
	return views
}

/*
==================
sessionSnapshotInto

One session's snapshot for both faces, written into dst: light skips the
peer presentation where the provider offers WorldView. Every session
filter is here, so the full walk and the views agree on which sessions
are in the world. A false return leaves dst holding a previous fill;
callers only publish slots this call filled.
==================
*/
func (b *Bridge) sessionSnapshotInto(dst *simulation.SessionSnapshot, s *transport.Session, light bool) bool {
	if s.Evicted() {
		// A session evicted by the single-bind swap is a
		// lame duck — it neither drives the tick nor receives its
		// pushes while the BYE drains. (Delivery is also refused at
		// the Session level; this skip stops its snapshot from
		// feeding the world computation at all.)
		return false
	}
	revision, active := s.SceneRevision()
	if !active {
		return false
	}
	v, ok := s.WorldSnapshot()
	if !ok {
		return false
	}
	provider, ok := v.(SnapshotProvider)
	if !ok {
		return false
	}
	if viewer, ok := v.(ViewProvider); ok && light {
		view := viewer.WorldView()
		*dst = simulation.SessionSnapshot{DivisionID: view.DivisionID, CharacterID: view.CharacterID,
			WorldInstance: view.WorldInstance, World: view.World}
	} else if into, ok := v.(SnapshotIntoProvider); ok {
		into.WorldSnapshotInto(dst)
	} else {
		*dst = provider.WorldSnapshot()
	}
	if b.PopulationLease != nil {
		division, name, bound := s.CharacterBinding()
		if !bound {
			return false
		}
		lease, valid := b.PopulationLease(division, name, s.ID)
		if !valid || uint32(lease.ID) != dst.WorldInstance {
			return false
		}
		dst.Population = lease
	} else {
		// A retained slot may carry a previous walk's lease; the field
		// is the bridge's, so a bridge without leases clears it.
		dst.Population = instance.Lease{}
	}
	dst.SessionID = b.sceneID(s.ID, revision)
	// Shared, not copied: the full walk and every view hook read this set
	// each tick, and every tick reader only reads it (SessionSnapshot).
	published, current := s.PublishedObjectsShared(revision)
	if !current {
		return false
	}
	dst.PublishedObjects = published
	if dst.DivisionID == "" {
		if div, ok := s.DivisionID(); ok {
			dst.DivisionID = div
		}
	}
	return true
}

/*
==================
sceneID

The canonical scene id string for one session admission, kept across
walks (#2989): the id is a pure function of (session, revision), both
stable for an admission's life, so the string is built once per
admission instead of once per session per tick. Callers hold viewsMu.
==================
*/
func (b *Bridge) sceneID(id, revision uint64) string {
	key := sceneIDKey{id: id, revision: revision}
	if cached, ok := b.snapIDs[key]; ok {
		return cached
	}
	built := sceneSessionID(id, revision)
	if b.snapIDs == nil {
		b.snapIDs = make(map[sceneIDKey]string)
	}
	b.snapIDs[key] = built
	return built
}

/*
==================
PushToSession

PushToSession delivers tick frames to one session. Unknown ids are
dropped silently: the session closed between snapshot and push, which is
normal churn.
==================
*/
func (b *Bridge) PushToSession(sessionID string, frames []simulation.Frame) {
	s, revision, ok := b.SceneSession(sessionID)
	if !ok {
		return
	}
	b.deliver(s, frames, revision)
}

/*
==================
SceneSession

The live session and scene revision a tick session id names (sceneSessionID).
An unscoped id has revision 0; a closed session or malformed id is absent.
==================
*/
func (b *Bridge) SceneSession(sessionID string) (*transport.Session, uint64, bool) {
	identity, generation, scoped := strings.Cut(sessionID, ":")
	revision := uint64(0)
	if scoped {
		var err error
		revision, err = strconv.ParseUint(generation, 10, 64)
		if err != nil {
			return nil, 0, false
		}
	}
	id, err := strconv.ParseUint(identity, 10, 64)
	if err != nil {
		return nil, 0, false
	}
	s, ok := b.hub.Session(id)
	if !ok {
		return nil, 0, false
	}
	return s, revision, true
}

/*
==================
PushToDivision

PushToDivision delivers tick frames to every session in a division,
skipping exceptSessionID (empty string skips nobody). It iterates the
hub's division index — only that division's members, not every live
session (the old full-scan-and-filter was O(sessions) per push, 10Hz x
movers). The index is maintained by the session's explicit player-context
methods, so membership here and snapshot identity agree by construction;
evicted members are still enumerated but delivery to
them is refused at the Session level, exactly as before.
==================
*/
func (b *Bridge) PushToDivision(divisionID string, frames []simulation.Frame, exceptSessionID string) {
	for _, s := range b.hub.SessionsInDivision(divisionID) {
		if exceptSessionID != "" && SessionIDString(s.ID) == strings.SplitN(exceptSessionID, ":", 2)[0] {
			continue
		}
		revision, active := s.SceneRevision()
		if !active {
			continue
		}
		b.deliver(s, frames, revision)
	}
}

func (b *Bridge) deliver(s *transport.Session, frames []simulation.Frame, revision uint64) {
	var changes []transport.ObjectScopeChange
	ordered := false
	for _, f := range frames {
		ordered = ordered || f.Current != nil || !transport.IsLossTolerantOpcode(f.Opcode)
		changes = append(changes, transport.ScopeChanges(f.Scope)...)
		if f.ScopeGID != 0 {
			changes = append(changes, transport.ObjectScopeChange{GID: f.ScopeGID, Visible: f.ScopeVisible})
		}
	}
	if len(changes) != 0 || ordered {
		batch := make([]transport.Frame, len(frames))
		for i, f := range frames {
			batch[i] = transport.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current}
		}
		if len(changes) != 0 {
			_ = s.PublishSceneObjects(revision, changes, batch)
		} else {
			// A facing/result/HP/life burst is one transaction. Splitting it
			// across reliable and lossy queues lets B245 overtake its facing;
			// separate Send calls also admit unrelated frames in its middle.
			_ = s.SendSceneBatch(revision, batch)
		}
		return
	}
	for _, f := range frames {
		// Movement-only batches retain the loss-tolerant coalesced lane.
		_ = s.SendSceneUnreliableKeyed(revision, f.Opcode, moverGid(f), f.Payload)
	}
}

/*
==================
moverGid

moverGid extracts the entity gid used as the coalesce key. The two
loss-tolerant payloads place it differently (both wire-pinned, see
internal/game/item/wire/objectmove.go):

	0x30E3 [u16 region][f32 x][f32 y][f32 z][u16 heading][u32 gid] — LAST
	0xB2F5 [u32 gid][u16 region][f32 x][f32 y][f32 z][u16 heading] — FIRST

A malformed payload coalesces under key 0 rather than being dropped;
degraded but safe, and the parity tests upstream make it unreachable.
==================
*/
func moverGid(f simulation.Frame) uint64 {
	if len(f.Payload) != 20 {
		return 0
	}
	switch f.Opcode {
	case transport.OpObjectSourceMove:
		return uint64(binary.LittleEndian.Uint32(f.Payload[16:20]))
	case transport.OpObjectSourceCorrection:
		return uint64(binary.LittleEndian.Uint32(f.Payload[0:4]))
	default:
		return 0
	}
}

func sceneSessionID(id, revision uint64) string {
	if revision == 0 {
		return SessionIDString(id)
	}
	return SessionIDString(id) + ":" + strconv.FormatUint(revision, 10)
}

// SessionSceneID identifies the current actor admission, including re-entry.
func SessionSceneID(s *transport.Session) string {
	revision, _ := s.SceneRevision()
	return sceneSessionID(s.ID, revision)
}
