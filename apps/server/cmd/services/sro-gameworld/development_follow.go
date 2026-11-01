/*
===========================================================================

development_follow.go - scripted summon-follow fixture (development shard only)

===========================================================================
*/

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
	"opensro.online/server/internal/transport/worldsession"
)

type followFixturePhase string

const (
	fixtureLeader      followFixturePhase = "leader-created"
	fixtureSummoning   followFixturePhase = "summoning"
	fixtureFollowing   followFixturePhase = "following"
	fixtureStationary  followFixturePhase = "stationary"
	fixtureRetaliation followFixturePhase = "retaliation"
	fixtureLeaderLost  followFixturePhase = "leader-lost"
)

type followFixtureObservation struct {
	At     int64                                 `json:"at"`
	Actors []simulation.DevelopmentActorSnapshot `json:"actors"`
}
type liveFollowFixture struct {
	Phase          followFixturePhase         `json:"phase"`
	Leader         uint32                     `json:"leader"`
	Child          uint32                     `json:"child"`
	Skill          uint32                     `json:"skill"`
	Expires        int64                      `json:"expires"`
	Current        followFixtureObservation   `json:"current"`
	History        []followFixtureObservation `json:"-"`
	Decisions      []followFixtureObservation `json:"decisions"`
	division, name string
	session        *transport.Session
}

/*
==================
followFixtureControl

This fixture scripts stimuli, never a child's AI result. The existing Go
enum/transition owner is the dependency-boundary exception to XState. No
background ticker is created: expiration/observations use the mission tick.
==================
*/
type followFixtureControl struct {
	mu       sync.Mutex
	game     *gameplayPlane
	ops      *simulation.MonsterMoverOps
	bridge   *worldsession.Bridge
	active   map[string]*liveFollowFixture
	requests chan followFixtureRequest
}

type followFixtureReply struct {
	value any
	err   error
}
type followFixtureRequest struct {
	division, name, command string
	expires                 time.Time
	reply                   chan followFixtureReply
}

func (game *gameplayPlane) installFollowFixture(api *agentapi.API, ticker *simulation.Ticker) {
	if os.Getenv(agentapi.EnvBenchmarkFixtureControl) != "1" || ticker.Monsters == nil {
		return
	}
	c := &followFixtureControl{game: game, ops: ticker.Monsters, bridge: worldsession.New(game.hub), active: make(map[string]*liveFollowFixture), requests: make(chan followFixtureRequest, 8)}
	api.InstallFollowFixture(c.command)
	ticker.Hooks = append(ticker.Hooks, simulation.NamedTickHook{Name: "dev-follow-fixture", Hook: c.tick})
}

func (c *followFixtureControl) observe(f *liveFollowFixture, now int64) {
	all := c.ops.Monsters.DevelopmentFollowSnapshot(f.division, f.Leader, now)
	previous := f.Current
	f.Current = followFixtureObservation{At: now, Actors: all}
	if len(f.History) < 1800 { // hard bounded even if the clock/cleanup stalls
		selected := []simulation.DevelopmentActorSnapshot{}
		for _, a := range all {
			if a.GID == f.Leader || a.GID == f.Child {
				selected = append(selected, a)
			}
		}
		f.History = append(f.History, followFixtureObservation{At: now, Actors: selected})
		changed := false
		for _, actor := range selected {
			found := false
			for _, old := range previous.Actors {
				if old.GID == actor.GID && old.Serial == actor.Serial {
					found = true
					break
				}
			}
			if !found {
				changed = true
			}
		}
		if changed {
			f.Decisions = append(f.Decisions, followFixtureObservation{At: now, Actors: selected})
			if len(f.Decisions) > 128 {
				f.Decisions = f.Decisions[1:]
			}
		}
	}
}

func (c *followFixtureControl) cleanup(key string, f *liveFollowFixture) {
	c.ops.Monsters.DevelopmentRemoveFamily(f.division, f.Leader)
	c.game.items.DevelopmentObserverProtection(f.division, f.name, false)
	delete(c.active, key)
}

/*
==================
tick

Coordinator hook. It ignores the coordinator's tick time on purpose: commands
admitted below happen after that sample was taken, so observations are
stamped with the wall clock read after admission and never predate them.
==================
*/
func (c *followFixtureControl) tick(_ int64) []simulation.DivisionFrames {
	c.mu.Lock()
	defer c.mu.Unlock()
	// The coordinator invokes hooks after all division workers have joined.
	// Apply stimuli here so the next AI tick snapshots the new observer status
	// and retaliation together. HTTP must not arm against a tick's old viewer.
	for count := len(c.requests); count > 0; count-- {
		r := <-c.requests
		if time.Now().After(r.expires) {
			r.reply <- followFixtureReply{err: fmt.Errorf("fixture command expired before admission")}
			continue
		}
		value, err := c.commandLocked(r.division, r.name, r.command)
		var payload []byte
		if err == nil {
			payload, err = json.Marshal(value)
		}
		r.reply <- followFixtureReply{value: json.RawMessage(payload), err: err}
	}
	now := time.Now().UnixMilli()
	for key, f := range c.active {
		session, bound := c.game.hub.BoundSession(key)
		isolated := true
		for _, peer := range c.bridge.SnapshotSessions() {
			if peer.DivisionID == f.division && peer.SessionID != worldsession.SessionSceneID(f.session) {
				isolated = false
				break
			}
		}
		if !bound || session != f.session || now >= f.Expires || !isolated {
			c.cleanup(key, f)
			continue
		}
		// Only the scripted leader waits between commands. Never hold a child.
		m, ok := c.ops.Monsters.Mover(f.division, f.Leader)
		if ok && m.Mode() == monster.MoverIdle && m.BehaviorDeadlineMs < f.Expires {
			m.BehaviorDeadlineMs = f.Expires
			c.ops.Monsters.CommitMover(f.division, f.Leader, m)
		}
		c.observe(f, now)
	}
	return nil
}

func (c *followFixtureControl) command(division, name, command string) (any, error) {
	r := followFixtureRequest{division: division, name: name, command: command, expires: time.Now().Add(2 * time.Second), reply: make(chan followFixtureReply, 1)}
	select {
	case c.requests <- r:
	default:
		return nil, fmt.Errorf("fixture command queue full")
	}
	select {
	case reply := <-r.reply:
		return reply.value, reply.err
	case <-time.After(3 * time.Second):
		return nil, fmt.Errorf("fixture coordinator unavailable")
	}
}

func (c *followFixtureControl) commandLocked(division, name, command string) (any, error) {
	waveHealth := uint32(90)
	var buffRef uint32
	if strings.HasPrefix(command, "summon:") {
		n, err := strconv.ParseUint(strings.TrimPrefix(command, "summon:"), 10, 32)
		if err != nil || n == 0 || n > 90 {
			return nil, fmt.Errorf("invalid fixture health")
		}
		waveHealth = uint32(n)
		command = "summon"
	}
	if strings.HasPrefix(command, "buff:") {
		n, err := strconv.ParseUint(strings.TrimPrefix(command, "buff:"), 10, 32)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("invalid fixture child reference")
		}
		buffRef = uint32(n)
		command = "buff"
	}
	var requestedCodename string
	if strings.HasPrefix(command, "create:") {
		requestedCodename = strings.TrimPrefix(command, "create:")
		if requestedCodename == "" {
			return nil, fmt.Errorf("missing fixture unique codename")
		}
		command = "create"
	}
	key := division + ":" + strings.ToLower(name)
	f := c.active[key]
	now := time.Now().UnixMilli()
	if command == "cleanup" {
		if f != nil {
			c.observe(f, now)
			c.cleanup(key, f)
		}
		return map[string]any{"cleaned": true}, nil
	}
	session, bound := c.game.hub.BoundSession(key)
	if !bound {
		return nil, fmt.Errorf("fixture requires an authenticated in-world character")
	}
	v, ok := session.WorldSnapshot()
	if !ok {
		return nil, fmt.Errorf("world snapshot absent")
	}
	provider, ok := v.(worldsession.SnapshotProvider)
	if !ok {
		return nil, fmt.Errorf("world provider absent")
	}
	viewer := provider.WorldSnapshot()
	viewer.SessionID = worldsession.SessionSceneID(session)
	if viewer.World.Spawn.RegionID == 0 || command == "create" && !viewer.CombatEligible {
		return nil, fmt.Errorf("fixture creation requires a living world viewer")
	}
	if command == "restore" {
		character := c.game.deps.CharacterByName(division, name)
		result := c.game.items.DevelopmentRestoreViewer(division, character)
		if result.DiagnosticRefusal != "" {
			return nil, fmt.Errorf("%s", result.DiagnosticRefusal)
		}
		action.SendFrames(session, result.Frames)
		action.BroadcastObservedFrames(c.game.hub, division, session.ID, enterworld.ObjectIDForCharacter(character), result.Broadcast)
		return map[string]any{"restored": enterworld.CharacterAlive(character)}, nil
	}
	if command == "create" {
		if f != nil {
			return nil, fmt.Errorf("cleanup existing fixture before create")
		}
		for _, peer := range c.bridge.SnapshotSessions() {
			if peer.DivisionID == division && peer.SessionID != viewer.SessionID {
				return nil, fmt.Errorf("fixture requires an otherwise empty shard")
			}
		}
		ref, err := c.game.items.DevelopmentSummonReferenceByCodename(requestedCodename)
		if err != nil {
			return nil, err
		}
		if !c.game.items.DevelopmentObserverProtection(division, name, true) {
			return nil, fmt.Errorf("observer must start without a body status")
		}
		pose := monster.Pose{RegionID: viewer.World.Spawn.RegionID, X: viewer.World.Spawn.X - 160, Y: viewer.World.Spawn.Y, Z: viewer.World.Spawn.Z}
		parent, err := c.ops.Monsters.DevelopmentCreateLeader(division, ref.RefObjID, pose, now+150000)
		if err != nil {
			c.game.items.DevelopmentObserverProtection(division, name, false)
			return nil, err
		}
		f = &liveFollowFixture{Phase: fixtureLeader, Leader: parent.Gid, Expires: now + 150000, division: division, name: name, session: session}
		c.active[key] = f
		c.observe(f, now)
		return f, nil
	}
	if f == nil || f.session != session {
		return nil, fmt.Errorf("no fixture for this session generation")
	}
	c.observe(f, now)
	if command == "status" {
		return f, nil
	}
	if command == "capture" {
		return map[string]any{"state": f, "history": f.History}, nil
	}
	var frames []simulation.Frame
	switch command {
	case "summon":
		if f.Phase != fixtureLeader {
			return nil, fmt.Errorf("summon requires leader-created")
		}
		skill, err := c.game.items.DevelopmentStartSummonAtHealth(division, f.Leader, waveHealth)
		if err != nil {
			return nil, err
		}
		f.Skill = skill
		f.Phase = fixtureSummoning
	case "buff":
		if f.Phase != fixtureSummoning {
			return nil, fmt.Errorf("buff requires released authored wave")
		}
		var selected monster.Instance
		var threshold uint32
		for _, a := range f.Current.Actors {
			if a.Summoner != f.Leader || a.Reference != buffRef || a.HP == 0 {
				continue
			}
			i, ok := c.ops.Monsters.Get(division, a.GID)
			if !ok {
				continue
			}
			for _, condition := range i.Nest.ConditionalSkills {
				if condition.SkillID != 0 && condition.ConditionType == 0 && condition.Data > 0 && condition.Data < 100 {
					selected = i
					threshold = condition.Data
					break
				}
			}
			if selected.Gid != 0 {
				break
			}
		}
		if selected.Gid == 0 {
			return nil, fmt.Errorf("authored wave lacks requested conditional child")
		}
		remaining := uint32(uint64(selected.EffectiveMaxHP()) * uint64(threshold) / 100)
		if remaining > 0 {
			remaining--
		}
		if remaining == 0 || remaining >= selected.CurrentHP {
			return nil, fmt.Errorf("child threshold already crossed")
		}
		if !c.game.items.DevelopmentObserverProtection(division, name, false) {
			return nil, fmt.Errorf("observer protection release refused")
		}
		if _, ok := c.ops.Monsters.ApplyDamage(division, selected.Gid, selected.CurrentHP-remaining); !ok {
			return nil, fmt.Errorf("child stimulus refused")
		}
		if !c.ops.Monsters.ArmRetaliation(division, selected.Gid, simulation.PlayerObjectID(viewer.CharacterID)) {
			return nil, fmt.Errorf("child retaliation refused")
		}
		f.Child = selected.Gid
		f.Phase = fixtureRetaliation
	case "protect":
		if f.Phase != fixtureRetaliation {
			return nil, fmt.Errorf("protect requires retaliation")
		}
		if !c.game.items.DevelopmentObserverProtection(division, name, true) {
			return nil, fmt.Errorf("observer protection refused")
		}
	case "move-leader":
		if f.Phase != fixtureSummoning {
			return nil, fmt.Errorf("move-leader requires summoning")
		}
		best := math.Inf(1)
		var child simulation.DevelopmentActorSnapshot
		for _, a := range f.Current.Actors {
			if a.Summoner != f.Leader || a.HP == 0 || a.Mode != "idle" {
				continue
			}
			d := math.Hypot(a.Pose.X-viewer.World.Spawn.X, a.Pose.Z-viewer.World.Spawn.Z)
			if d < best {
				child = a
				best = d
			}
		}
		if child.GID == 0 {
			return nil, fmt.Errorf("authored wave has not released a child")
		}
		f.Child = child.GID
		goal := child.Pose
		goal.X += child.FollowRange + 140
		var err error
		frames, err = c.ops.DevelopmentMoveLeader(division, f.Leader, goal, now)
		if err != nil {
			return nil, err
		}
		f.Phase = fixtureFollowing
	case "refresh":
		if f.Phase != fixtureFollowing {
			return nil, fmt.Errorf("refresh requires following")
		}
		leader, _ := c.ops.Monsters.Mover(division, f.Leader)
		goal := leader.LivePoseAt(now, nil)
		goal.X += 120
		goal.Z += 50
		var err error
		frames, err = c.ops.DevelopmentMoveLeader(division, f.Leader, goal, now)
		if err != nil {
			return nil, err
		}
	case "stationary":
		if f.Phase != fixtureFollowing {
			return nil, fmt.Errorf("stationary requires following")
		}
		frames = c.ops.DevelopmentStopLeader(division, f.Leader, now, f.Expires)
		f.Phase = fixtureStationary
	case "retaliate":
		if f.Phase != fixtureStationary {
			return nil, fmt.Errorf("retaliate requires stationary")
		}
		c.game.items.DevelopmentObserverProtection(division, name, false)
		if _, ok := c.ops.Monsters.ApplyDamage(division, f.Child, 1); !ok {
			return nil, fmt.Errorf("child damage refused")
		}
		if !c.ops.Monsters.ArmRetaliation(division, f.Child, simulation.PlayerObjectID(viewer.CharacterID)) {
			return nil, fmt.Errorf("retaliation refused")
		}
		f.Phase = fixtureRetaliation
	case "leader-loss":
		if f.Phase != fixtureFollowing {
			return nil, fmt.Errorf("leader-loss requires following")
		}
		// Normal visibility publishes leader despawn; children remain live
		// so their production FOLLOW tick suspends steering (as the
		// server's follow tick does), while an accepted navigation command
		// can finish. Removal is not CSNM 12.
		c.ops.Monsters.DevelopmentRemoveLeader(division, f.Leader)
		f.Phase = fixtureLeaderLost
	default:
		return nil, fmt.Errorf("unknown fixture command")
	}
	if len(frames) > 0 {
		c.bridge.PushToDivision(division, frames, "")
	}
	c.observe(f, now)
	return f, nil
}
