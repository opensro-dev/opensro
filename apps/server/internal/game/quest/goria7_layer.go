/*
===========================================================================

goria7_layer.go - GORIA 7: the hunt layer's clock

The layer half of the GORIA 7 programme (goria7.go owns the
hunter's talk and the proof item that re-opens this layer). The
lancer appears 30,000 ms after the layer opens, speaks, and the
battle runs: warnings with ten and five minutes left, the lancer
gone at twenty minutes, the layer's own end ten seconds later,
and the party returned on the kill with its proof-item credit.

===========================================================================
*/

package quest

import (
	"sort"
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
goria7Layer

One open hunt, keyed by its world. BattleMs is when the battle
opened (after the lancer's line), zero until then.
================
*/
type goria7Layer struct {
	division  string
	summoner  string
	residents map[string]bool
	lease     instance.Lease
	openedMs  int64
	spawnedMs int64
	lines     int
	battleMs  int64
	warnings  int
	bossGone  bool
	killed    bool
	credited  bool
	graceEnd  int64
	ended     bool
	// bossSite is where the boss fell (NoteKillSite), the gate's spot.
	bossSite      simulation.Spawn
	bossSiteKnown bool
}

/*
================
goria7Programme

The programme's runtime state: one layer per open world, the
leases the invitations returned, and which leader already holds
a hunt. Process-local, like the leases themselves.
================
*/
type goria7Programme struct {
	mu       sync.Mutex
	layers   map[instance.ID]*goria7Layer
	leases   map[instance.ID]instance.Lease
	summoner map[string]instance.ID
}

/*
================
RegisterGoria7Programme

Subscribes the GORIA 7 programme to its instance definition's
lifecycle. Death goes unregistered: the kill reaches the
programme through the quest kill updater, which knows the
monster and the killer's world.
================
*/
func (rt *Runtime) RegisterGoria7Programme() {
	rt.RegisterInstanceHandler(goria7Definition, InstanceHandler{
		Setup:        rt.goria7Setup,
		Enter:        rt.goria7Enter,
		Leave:        rt.goria7Leave,
		Update:       rt.goria7Update,
		Activated:    rt.goria7Activated,
		PartyChanged: rt.goria7PartyChanged,
	})
}

/*
================
Goria7MemberEligible

The invitation predicate the native entry states: members doing
this quest, or having finished it, may come.
================
*/
func (rt *Runtime) Goria7MemberEligible(c *enterworld.Character) bool {
	def, ok := rt.Defs.ByCodename(goria7QuestCodename)
	if !ok || c == nil {
		return false
	}
	return activeQuestIndex(c, def.RefID) >= 0 || questCompleted(c, def.RefID)
}

/*
================
summonedBy

Whether this leader already holds an open hunt.
================
*/
func (p *goria7Programme) summonedBy(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, open := p.summoner[name]
	return open
}

/*
================
rememberLease

Keeps the invitation's lease and its summoner under the world
the layer opened as. The setup event may already have been
delivered; the layer adopts the lease at its next update.
================
*/
func (p *goria7Programme) rememberLease(lease instance.Lease, summoner string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.leases == nil {
		p.leases = make(map[instance.ID]instance.Lease)
	}
	if p.summoner == nil {
		p.summoner = make(map[string]instance.ID)
	}
	p.leases[lease.ID] = lease
	p.summoner[summoner] = lease.ID
}

/*
================
layerFor

The layer state for one world, created on first sight (setup, or
an enter that raced it).
================
*/
func (rt *Runtime) goria7LayerFor(event InstanceEvent) *goria7Layer {
	p := &rt.goria7
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.layers == nil {
		p.layers = make(map[instance.ID]*goria7Layer)
	}
	layer := p.layers[event.World]
	if layer == nil {
		layer = &goria7Layer{
			division:  event.Division,
			residents: make(map[string]bool),
			openedMs:  rt.goria7Now(),
		}
		p.layers[event.World] = layer
	}
	return layer
}

/*
================
forgetLayer

Drops a finished layer and everything that pointed at it.
================
*/
func (rt *Runtime) goria7ForgetLayer(world instance.ID) {
	p := &rt.goria7
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, id := range p.summoner {
		if id == world {
			delete(p.summoner, name)
		}
	}
	delete(p.layers, world)
	delete(p.leases, world)
}

/*
================
goria7Setup

A layer opened: start its encounter clock.
================
*/
func (rt *Runtime) goria7Setup(event InstanceEvent) {
	rt.goria7LayerFor(event)
}

/*
================
goria7Enter

A resident arrived. The first through the door is the summoner
(the invitation moves the leader in before any member answers).
================
*/
func (rt *Runtime) goria7Enter(event InstanceEvent) {
	layer := rt.goria7LayerFor(event)
	rt.goria7.mu.Lock()
	layer.residents[event.Name] = true
	if layer.summoner == "" {
		layer.summoner = event.Name
	}
	rt.goria7.mu.Unlock()
}

/*
================
goria7Leave

A resident returned. When the last one leaves a hunt that is not
already ending, the room closes behind them instead of idling to
its deadline.
================
*/
func (rt *Runtime) goria7Leave(event InstanceEvent) {
	rt.goria7.mu.Lock()
	layer := rt.goria7.layers[event.World]
	rt.goria7.mu.Unlock()
	if layer == nil {
		return
	}
	rt.goria7.mu.Lock()
	delete(layer.residents, event.Name)
	empty := len(layer.residents) == 0
	ended := layer.ended
	lease := layer.lease
	if lease.ID == 0 {
		lease = rt.goria7.leases[event.World]
	}
	rt.goria7.mu.Unlock()
	if !empty || ended {
		return
	}
	rt.goria7ForgetLayer(event.World)
	rt.removeExitGates(event.World)
	if rt.InstanceClose != nil && lease.ID != 0 {
		rt.InstanceClose(lease)
	}
}

/*
================
goria7Activated

The post-entry checkpoint: a resident who is neither doing nor
has finished this quest does not belong to the hunt - they hear
the instance-condition line and are returned.
================
*/
func (rt *Runtime) goria7Activated(event InstanceEvent) {
	character := rt.goria7Character(event.Division, event.Name)
	if character == nil || rt.Goria7MemberEligible(character) {
		return
	}
	rt.goria7PushOne(event.Name, goria7SymCondition)
	if rt.InstanceReturn != nil {
		rt.InstanceReturn(character)
	}
}

/*
================
goria7PartyChanged

A resident's party standing broke mid-hunt (they left, or a
leadership change named them): the native guard answers with
the instance-condition line and sends them out.
================
*/
func (rt *Runtime) goria7PartyChanged(event InstanceEvent) {
	rt.goria7.mu.Lock()
	layer := rt.goria7.layers[event.World]
	inside := layer != nil && layer.residents[event.Name]
	rt.goria7.mu.Unlock()
	if !inside {
		return
	}
	character := rt.goria7Character(event.Division, event.Name)
	if character == nil {
		return
	}
	rt.goria7PushOne(event.Name, goria7SymCondition)
	if rt.InstanceReturn != nil {
		rt.InstanceReturn(character)
	}
}

/*
================
goria7Character

One division character by name, from the character source.
================
*/
func (rt *Runtime) goria7Character(division, name string) *enterworld.Character {
	for _, candidate := range rt.deps.CharactersForDivision(division) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return candidate
		}
	}
	return nil
}

/*
================
goria7PushOne

One quest-notification frame to one character, when the push
door is wired.
================
*/
func (rt *Runtime) goria7PushOne(name, symbol string) {
	if rt.PushCharacterFrames == nil || name == "" {
		return
	}
	rt.PushCharacterFrames(name, []wire.Frame{questNotification(symbol)})
}

/*
================
goria7Push

One notice to every resident, in name order so a test (and a
log) reads the same sequence every time.
================
*/
func (rt *Runtime) goria7Push(layer *goria7Layer, symbol string) {
	for _, name := range goria7ResidentNames(layer) {
		rt.goria7PushOne(name, symbol)
	}
}

/*
================
goria7ResidentNames

A layer's residents, sorted.
================
*/
func goria7ResidentNames(layer *goria7Layer) []string {
	names := make([]string, 0, len(layer.residents))
	for name := range layer.residents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

/*
================
noteGoria7Kill

The lancer fell: mark its layer. Only records; the credit runs
at the layer's next update, lock-free.
================
*/
func (rt *Runtime) noteGoria7Kill(c *enterworld.Character, codename string) {
	if codename != goria7BossCodename || c == nil || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != goria7Definition {
		return
	}
	rt.goria7.mu.Lock()
	if layer := rt.goria7.layers[world]; layer != nil && !layer.bossGone {
		layer.killed = true
	}
	rt.goria7.mu.Unlock()
}

/*
================
goria7Update

The encounter clock. One pass per layer tick; elapsed time
comes from the runtime clock, never from counting ticks. The
lancer is named 30,000 ms in and speaks 2,000 ms later; the
battle clock runs from 5,000 ms after the line, warnings land
with ten and five minutes left, the lancer leaves at twenty
minutes, and the layer's own end returns the room; a kill
credits the party and returns it at once.
================
*/
func (rt *Runtime) goria7Update(event InstanceEvent) {
	layer := rt.goria7LayerFor(event)
	rt.goria7.mu.Lock()
	if layer.lease.ID == 0 {
		layer.lease = rt.goria7.leases[event.World]
	}
	killed := layer.killed && !layer.credited
	if killed {
		layer.credited = true
	}
	ended := layer.ended
	rt.goria7.mu.Unlock()
	if ended {
		return
	}
	now := rt.goria7Now()
	if killed {
		rt.goria7FinishKill(event, layer, now)
		return
	}
	if layer.killed {
		if now >= layer.graceEnd {
			rt.goria7EndLayer(event, layer)
		}
		return
	}
	if layer.spawnedMs == 0 {
		if now-layer.openedMs < goria7SpawnDelayMs {
			return
		}
		// Anchored at the phase's exact end, not at the tick
		// that noticed it, like every clock below.
		layer.spawnedMs = layer.openedMs + goria7SpawnDelayMs
		rt.goria7SpawnLancer(event.Division, layer)
		return
	}
	if layer.battleMs == 0 {
		lineAt := layer.spawnedMs + goria7FirstLineMs
		if now < lineAt {
			return
		}
		layer.battleMs = lineAt + goria7BattleStepMs
		rt.goria7Push(layer, goria7SymBossLine)
		return
	}
	if layer.lines == 0 {
		if now < layer.battleMs {
			return
		}
		layer.lines = 1
		rt.goria7Push(layer, goria7SymBattleBegun)
	}
	switch elapsed := now - layer.battleMs; {
	case layer.bossGone:
		if elapsed >= goria7LayerEndMs {
			rt.goria7EndLayer(event, layer)
		}
	case layer.warnings == 0 && elapsed >= goria7WarnFirstMs:
		layer.warnings = 1
		rt.goria7Push(layer, goria7SymWarnFirst)
	case layer.warnings == 1 && elapsed >= goria7WarnSecondMs:
		layer.warnings = 2
		rt.goria7Push(layer, goria7SymWarnSecond)
	case elapsed >= goria7BossLeavesMs:
		layer.bossGone = true
		rt.goria7Push(layer, goria7SymBossLeaves)
	}
}

/*
================
goria7SpawnLancer

Places the Hun Lancer 20 to 100 from the entry point in the
layer's population, inside the summoner's character door, the
quest-spawn contract. The native spawn anchors at the instance
teleport point; the port anchors at the layer's entry point,
the point the party landed on.
================
*/
func (rt *Runtime) goria7SpawnLancer(division string, layer *goria7Layer) {
	if rt.SpawnQuestMonster == nil {
		return
	}
	host := layer.summoner
	if !layer.residents[host] {
		names := goria7ResidentNames(layer)
		if len(names) == 0 {
			return
		}
		host = names[0]
	}
	character := rt.goria7Character(division, host)
	if character == nil {
		return
	}
	request := simulation.QuestMonsterSpawn{
		Division:      division,
		Population:    layer.lease,
		Codename:      goria7BossCodename,
		Position:      goria7EntrySpawn,
		FixedPosition: true,
		RadiusMin:     goria7SpawnRadiusMin,
		RadiusSpan:    goria7SpawnRadiusSpan,
		LifetimeMs:    goria7BossLifeMs,
		NowMs:         rt.goria7Now(),
	}
	rt.deps.Update(character, "goria7-spawn", func() bool {
		return rt.SpawnQuestMonster(character, request)
	})
}

/*
================
goria7FinishKill

The lancer is down. Its death line goes to the room; every
resident doing the quest is credited with the proof item they
lack. The exit gate rises beside the lancer and the room's
sixty-second grace starts: the reference death handler moves
nobody (its member loop only sets state and the quest timer)
and sets the event timer to 0xEA60, whose expiry returns the
party.
================
*/
func (rt *Runtime) goria7FinishKill(event InstanceEvent, layer *goria7Layer, now int64) {
	rt.goria7Push(layer, goria7SymBossDeath)
	layer.graceEnd = now + goria7KillGraceMs
	rt.goria7.mu.Lock()
	site, known := layer.bossSite, layer.bossSiteKnown
	rt.goria7.mu.Unlock()
	rt.raiseExitGate(event.World, site, known)
	def, ok := rt.Defs.ByCodename(goria7QuestCodename)
	if ok {
		for _, name := range goria7ResidentNames(layer) {
			rt.goria7CreditKill(event.Division, name, def)
		}
	}
}

/*
================
goria7CreditKill

One resident's kill credit: the lancer's fall counts as the
resident's own kill and the proof item lands for a resident
doing the quest who lacks it; the journal follows. A resident
not doing the quest (a finished helper) is untouched.
================
*/
func (rt *Runtime) goria7CreditKill(division, name string, def *Definition) {
	character := rt.goria7Character(division, name)
	if character == nil {
		return
	}
	var frames []wire.Frame
	rt.deps.Update(character, "goria7-kill-credit", func() bool {
		if character.DeletePending {
			return false
		}
		at := activeQuestIndex(character, def.RefID)
		if at < 0 {
			return false
		}
		if rt.PlanInventory != nil && captureItemCount(character, goria7ItemCodename) == 0 {
			rows, updates, err := rt.PlanInventory(character, nil, []inventory.ItemAmount{{Codename: goria7ItemCodename, Count: 1}})
			if err == nil {
				character.MissionInventory = rows
				frames = append(frames, updates...)
			}
		}
		updated, changed := refreshMissions(character, def, character.ActiveQuests[at], goria7BossCodename, 0)
		if changed {
			frames = append(frames, missionProgressFrames(def, character.ActiveQuests[at], updated)...)
			character.ActiveQuests[at] = updated
		}
		return len(frames) > 0
	})
	if len(frames) > 0 && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(name, frames)
	}
}

/*
================
goria7EndLayer

Returns every resident to their anchor and closes the layer;
the close evacuates anyone the returns missed, so nobody is
stranded.
================
*/
func (rt *Runtime) goria7EndLayer(event InstanceEvent, layer *goria7Layer) {
	rt.goria7.mu.Lock()
	if layer.ended {
		rt.goria7.mu.Unlock()
		return
	}
	layer.ended = true
	lease := layer.lease
	rt.goria7.mu.Unlock()
	for _, name := range goria7ResidentNames(layer) {
		if character := rt.goria7Character(event.Division, name); character != nil && rt.InstanceReturn != nil {
			rt.InstanceReturn(character)
		}
	}
	rt.goria7ForgetLayer(event.World)
	rt.removeExitGates(event.World)
	if rt.InstanceClose != nil && lease.ID != 0 {
		rt.InstanceClose(lease)
	}
}
