/*
===========================================================================

party_summon.go - the party leader's summon into an instanced world

The v1.696 client's party summon window (interface 214; its title row
is kind 41 of the later client's dialog factory): the party leader,
standing inside an instanced world, opens a roster of the party. Each
row carries the member's eligibility as the native status cell renders
it - byte 2 Possible, byte 3 Impossible (StatusCellRender reads that
byte). The Summon button sends the native staged request and every
possible member gets the invitation box (UIIT_STT_PARTY_SUMMON_CONFIRM_BOX);
an accept moves the member into the leader's layer.

Wire: C->S 0x34C6, S->C 0xB4C6 - free in both ports' contracts (the
v1.150 client owns neither). Only the summon action's bytes {1, 0} are
native (the staged builder in the later client's click path); the
roster request {0, 0}, the answer {2, accept}, and the reply kinds are
the port's completion and are marked where they are built. The roster
reply keeps the native row shape {u32 id, u8 eligibility, u8, u16}
under a kind byte, and a refused request is {0, u16 code} through the
same item-use error router the later client ships (0x1c25-0x1c3a). The
reply header (allowed count, summoned count, limit seconds) has no
native counterpart - the later client derives those locally; ours
cannot - and is a port extension, recorded here.

Eligibility answers in the native refusal-text order
(UIIT_STT_PARTY_SUMMON_IMPOSSILBE_1.._9): outside of a town, players
allowed full, level requirement, job outfit, private store or trading,
on a transport, guild war, dead, Free PvP mode. The client routes
codes for _1.._4, _6, _7 and _8; _5 (store/trading) and _9 (Free PvP)
exist only as text, so a member whose first failure is one of those
reads Impossible and the summon answers the router's busy code
(0x1c0b) - a port choice, not a native mapping. The pending refusal
(UIIT_STT_PARTY_SUMMON_WAIT_ERR) answers while one of this leader's
requests is unanswered, and the 5 second button wait
(UIIT_STT_PARTY_SUMMON_BUTTON) is enforced for the leader. A member
already inside another programme, or holding another lane's prompt,
reads Impossible under the busy code (no native line exists for
either). Offline members get no row: the port holds no name for them.

Answers live 30 s, the shared transaction window the instance
invitation beside this lane reuses. All of it is session-scoped
runtime state, like the party registry itself: nothing persists.

===========================================================================
*/

package action

import (
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/temple"
)

// The summon wire (free in both contracts; see the file banner).
const (
	opPartySummonRequest uint16 = 0x34C6
	opPartySummonAnswer  uint16 = 0xB4C6
)

// The C->S actions. Only the summon action's bytes are native.
const (
	partySummonActionRoster uint8 = 0 // port-only: read the roster
	partySummonActionSummon uint8 = 1 // native staged request {1, 0}
	partySummonActionAnswer uint8 = 2 // port-only: answer a prompt
)

// The S->C reply kinds. A refusal keeps the native {0, u16 code} shape;
// the roster keeps the native row shape under kind 1; the prompt and
// the notice are the port's completion.
const (
	partySummonKindRefusal uint8 = 0
	partySummonKindRoster  uint8 = 1
	partySummonKindPrompt  uint8 = 2 // port-only
	partySummonKindNotice  uint8 = 3 // port-only: a sized UIIT key
)

// The row's eligibility bytes, as the native status cell renders them.
const (
	partySummonPossible   uint8 = 2
	partySummonImpossible uint8 = 3
)

// The refusal codes, through the later client's item-use error router:
// 0x1c37 outside of a town (_1), 0x1c25 players allowed full (_2),
// 0x1c2c level requirement (_3), 0x1c2e job outfit (_4), 0x1c30 on a
// transport (_6), 0x1c39 guild war (_7), 0x1c36 dead (_8), 0x1c3a the
// request is pending (WAIT_ERR). _5 and _9 have no routed code and
// answer the router's busy line (0x1c0b), the port's choice above.
const (
	partySummonCodeOutsideTown uint16 = 0x1c37
	partySummonCodeFull        uint16 = 0x1c25
	partySummonCodeLevel       uint16 = 0x1c2c
	partySummonCodeJobOutfit   uint16 = 0x1c2e
	partySummonCodeTransport   uint16 = 0x1c30
	partySummonCodeGuildWar    uint16 = 0x1c39
	partySummonCodeDead        uint16 = 0x1c36
	partySummonCodePending     uint16 = 0x1c3a
	partySummonCodeBusy        uint16 = 0x1c0b
)

// partySummonAnswerWindowMs is how long an invitation waits for its
// answer: the 30 s the shared transaction manager hands the instance
// invitation lane beside this one.
const partySummonAnswerWindowMs = 30_000

// partySummonCooldownMs is the native button wait: the leader must
// wait 5 seconds before summoning again (UIIT_STT_PARTY_SUMMON_BUTTON).
const partySummonCooldownMs = 5_000

/*
================
partySummon

One outstanding invitation: who leads, the layer the member would
enter, and where. The party and world parts are re-validated at
answer time - either may have changed while the box was up.
================
*/
type partySummon struct {
	leaderName string
	leaderGid  uint32
	lease      instance.Lease
	entry      simulation.Spawn
	expiresMs  int64
}

/*
================
partySummonBook

The outstanding invitations, by target, and each leader's last
summon stamp. Session-scoped runtime state: it legitimately dies on
process reboot (the party registry's verdict).
================
*/
type partySummonBook struct {
	mu        sync.Mutex
	byTarget  map[string]partySummon
	lastStamp map[string]int64
}

/*
================
partySummonKey

divisionID + ":" + lowercase(name) - the hub's bind-key shape, as the
instance invitation book keys its table.
================
*/
func partySummonKey(division, name string) string {
	return division + ":" + strings.ToLower(name)
}

/*
================
put
================
*/
func (b *partySummonBook) put(division, name string, summon partySummon) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byTarget == nil {
		b.byTarget = make(map[string]partySummon)
	}
	b.byTarget[partySummonKey(division, name)] = summon
}

/*
================
pending

Reports an invitation still inside its answer window.
================
*/
func (b *partySummonBook) pending(division, name string, nowMs int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	summon, ok := b.byTarget[partySummonKey(division, name)]
	return ok && nowMs <= summon.expiresMs
}

/*
================
pendingForLeader

Reports one of this leader's invitations still inside its window -
the native "summoning request is currently pending".
================
*/
func (b *partySummonBook) pendingForLeader(division, leaderName string, nowMs int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	prefix := division + ":"
	for key, summon := range b.byTarget {
		if strings.HasPrefix(key, prefix) && summon.leaderName == leaderName && nowMs <= summon.expiresMs {
			return true
		}
	}
	return false
}

/*
================
take

Consumes the target's invitation. An expired one is consumed too and
reports false: the answer came too late to act on.
================
*/
func (b *partySummonBook) take(division, name string, nowMs int64) (partySummon, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := partySummonKey(division, name)
	summon, ok := b.byTarget[key]
	if ok {
		delete(b.byTarget, key)
	}
	return summon, ok && nowMs <= summon.expiresMs
}

/*
================
drop

Forgets every invitation the named character leads or holds (their
session ended: a fresh client shows no prompt), and their summon
stamp. Reports whether an invitation dropped.
================
*/
func (b *partySummonBook) drop(division, name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	dropped := false
	for key, summon := range b.byTarget {
		if key == partySummonKey(division, name) || summon.leaderName == name {
			delete(b.byTarget, key)
			dropped = true
		}
	}
	if b.lastStamp != nil {
		delete(b.lastStamp, partySummonKey(division, name))
	}
	return dropped
}

/*
================
stamp
================
*/
func (b *partySummonBook) stamp(division, leaderName string, nowMs int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastStamp == nil {
		b.lastStamp = make(map[string]int64)
	}
	b.lastStamp[partySummonKey(division, leaderName)] = nowMs
}

/*
================
stampedAgoMs

How long ago the leader last summoned, and whether they ever did.
================
*/
func (b *partySummonBook) stampedAgoMs(division, leaderName string, nowMs int64) (int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	stamp, ok := b.lastStamp[partySummonKey(division, leaderName)]
	return nowMs - stamp, ok
}

/*
================
partySummonLayer

The layer the leader stands in, as the roster reads it: the lease,
its authored shape, who already resides inside, the level band a
temple world carries (if any), and the hard end (0 = none).
================
*/
type partySummonLayer struct {
	lease      instance.Lease
	definition instance.Definition
	residents  map[string]bool
	deadlineMs int64
	minLevel   int
	maxLevel   int
	hasBand    bool
}

/*
================
partySummonRow

One computed roster row: the member, their eligibility byte as the
native status cell renders it, whether they already stand inside the
leader's layer, and the first failing line's refusal code (0 when
the member may be summoned).
================
*/
type partySummonRow struct {
	member      *enterworld.Character
	gid         uint32
	eligibility uint8
	entered     bool
	code        uint16
}

/*
================
HandlePartySummon

0x34C6 {u8 action, u8 arg}: roster (0), summon (1), answer (2, arg is
the accept byte). The answer needs no party; the other two are the
leader's conversation.
================
*/
func (rt *Runtime) HandlePartySummon(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	action, _ := r.U8()
	arg, _ := r.U8()
	if r.Done() != nil || c == nil {
		return OpResult{}
	}
	if action == partySummonActionAnswer {
		return rt.partySummonAnswer(division, c, arg == 1)
	}
	if action != partySummonActionRoster && action != partySummonActionSummon {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	nowMs := rt.Now().UnixMilli()
	if !rt.partySummonLeads(division, c) {
		return partySummonNoticeResult("UIIT_MGS_ERR_PARTY_SUMMON_NOT")
	}
	layer, ok := rt.partySummonLeaderLayer(division, c)
	if !ok {
		// The leader leads no layer this window serves: the busy line
		// is the port's answer (no native text covers opening the
		// window outside an instanced world).
		return partySummonRefusalResult(partySummonCodeBusy)
	}
	if action == partySummonActionRoster {
		return partySummonResult(partySummonRosterBody(rt.partySummonLayerState(division, c, layer, nowMs)))
	}
	return rt.partySummon(division, c, layer, nowMs)
}

/*
================
partySummonLeads

Whether the character leads the party they belong to - the window's
first gate (UIIT_MGS_ERR_PARTY_SUMMON_NOT). The caller holds the
division lock.
================
*/
func (rt *Runtime) partySummonLeads(division string, c *enterworld.Character) bool {
	party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(c))
	return party != nil && party.LeaderID == enterworld.ObjectIDForCharacter(c)
}

/*
================
partySummonLeaderLayer

The instanced (type-1) layer the leader stands in. Anything else -
the default world, a permanent world, a layer the monster lane no
longer holds - is not a place this window summons into. The caller
holds the division lock.
================
*/
func (rt *Runtime) partySummonLeaderLayer(division string, leader *enterworld.Character) (partySummonLayer, bool) {
	if rt.Monsters == nil {
		return partySummonLayer{}, false
	}
	worldID := instance.ID(domain.CharacterWorldInstance(leader))
	lease, open := rt.Monsters.PopulationLease(division, worldID)
	if !open || lease.ID != worldID {
		return partySummonLayer{}, false
	}
	definition, ok := instance.Lookup(lease.ID.Definition())
	if !ok || definition.NativeType != 1 {
		return partySummonLayer{}, false
	}
	layer := partySummonLayer{lease: lease, definition: definition, residents: make(map[string]bool)}
	for _, name := range rt.instanceResidentNames(division, lease) {
		layer.residents[name] = true
	}
	if minLevel, maxLevel, banded := temple.Band(int(definition.ID)); banded {
		layer.minLevel, layer.maxLevel, layer.hasBand = minLevel, maxLevel, true
	}
	if tracked, found := rt.instanceWorlds.Load(instanceWorldKey{division: division, id: lease.ID}); found {
		layer.deadlineMs = tracked.(trackedInstanceWorld).deadlineMs
	}
	return layer, true
}

/*
================
partySummonLayerState

The roster for the leader's layer right now: the authored allowed
count (UIIT_STT_PARTY_SUMMON's 1-%d), how many already stand inside
(UIIT_STT_PARTY_SUMMON_PERSONNEL), the entry limit's remaining
seconds (UIIT_STT_PARTY_SUMMON_LIMIT_TIME), and one row per online
member. The caller holds the division lock.
================
*/
type partySummonLayerState struct {
	allowed      uint16
	summoned     int
	limitSeconds uint32
	remaining    uint16
	rows         []partySummonRow
}

func (rt *Runtime) partySummonLayerState(division string, leader *enterworld.Character, layer partySummonLayer, nowMs int64) partySummonLayerState {
	state := partySummonLayerState{allowed: layer.definition.PlayerLimit, summoned: len(layer.residents)}
	if layer.deadlineMs > nowMs {
		state.limitSeconds = uint32((layer.deadlineMs - nowMs) / 1000)
	}
	if limit := int(layer.definition.PlayerLimit); limit > 0 {
		if left := limit - state.summoned; left > 0 {
			state.remaining = uint16(left)
		}
	}
	party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(leader))
	if party == nil {
		return state
	}
	leaderGid := enterworld.ObjectIDForCharacter(leader)
	for _, gid := range party.Members {
		if gid == leaderGid {
			continue
		}
		member := rt.findCharacterByGid(division, gid)
		if member == nil {
			continue // offline: the port holds no name for them
		}
		row := partySummonRow{member: member, gid: gid, eligibility: partySummonPossible}
		if layer.residents[member.Name] {
			row.entered = true
		} else {
			row.eligibility, row.code = rt.partySummonGate(division, member, layer, nowMs)
		}
		state.rows = append(state.rows, row)
	}
	return state
}

/*
================
partySummonGate

One member's eligibility, in the native refusal-text order (_1.._9);
the byte is what the window's status cell renders and the code is the
first failing line's wire answer. Preconditions with no native line
(delete-pending, already inside a programme, another lane's prompt)
read Impossible under the busy code. The caller holds the division
lock.
================
*/
func (rt *Runtime) partySummonGate(division string, member *enterworld.Character, layer partySummonLayer, nowMs int64) (uint8, uint16) {
	if member.DeletePending {
		return partySummonImpossible, partySummonCodeBusy
	}
	if member.World != nil && member.World.InstanceReturn != nil {
		return partySummonImpossible, partySummonCodeBusy // inside a programme
	}
	if !rt.inTown(division, member) {
		return partySummonImpossible, partySummonCodeOutsideTown // _1
	}
	if limit := int(layer.definition.PlayerLimit); limit > 0 && len(layer.residents) >= limit {
		return partySummonImpossible, partySummonCodeFull // _2
	}
	if layer.hasBand {
		level := int64(1)
		if member.Level != nil {
			level = *member.Level
		}
		if level < int64(layer.minLevel) || level > int64(layer.maxLevel) {
			return partySummonImpossible, partySummonCodeLevel // _3
		}
	}
	if enterworld.DressedJob(member) != 0 {
		return partySummonImpossible, partySummonCodeJobOutfit // _4
	}
	if rt.Stalls != nil && rt.Stalls.Keeping(division, member.Name) ||
		rt.Exchanges != nil && (rt.Exchanges.Trading(division, member.Name) || rt.Exchanges.Pending(division, member.Name, nowMs)) {
		return partySummonImpossible, partySummonCodeBusy // _5: no routed code
	}
	if rt.transportSummoned(member) {
		return partySummonImpossible, partySummonCodeTransport // _6
	}
	if member.GuildID != nil && len(rt.GuildWars.Wars(division, *member.GuildID)) > 0 {
		return partySummonImpossible, partySummonCodeGuildWar // _7
	}
	if !enterworld.CharacterAlive(member) {
		return partySummonImpossible, partySummonCodeDead // _8
	}
	if rt.inFreeBattle(member) {
		return partySummonImpossible, partySummonCodeBusy // _9: no routed code
	}
	if rt.partySummons.pending(division, member.Name, nowMs) {
		return partySummonImpossible, partySummonCodePending
	}
	if rt.instanceInvites.pending(division, member.Name, nowMs) ||
		rt.ProposalPending != nil && rt.ProposalPending(division, member.Name) {
		return partySummonImpossible, partySummonCodeBusy // another lane's prompt is up
	}
	return partySummonPossible, 0
}

/*
================
partySummon

The Summon button: prompts every possible member who is not already
inside, in party order. The answer is the first impossible member's
refusal line when any member could not be asked (the router shows
it), the refreshed roster otherwise. A pending request of this
leader's (WAIT_ERR) and the 5 second button wait refuse before
anything is asked. The caller holds the division lock.
================
*/
func (rt *Runtime) partySummon(division string, leader *enterworld.Character, layer partySummonLayer, nowMs int64) OpResult {
	if rt.partySummons.pendingForLeader(division, leader.Name, nowMs) {
		return partySummonRefusalResult(partySummonCodePending)
	}
	if ago, stamped := rt.partySummons.stampedAgoMs(division, leader.Name, nowMs); stamped && ago < partySummonCooldownMs {
		return partySummonNoticeResult("UIIT_STT_PARTY_SUMMON_BUTTON")
	}
	state := rt.partySummonLayerState(division, leader, layer, nowMs)
	entry := rt.liveSpawn(simulation.WorldKey(division, leader.Name), leader, nowMs)
	leaderGid := enterworld.ObjectIDForCharacter(leader)
	firstRefusal := uint16(0)
	for _, row := range state.rows {
		if row.entered {
			continue
		}
		if row.code != 0 {
			if firstRefusal == 0 {
				firstRefusal = row.code
			}
			continue
		}
		rt.partySummons.put(division, row.member.Name, partySummon{
			leaderName: leader.Name,
			leaderGid:  leaderGid,
			lease:      layer.lease,
			entry:      entry,
			expiresMs:  nowMs + partySummonAnswerWindowMs,
		})
		if rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(division, row.member.Name, []wire.Frame{{
				Opcode:  opPartySummonAnswer,
				Payload: partySummonPromptBody(leaderGid, leader.Name),
			}})
		}
	}
	rt.partySummons.stamp(division, leader.Name, nowMs)
	if firstRefusal != 0 {
		return partySummonRefusalResult(firstRefusal)
	}
	return partySummonResult(partySummonRosterBody(state))
}

/*
================
partySummonAnswer

A member's answer: consumes the invitation either way. An accept
re-checks the party and world parts of eligibility (the box has
been up for as long as the window) and transfers the member into
the leader's layer under the division lock; the transfer's own
gates still run inside. The leader's window refreshes after every
answer.
================
*/
func (rt *Runtime) partySummonAnswer(division string, c *enterworld.Character, accepted bool) OpResult {
	summon, ok := rt.partySummons.take(division, c.Name, rt.Now().UnixMilli())
	if !ok {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if accepted {
		if member := rt.findCharacter(division, c.Name); member != nil &&
			rt.partySummonAcceptEligible(division, member, summon) {
			rt.transferCharacterToInstance(division, member, summon.lease, summon.entry)
		}
	}
	if leader := rt.findCharacter(division, summon.leaderName); leader != nil {
		if frame, live := rt.partySummonRosterFrame(division, leader); live {
			if rt.PushCharacterFrames != nil {
				rt.PushCharacterFrames(division, leader.Name, []wire.Frame{frame})
			}
		}
	}
	return OpResult{}
}

/*
================
partySummonAcceptEligible

The answer-time re-check: the member still belongs to the inviting
leader's party, the leader still leads it and still stands in the
layer, and the member has not entered another programme meanwhile.
The caller holds the division lock.
================
*/
func (rt *Runtime) partySummonAcceptEligible(division string, member *enterworld.Character, summon partySummon) bool {
	if member.World != nil && member.World.InstanceReturn != nil {
		return false // inside another programme now
	}
	leader := rt.findCharacter(division, summon.leaderName)
	if leader == nil || instance.ID(domain.CharacterWorldInstance(leader)) != summon.lease.ID {
		return false // the leader left the layer
	}
	party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(member))
	return party != nil && party.LeaderID == summon.leaderGid
}

/*
================
partySummonRosterFrame

The leader's refreshed roster frame, or false when the leader no
longer qualifies (their window keeps its last roster). The caller
holds the division lock.
================
*/
func (rt *Runtime) partySummonRosterFrame(division string, leader *enterworld.Character) (wire.Frame, bool) {
	if !rt.partySummonLeads(division, leader) {
		return wire.Frame{}, false
	}
	layer, ok := rt.partySummonLeaderLayer(division, leader)
	if !ok {
		return wire.Frame{}, false
	}
	state := rt.partySummonLayerState(division, leader, layer, rt.Now().UnixMilli())
	return wire.Frame{Opcode: opPartySummonAnswer, Payload: partySummonRosterBody(state)}, true
}

/*
================
DropPartySummon

Forgets the invitations the character leads or holds (their session
ended or they re-entered the world: a fresh client shows no prompt).
Wiring calls it from the session hooks, like the other lanes' drops.
================
*/
func (rt *Runtime) DropPartySummon(division, name string) bool {
	return rt.partySummons.drop(division, name)
}

//============================================================================
// THE WIRE
//============================================================================

/*
================
partySummonRosterBody

The roster reply: kind 1, the port header (allowed, summoned, limit
seconds), then the native-shaped rows {u32 gid, u8 eligibility,
u8 entered, u16 entries remaining}.
================
*/
func partySummonRosterBody(state partySummonLayerState) []byte {
	w := wire.NewWriter(9 + 8*len(state.rows)).
		U8(partySummonKindRoster).U16(state.allowed).U8(uint8(state.summoned)).
		U32(state.limitSeconds).U8(uint8(len(state.rows)))
	for _, row := range state.rows {
		entered := uint8(0)
		if row.entered {
			entered = 1
		}
		w.U32(row.gid).U8(row.eligibility).U8(entered).U16(state.remaining)
	}
	return w.Payload()
}

/*
================
partySummonPromptBody

The invitation box frame: kind 2, the leader's gid, then their sized
name - the box line is UIIT_STT_PARTY_SUMMON_CONFIRM_BOX with the
leader's name. Port-only (the native prompt wire is unread).
================
*/
func partySummonPromptBody(leaderGid uint32, leaderName string) []byte {
	return wire.NewWriter(7 + len(leaderName)).
		U8(partySummonKindPrompt).U32(leaderGid).
		U16(uint16(len(leaderName))).Bytes([]byte(leaderName)).Payload()
}

/*
================
partySummonResult
================
*/
func partySummonResult(body []byte) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opPartySummonAnswer, Payload: body}}}
}

/*
================
partySummonRefusalResult

The refused conversation, native-shaped: {0, u16 code} through the
item-use error router.
================
*/
func partySummonRefusalResult(code uint16) OpResult {
	return partySummonResult(wire.NewWriter(3).U8(partySummonKindRefusal).U16(code).Payload())
}

/*
================
partySummonNoticeResult

A leader-side line the router has no code for, as a sized UIIT key
(port-only kind 3): the leader-only line and the button wait.
================
*/
func partySummonNoticeResult(key string) OpResult {
	return partySummonResult(wire.NewWriter(3 + len(key)).
		U8(partySummonKindNotice).U16(uint16(len(key))).Bytes([]byte(key)).Payload())
}
