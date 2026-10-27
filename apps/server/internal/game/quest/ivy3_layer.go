/*
===========================================================================

ivy3_layer.go - IVY 3: the fortress summon's layer clock

The layer half of the IVY 3 programme (ivy3.go owns Ratchel's talk,
the Horn grants, and the assembled key that opens this layer). Ivy
appears 30,000 ms after the layer opens, trades her three lines, and
the battle runs the EASTEU 21 clock: warnings with ten and five
minutes left, Ivy gone at twenty minutes, the layer's own end ten
seconds later, and a sixty-second grace after the party's kill.

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
ivy3Layer

One open Ivy summon, keyed by its world. BattleMs is when the battle
opened (after the three lines), zero until then; the kill leaves a
grace deadline instead of closing at once.
================
*/
type ivy3Layer struct {
	division    string
	summoner    string
	residents   map[string]bool
	lease       instance.Lease
	openedMs    int64
	spawnedMs   int64
	lines       int
	battleMs    int64
	battleBegun bool
	warnings    int
	bossGone    bool
	killed      bool
	credited    bool
	graceEnd    int64
	ended       bool
	// bossSite is where Ivy fell (NoteKillSite); the exit gate stands
	// beside it.
	bossSite      simulation.Spawn
	bossSiteKnown bool
}

/*
================
ivy3Programme

The programme's runtime state: one layer per open world, the leases
the invitations returned, which leader already holds a summon, and
the sentry leaders the Horns have called, by codename. Process-local,
like the leases themselves.
================
*/
type ivy3Programme struct {
	mu       sync.Mutex
	layers   map[instance.ID]*ivy3Layer
	leases   map[instance.ID]instance.Lease
	summoner map[string]instance.ID
	sentries map[string][]ivy3Sentry
}

/*
================
RegisterIvy3Programme

Subscribes the IVY 3 programme to its instance definition's
lifecycle (framework step 5). Death goes unregistered: the kill
reaches the programme through the quest kill updater, which knows
the monster and the killer's world.
================
*/
func (rt *Runtime) RegisterIvy3Programme() {
	rt.RegisterInstanceHandler(ivy3Definition, InstanceHandler{
		Setup:        rt.ivy3Setup,
		Enter:        rt.ivy3Enter,
		Leave:        rt.ivy3Leave,
		Update:       rt.ivy3Update,
		Activated:    rt.ivy3Activated,
		PartyChanged: rt.ivy3PartyChanged,
	})
}

/*
================
Ivy3MemberEligible

The invitation predicate the native entry states: members doing this
quest, having finished it, or holding a key may come.
================
*/
func (rt *Runtime) Ivy3MemberEligible(c *enterworld.Character) bool {
	def, ok := rt.Defs.ByCodename(ivy3QuestCodename)
	if !ok || c == nil {
		return false
	}
	return activeQuestIndex(c, def.RefID) >= 0 || questCompleted(c, def.RefID) ||
		captureItemCount(c, ivy3KeyCodename) > 0
}

/*
================
summonedBy

Whether this leader already holds an open summon.
================
*/
func (p *ivy3Programme) summonedBy(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, open := p.summoner[name]
	return open
}

/*
================
rememberLease

Keeps the invitation's lease and its summoner under the world the
layer opened as. The setup event may already have been delivered;
the layer adopts the lease at its next update.
================
*/
func (p *ivy3Programme) rememberLease(lease instance.Lease, summoner string) {
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

The layer state for one world, created on first sight (setup, or an
enter that raced it).
================
*/
func (rt *Runtime) ivy3LayerFor(event InstanceEvent) *ivy3Layer {
	p := &rt.ivy3
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.layers == nil {
		p.layers = make(map[instance.ID]*ivy3Layer)
	}
	layer := p.layers[event.World]
	if layer == nil {
		layer = &ivy3Layer{
			division:  event.Division,
			residents: make(map[string]bool),
			openedMs:  rt.ivy3Now(),
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
func (rt *Runtime) ivy3ForgetLayer(world instance.ID) {
	p := &rt.ivy3
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
ivy3Setup

A layer opened: start its encounter clock.
================
*/
func (rt *Runtime) ivy3Setup(event InstanceEvent) {
	rt.ivy3LayerFor(event)
}

/*
================
ivy3Enter

A resident arrived. The first through the door is the summoner (the
invitation moves the leader in before any member answers).
================
*/
func (rt *Runtime) ivy3Enter(event InstanceEvent) {
	layer := rt.ivy3LayerFor(event)
	rt.ivy3.mu.Lock()
	layer.residents[event.Name] = true
	if layer.summoner == "" {
		layer.summoner = event.Name
	}
	rt.ivy3.mu.Unlock()
}

/*
================
ivy3Leave

A resident returned. When the last one leaves a summon that is not
already ending, the room closes behind them instead of idling to
its deadline.
================
*/
func (rt *Runtime) ivy3Leave(event InstanceEvent) {
	rt.ivy3.mu.Lock()
	layer := rt.ivy3.layers[event.World]
	rt.ivy3.mu.Unlock()
	if layer == nil {
		return
	}
	rt.ivy3.mu.Lock()
	delete(layer.residents, event.Name)
	empty := len(layer.residents) == 0
	ended := layer.ended
	lease := layer.lease
	if lease.ID == 0 {
		lease = rt.ivy3.leases[event.World]
	}
	rt.ivy3.mu.Unlock()
	if !empty || ended {
		return
	}
	rt.ivy3ForgetLayer(event.World)
	if rt.RemoveExitGates != nil {
		rt.RemoveExitGates(event.World)
	}
	if rt.InstanceClose != nil && lease.ID != 0 {
		rt.InstanceClose(lease)
	}
}

/*
================
ivy3Activated

The post-entry checkpoint: a resident who is neither doing nor has
finished this quest, and holds no key, does not belong to the
summon - they hear the instance-condition line and are returned.
================
*/
func (rt *Runtime) ivy3Activated(event InstanceEvent) {
	character := rt.ivy3Character(event.Division, event.Name)
	if character == nil || rt.Ivy3MemberEligible(character) {
		return
	}
	rt.ivy3PushOne(event.Name, ivy3SymCondition)
	if rt.InstanceReturn != nil {
		rt.InstanceReturn(character)
	}
}

/*
================
ivy3PartyChanged

A resident's party standing broke mid-summon (they left, or a
leadership change named them): the native handler answers with the
instance-condition line and sends them out.
================
*/
func (rt *Runtime) ivy3PartyChanged(event InstanceEvent) {
	rt.ivy3.mu.Lock()
	layer := rt.ivy3.layers[event.World]
	inside := layer != nil && layer.residents[event.Name]
	rt.ivy3.mu.Unlock()
	if !inside {
		return
	}
	character := rt.ivy3Character(event.Division, event.Name)
	if character == nil {
		return
	}
	rt.ivy3PushOne(event.Name, ivy3SymCondition)
	if rt.InstanceReturn != nil {
		rt.InstanceReturn(character)
	}
}

/*
================
ivy3Character

One division character by name, from the character source.
================
*/
func (rt *Runtime) ivy3Character(division, name string) *enterworld.Character {
	for _, candidate := range rt.deps.CharactersForDivision(division) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return candidate
		}
	}
	return nil
}

/*
================
ivy3PushOne

One quest-notification frame to one character, when the push door
is wired.
================
*/
func (rt *Runtime) ivy3PushOne(name, symbol string) {
	if rt.PushCharacterFrames == nil || name == "" {
		return
	}
	rt.PushCharacterFrames(name, []wire.Frame{questNotification(symbol)})
}

/*
================
ivy3Push

One notice to every resident, in name order so a test (and a log)
reads the same sequence every time.
================
*/
func (rt *Runtime) ivy3Push(layer *ivy3Layer, symbol string) {
	for _, name := range ivy3ResidentNames(layer) {
		rt.ivy3PushOne(name, symbol)
	}
}

/*
================
ivy3ResidentNames

A layer's residents, sorted.
================
*/
func ivy3ResidentNames(layer *ivy3Layer) []string {
	names := make([]string, 0, len(layer.residents))
	for name := range layer.residents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

/*
================
noteIvy3BossKill

Ivy fell: mark her layer. Only records; the credit runs at the
layer's next update, lock-free.
================
*/
func (rt *Runtime) noteIvy3BossKill(c *enterworld.Character) {
	if c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != ivy3Definition {
		return
	}
	rt.ivy3.mu.Lock()
	if layer := rt.ivy3.layers[world]; layer != nil && !layer.bossGone {
		layer.killed = true
	}
	rt.ivy3.mu.Unlock()
}

/*
================
noteIvy3BossSite

Where Ivy fell in her layer (NoteKillSite, just before the kill note):
the exit gate rises beside it. Records only.
================
*/
func (rt *Runtime) noteIvy3BossSite(c *enterworld.Character, codename string, fell simulation.Spawn) {
	if codename != ivy3BossCodename || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != ivy3Definition {
		return
	}
	rt.ivy3.mu.Lock()
	if layer := rt.ivy3.layers[world]; layer != nil && !layer.bossGone && !layer.bossSiteKnown {
		layer.bossSite, layer.bossSiteKnown = fell, true
	}
	rt.ivy3.mu.Unlock()
}

/*
================
ivy3Update

The encounter clock. One pass per layer tick; elapsed time comes
from the runtime clock, never from counting ticks. Ivy is named at
the summon and trades her three lines before the battle opens; the
battle clock runs from the opening, warnings land with ten and five
minutes left, Ivy leaves at twenty minutes, and the layer's own end
returns the room; a kill instead leaves a sixty-second grace after
its credit.
================
*/
func (rt *Runtime) ivy3Update(event InstanceEvent) {
	layer := rt.ivy3LayerFor(event)
	rt.ivy3.mu.Lock()
	if layer.lease.ID == 0 {
		layer.lease = rt.ivy3.leases[event.World]
	}
	killed := layer.killed && !layer.credited
	if killed {
		layer.credited = true
	}
	ended := layer.ended
	rt.ivy3.mu.Unlock()
	if ended {
		return
	}
	now := rt.ivy3Now()
	if killed {
		rt.ivy3FinishKill(event, layer, now)
		return
	}
	if layer.killed {
		if now >= layer.graceEnd {
			rt.ivy3EndLayer(event, layer)
		}
		return
	}
	if layer.spawnedMs == 0 {
		if now-layer.openedMs < ivy3SpawnDelayMs {
			return
		}
		// Anchored at the phase's exact end, not at the tick that
		// noticed it, like every clock below.
		layer.spawnedMs = layer.openedMs + ivy3SpawnDelayMs
		rt.ivy3SpawnIvy(event.Division, layer)
		return
	}
	if !layer.battleBegun {
		// The three lines, five seconds apart from the first: the
		// battle clock is anchored at the battle's opening (the
		// last line plus its 5,000 ms), not at a line itself and
		// not at the tick that noticed it.
		lineAt := layer.spawnedMs + ivy3FirstLineMs + int64(layer.lines)*ivy3LineStepMs
		if now < lineAt {
			return
		}
		switch layer.lines {
		case 0:
			layer.lines = 1
			rt.ivy3Push(layer, ivy3SymBossThreat)
		case 1:
			layer.lines = 2
			rt.ivy3Push(layer, ivy3SymPlayerPlea)
		case 2:
			layer.lines = 3
			layer.battleMs = lineAt + ivy3LineStepMs
			rt.ivy3Push(layer, ivy3SymBossRefusal)
		default:
			if now >= layer.battleMs {
				layer.battleBegun = true
				rt.ivy3Push(layer, ivy3SymBattleBegun)
			}
		}
		return
	}
	switch elapsed := now - layer.battleMs; {
	case layer.bossGone:
		if elapsed >= ivy3LayerEndMs {
			rt.ivy3EndLayer(event, layer)
		}
	case layer.warnings == 0 && elapsed >= ivy3BattleMs:
		layer.warnings = 1
		rt.ivy3Push(layer, ivy3SymWarn10)
	case layer.warnings == 1 && elapsed >= ivy3BattleMs+ivy3WarningStepMs:
		layer.warnings = 2
		rt.ivy3Push(layer, ivy3SymWarn5)
	case elapsed >= ivy3BossLeavesMs:
		layer.bossGone = true
		rt.ivy3Push(layer, ivy3SymBossLeaves)
	}
}

/*
================
ivy3SpawnIvy

Places Ivy 20 to 100 from a resident (the summoner when they are
still inside) in the layer's population, inside that resident's
character door, the quest-spawn contract.
================
*/
func (rt *Runtime) ivy3SpawnIvy(division string, layer *ivy3Layer) {
	if rt.SpawnQuestMonster == nil {
		return
	}
	host := layer.summoner
	if !layer.residents[host] {
		names := ivy3ResidentNames(layer)
		if len(names) == 0 {
			return
		}
		host = names[0]
	}
	character := rt.ivy3Character(division, host)
	if character == nil {
		return
	}
	request := simulation.QuestMonsterSpawn{
		Division:   division,
		Population: layer.lease,
		Codename:   ivy3BossCodename,
		RadiusMin:  ivy3SpawnRadiusMin,
		RadiusSpan: ivy3SpawnRadiusSpan,
		NowMs:      rt.ivy3Now(),
	}
	rt.deps.Update(character, "ivy3-spawn", func() bool {
		return rt.SpawnQuestMonster(character, request)
	})
}

/*
================
ivy3FinishKill

Ivy is down. Her death line goes to the room; then every resident
doing the quest runs the kill credit - Horns deleted, the Red Hair
only for a resident holding the assembled key, whose keys are then
spent - the whole party, the dead included, exactly the native
kill evaluator - and the room's sixty-second grace starts.
================
*/
func (rt *Runtime) ivy3FinishKill(event InstanceEvent, layer *ivy3Layer, now int64) {
	rt.ivy3Push(layer, ivy3SymBossDeath)
	layer.graceEnd = now + ivy3KillGraceMs
	// The reference handler raises the exit gate beside Ivy, then sets the
	// sixty-second event timer: the gate lets the room leave early, the
	// timer still returns whoever stays.
	rt.ivy3.mu.Lock()
	site, known := layer.bossSite, layer.bossSiteKnown
	rt.ivy3.mu.Unlock()
	if known && rt.SpawnExitGate != nil {
		rt.SpawnExitGate(event.World, site)
	}
	def, ok := rt.Defs.ByCodename(ivy3QuestCodename)
	if ok {
		for _, name := range ivy3ResidentNames(layer) {
			rt.ivy3CreditKill(event.Division, name, def)
		}
	}
}

/*
================
ivy3CreditKill

One resident's kill credit, in the native evaluator's order: every
Horn Trumpet they hold is deleted first; then the assembled key is
counted - none held, nothing more (no Red Hair, no achievement);
held, every key is spent and the Red Hair lands, atomically, and
the journal follows the bag. A resident not doing the quest (a
finished helper, or a key holder without it) is untouched.
================
*/
func (rt *Runtime) ivy3CreditKill(division, name string, def *Definition) {
	character := rt.ivy3Character(division, name)
	if character == nil {
		return
	}
	var frames []wire.Frame
	rt.deps.Update(character, "ivy3-kill-credit", func() bool {
		if character.DeletePending {
			return false
		}
		at := activeQuestIndex(character, def.RefID)
		if at < 0 {
			return false
		}
		credited := false
		if rt.PlanInventory != nil {
			var take []inventory.ItemAmount
			if horns := captureItemCount(character, ivy3HornCodename); horns > 0 {
				take = append(take, inventory.ItemAmount{Codename: ivy3HornCodename, Count: horns})
			}
			var give []inventory.ItemAmount
			if held := captureItemCount(character, ivy3KeyCodename); held > 0 {
				credited = true
				take = append(take, inventory.ItemAmount{Codename: ivy3KeyCodename, Count: held})
				if captureItemCount(character, ivy3HairCodename) == 0 {
					give = append(give, inventory.ItemAmount{Codename: ivy3HairCodename, Count: 1})
				}
			}
			if len(take) > 0 || len(give) > 0 {
				rows, updates, err := rt.PlanInventory(character, take, give)
				if err == nil {
					character.MissionInventory = rows
					frames = append(frames, updates...)
				}
			}
		}
		// The Ivy hunt ticks only for a credited resident: the
		// keyless leave with their Horns gone and no achievement,
		// as the evaluator's key count leaves them.
		killed := ""
		if credited {
			killed = ivy3BossCodename
		}
		updated, changed := refreshMissions(character, def, character.ActiveQuests[at], killed, 0)
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
ivy3EndLayer

Returns every resident to their anchor and closes the layer; the
close evacuates anyone the returns missed, so nobody is stranded.
================
*/
func (rt *Runtime) ivy3EndLayer(event InstanceEvent, layer *ivy3Layer) {
	rt.ivy3.mu.Lock()
	if layer.ended {
		rt.ivy3.mu.Unlock()
		return
	}
	layer.ended = true
	lease := layer.lease
	rt.ivy3.mu.Unlock()
	for _, name := range ivy3ResidentNames(layer) {
		if character := rt.ivy3Character(event.Division, name); character != nil && rt.InstanceReturn != nil {
			rt.InstanceReturn(character)
		}
	}
	rt.ivy3ForgetLayer(event.World)
	if rt.RemoveExitGates != nil {
		rt.RemoveExitGates(event.World)
	}
	if rt.InstanceClose != nil && lease.ID != 0 {
		rt.InstanceClose(lease)
	}
}
