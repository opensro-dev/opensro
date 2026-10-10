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

// SessionIDString is the canonical mapping from a transport session ID to
// the string ids the simulation package speaks.
func SessionIDString(id uint64) string { return strconv.FormatUint(id, 10) }

// Bridge implements simulation.SessionSource and simulation.Pusher on a Hub.
type Bridge struct {
	hub *transport.Hub
	// Installed at composition before ticking. The action owner supplies the
	// authenticated membership lease; a wire world ID cannot manufacture it.
	PopulationLease func(division, name string, session uint64) (instance.Lease, bool)
}

// New builds the bridge. One per Hub is enough; it is stateless.
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
	hooks ...simulation.TickHook,
) *simulation.Ticker {
	bridge := New(hub)
	ticker := simulation.NewTicker(bridge, bridge)
	ticker.Roster = append([]simulation.NpcDef(nil), roster...)
	ticker.Hooks = append(ticker.Hooks, hooks...)
	ticker.Hooks = append(ticker.Hooks, tickDurationHook(hub, ticker))
	ticker.PhaseObserver = tickPhaseObserver(hub)
	return ticker
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

// SnapshotSessions lists the in-world sessions for one tick.
func (b *Bridge) SnapshotSessions() []simulation.SessionSnapshot {
	return b.snapshot(false)
}

/*
==================
SnapshotSessionViews

The same sessions as SnapshotSessions, without building their peer
presentation (action speed, spawn skills, guild, stall, companions): the
tick hooks that read only identity, population, publication and world.
==================
*/
func (b *Bridge) SnapshotSessionViews() []simulation.SessionView {
	snaps := b.snapshot(true)
	views := make([]simulation.SessionView, len(snaps))
	for i := range snaps {
		views[i] = snaps[i].View()
	}
	return views
}

/*
==================
snapshot

One walk for both faces: light skips the peer presentation where the
provider offers WorldView. Every session filter is shared.
==================
*/
func (b *Bridge) snapshot(light bool) []simulation.SessionSnapshot {
	sessions := b.hub.Sessions()
	out := make([]simulation.SessionSnapshot, 0, len(sessions))
	for _, s := range sessions {
		if s.Evicted() {
			// A session evicted by the single-bind swap is a
			// lame duck — it neither drives the tick nor receives its
			// pushes while the BYE drains. (Delivery is also refused at
			// the Session level; this skip stops its snapshot from
			// feeding the world computation at all.)
			continue
		}
		revision, active := s.SceneRevision()
		if !active {
			continue
		}
		v, ok := s.WorldSnapshot()
		if !ok {
			continue
		}
		provider, ok := v.(SnapshotProvider)
		if !ok {
			continue
		}
		var snap simulation.SessionSnapshot
		if viewer, ok := v.(ViewProvider); ok && light {
			view := viewer.WorldView()
			snap = simulation.SessionSnapshot{DivisionID: view.DivisionID, CharacterID: view.CharacterID,
				WorldInstance: view.WorldInstance, World: view.World}
		} else {
			snap = provider.WorldSnapshot()
		}
		if b.PopulationLease != nil {
			division, name, bound := s.CharacterBinding()
			if !bound {
				continue
			}
			lease, valid := b.PopulationLease(division, name, s.ID)
			if !valid || uint32(lease.ID) != snap.WorldInstance {
				continue
			}
			snap.Population = lease
		}
		snap.SessionID = sceneSessionID(s.ID, revision)
		published, current := s.PublishedObjects(revision)
		if !current {
			continue
		}
		snap.PublishedObjects = published
		if snap.DivisionID == "" {
			if div, ok := s.DivisionID(); ok {
				snap.DivisionID = div
			}
		}
		out = append(out, snap)
	}
	return out
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
	identity, generation, scoped := strings.Cut(sessionID, ":")
	revision := uint64(0)
	if scoped {
		var err error
		revision, err = strconv.ParseUint(generation, 10, 64)
		if err != nil {
			return
		}
	}
	id, err := strconv.ParseUint(identity, 10, 64)
	if err != nil {
		return
	}
	s, ok := b.hub.Session(id)
	if !ok {
		return
	}
	b.deliver(s, frames, revision)
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
