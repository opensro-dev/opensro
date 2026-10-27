/*
===========================================================================

instance_invite_test.go - the party instance invitation lane

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
instanceInviteFixture

A leader and three members admitted in the default world, in one
shared party, with the prompt frames and instance events captured.
================
*/
type instanceInviteFixture struct {
	rt      *Runtime
	clock   *fakeClock
	leader  *enterworld.Character
	members []*enterworld.Character
	prompts map[string][]wire.Frame
	events  []InstanceEvent
}

func newInstanceInviteFixture(t *testing.T, options uint8) *instanceInviteFixture {
	t.Helper()
	leader := instanceWorldTestCharacter(1, "Leader")
	members := []*enterworld.Character{
		instanceWorldTestCharacter(2, "MemberA"),
		instanceWorldTestCharacter(3, "MemberB"),
		instanceWorldTestCharacter(4, "MemberC"),
	}
	all := append([]*enterworld.Character{leader}, members...)
	rt, clock := newInstanceWorldRuntime(t, all...)
	f := &instanceInviteFixture{
		rt: rt, clock: clock, leader: leader, members: members,
		prompts: make(map[string][]wire.Frame),
	}
	for i, c := range all {
		admitInstanceWorldCharacter(t, rt, c, uint64(100+i), instanceWorldTestStart)
	}
	rt.PushCharacterFrames = func(_ string, name string, frames []wire.Frame) {
		f.prompts[name] = append(f.prompts[name], frames...)
	}
	rt.InstanceEventSink = func(event InstanceEvent) { f.events = append(f.events, event) }
	gids := make([]uint32, 0, len(all))
	for _, c := range all {
		gids = append(gids, enterworld.ObjectIDForCharacter(c))
	}
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{
			Order: 1, Options: options,
			LeaderID: enterworld.ObjectIDForCharacter(leader),
			Members:  gids,
		}}
	}
	return f
}

func (f *instanceInviteFixture) invite(t *testing.T) InstanceInviteResult {
	t.Helper()
	return f.rt.InvitePartyToInstance(InstanceInviteRequest{
		Division:   testDivision,
		LeaderName: f.leader.Name,
		Definition: instanceWorldTestPartyDefinition,
		Entry:      instanceWorldTestEntry,
	})
}

func (f *instanceInviteFixture) inside(c *enterworld.Character) bool {
	return c.World != nil && c.World.InstanceReturn != nil
}

func (f *instanceInviteFixture) kinds() []InstanceEventKind {
	kinds := make([]InstanceEventKind, 0, len(f.events))
	for _, event := range f.events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

/*
================
TestInstanceInvitePromptsThePartyAndMovesAccepts
================
*/
func TestInstanceInvitePromptsThePartyAndMovesAccepts(t *testing.T) {
	f := newInstanceInviteFixture(t, instanceInviteShareOptions)
	result := f.invite(t)
	if result.OpenStatus != instance.Success || result.LeaderStatus != instance.Success {
		t.Fatalf("invite = %+v, want opened and leader moved", result)
	}
	if !f.inside(f.leader) {
		t.Fatal("leader not inside after initiation")
	}
	if len(result.Prompted) != 3 || len(result.Skipped) != 0 {
		t.Fatalf("prompted %v skipped %v, want all three members prompted", result.Prompted, result.Skipped)
	}
	// The type-7 prompt: type, inviter gid, zeroed struct, member gids.
	prompt := f.prompts["MemberA"]
	if len(prompt) != 1 || prompt[0].Opcode != opInvitationProposal {
		t.Fatalf("MemberA frames = %v, want one 0x3393 prompt", prompt)
	}
	payload := prompt[0].Payload
	leaderGid := enterworld.ObjectIDForCharacter(f.leader)
	// The exact composition, pinned to the native literals rather than to
	// the constants (a typo in instanceInvitePromptType must fail here):
	// type 7 (client 0x7644E0's arm), the inviter gid, the 19 zero bytes of the
	// client struct QuestInstanceInvite_Read (0x764380) reads past, the count, then
	// the whole party's gids.
	header := []byte{7, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(header[1:5], leaderGid)
	header = append(header, make([]byte, 19)...)
	header = append(header, 4, 0)
	if len(payload) != 42 || !bytes.Equal(payload[:26], header) {
		t.Fatalf("prompt payload = %x, want header %x then 4 party gids", payload, header)
	}
	roster := map[uint32]bool{}
	for i := 26; i < 42; i += 4 {
		roster[binary.LittleEndian.Uint32(payload[i:i+4])] = true
	}
	for _, c := range append([]*enterworld.Character{f.leader}, f.members...) {
		if !roster[enterworld.ObjectIDForCharacter(c)] {
			t.Fatalf("prompt roster %x lacks %s", payload[26:], c.Name)
		}
	}
	if got, want := f.kinds(), []InstanceEventKind{InstanceEventSetup, InstanceEventEnter, InstanceEventActivated}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("events = %v, want setup, leader enter, leader activated", got)
	}

	consent := f.rt.InstanceInviteConsent()
	consent.ApplyConsent(nil, testDivision, f.members[0], 1, 1)
	if !f.inside(f.members[0]) {
		t.Fatal("MemberA not inside after accepting")
	}
	if consent.HasPendingInvite(testDivision, "MemberA") {
		t.Fatal("MemberA still holds an invitation after answering")
	}
	consent.ApplyConsent(nil, testDivision, f.members[1], 1, 2)
	if f.inside(f.members[1]) {
		t.Fatal("MemberB inside after declining")
	}
	got := f.kinds()
	want := []InstanceEventKind{
		InstanceEventSetup, InstanceEventEnter, InstanceEventActivated,
		InstanceEventInvitationAnswered, InstanceEventEnter, InstanceEventActivated,
		InstanceEventInvitationAnswered,
	}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	if !f.events[3].Accepted || f.events[6].Accepted {
		t.Fatalf("answered events lost their answers: %+v", f.events)
	}
}

/*
================
TestInstanceActivatedFollowsEnter

Every character who transfers in is activated right after their
enter, naming the same character: the programme's post-entry
checkpoint (#1434).
================
*/
func TestInstanceActivatedFollowsEnter(t *testing.T) {
	f := newInstanceInviteFixture(t, instanceInviteShareOptions)
	if result := f.invite(t); result.OpenStatus != instance.Success {
		t.Fatalf("invite = %+v", result)
	}
	f.rt.InstanceInviteConsent().ApplyConsent(nil, testDivision, f.members[0], 1, 1)

	// The activation names the character whose enter it follows.
	leaderGid := enterworld.ObjectIDForCharacter(f.leader)
	memberGid := enterworld.ObjectIDForCharacter(f.members[0])
	enterLeader, activatedLeader := f.events[1], f.events[2]
	if enterLeader.Kind != InstanceEventEnter || enterLeader.Gid != leaderGid {
		t.Fatalf("leader enter = %+v", enterLeader)
	}
	if activatedLeader.Kind != InstanceEventActivated || activatedLeader.Gid != leaderGid || activatedLeader.World != enterLeader.World {
		t.Fatalf("leader activated = %+v, want the leader in %v", activatedLeader, enterLeader.World)
	}
	enterMember, activatedMember := f.events[4], f.events[5]
	if enterMember.Kind != InstanceEventEnter || enterMember.Gid != memberGid {
		t.Fatalf("member enter = %+v", enterMember)
	}
	if activatedMember.Kind != InstanceEventActivated || activatedMember.Gid != memberGid || activatedMember.World != enterMember.World {
		t.Fatalf("member activated = %+v, want MemberA in %v", activatedMember, enterMember.World)
	}
}

/*
================
TestInstanceAcceptRechecksPartyAndWorld

The invitation was earned by standing in the leader's party, in the
leader's world, at prompt time. Thirty seconds is long enough to
leave both; an accept re-checks them before the transfer (#1435).
================
*/
func TestInstanceAcceptRechecksPartyAndWorld(t *testing.T) {
	// MemberA leaves the party while the prompt is up.
	f := newInstanceInviteFixture(t, instanceInviteShareOptions)
	if result := f.invite(t); result.OpenStatus != instance.Success {
		t.Fatalf("invite = %+v", result)
	}
	leaderGid := enterworld.ObjectIDForCharacter(f.leader)
	rest := []uint32{leaderGid}
	for _, c := range f.members[1:] {
		rest = append(rest, enterworld.ObjectIDForCharacter(c))
	}
	f.rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{
			Order: 1, Options: instanceInviteShareOptions,
			LeaderID: leaderGid, Members: rest,
		}}
	}
	f.rt.InstanceInviteConsent().ApplyConsent(nil, testDivision, f.members[0], 1, 1)
	if f.inside(f.members[0]) {
		t.Fatal("MemberA entered after leaving the party")
	}

	// MemberA steps into another world while the prompt is up.
	f = newInstanceInviteFixture(t, instanceInviteShareOptions)
	if result := f.invite(t); result.OpenStatus != instance.Success {
		t.Fatalf("invite = %+v", result)
	}
	otherWorld := domain.DefaultWorldInstance + 1
	f.members[0].World.PackedInstance = &otherWorld
	f.rt.InstanceInviteConsent().ApplyConsent(nil, testDivision, f.members[0], 1, 1)
	if f.inside(f.members[0]) {
		t.Fatal("MemberA entered from another world")
	}
}

/*
================
TestInstanceInviteRefusesWhatCannotStart
================
*/
func TestInstanceInviteRefusesWhatCannotStart(t *testing.T) {
	// An unshared party never opens the layer.
	f := newInstanceInviteFixture(t, 0)
	if result := f.invite(t); result.OpenStatus == instance.Success {
		t.Fatalf("unshared party opened: %+v", result)
	}
	if len(f.prompts) != 0 {
		t.Fatal("prompts sent for a refused initiation")
	}

	// Only the leader initiates.
	f = newInstanceInviteFixture(t, instanceInviteShareOptions)
	result := f.rt.InvitePartyToInstance(InstanceInviteRequest{
		Division: testDivision, LeaderName: "MemberA",
		Definition: instanceWorldTestPartyDefinition, Entry: instanceWorldTestEntry,
	})
	if result.OpenStatus == instance.Success {
		t.Fatalf("a member initiated: %+v", result)
	}
}

/*
================
TestInstanceInviteSkipsBusyAndIneligibleMembers
================
*/
func TestInstanceInviteSkipsBusyAndIneligibleMembers(t *testing.T) {
	f := newInstanceInviteFixture(t, instanceInviteShareOptions)
	f.rt.ProposalPending = func(_ string, name string) bool { return name == "MemberB" }
	result := f.rt.InvitePartyToInstance(InstanceInviteRequest{
		Division: testDivision, LeaderName: f.leader.Name,
		Definition: instanceWorldTestPartyDefinition, Entry: instanceWorldTestEntry,
		Eligible: func(c *enterworld.Character) bool { return c.Name != "MemberC" },
	})
	if len(result.Prompted) != 1 || result.Prompted[0] != "MemberA" {
		t.Fatalf("prompted = %v, want only MemberA", result.Prompted)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("skipped = %v, want MemberB and MemberC", result.Skipped)
	}
	if len(f.prompts["MemberB"]) != 0 || len(f.prompts["MemberC"]) != 0 {
		t.Fatal("a skipped member was prompted")
	}
}

/*
================
TestInstancePartyFactsAnswerTheTalkGates

The facts the quest talk gates read: a leader in a shared party, a plain
member, an unknown name (not found), and an unshared party's leader.
================
*/
func TestInstancePartyFactsAnswerTheTalkGates(t *testing.T) {
	f := newInstanceInviteFixture(t, instanceInviteShareOptions)
	leader := f.rt.InstancePartyFacts(testDivision, "Leader")
	if !leader.Found || !leader.InParty || !leader.Leader || !leader.Shared || leader.Transported {
		t.Fatalf("leader facts %+v", leader)
	}
	member := f.rt.InstancePartyFacts(testDivision, "MemberA")
	if !member.Found || !member.InParty || member.Leader || !member.Shared {
		t.Fatalf("member facts %+v", member)
	}
	if got := f.rt.InstancePartyFacts(testDivision, "Nobody"); got.Found {
		t.Fatalf("an unknown name answered found: %+v", got)
	}
	f = newInstanceInviteFixture(t, 0)
	if facts := f.rt.InstancePartyFacts(testDivision, "Leader"); !facts.InParty || !facts.Leader || facts.Shared {
		t.Fatalf("unshared leader facts %+v", facts)
	}
}

/*
================
TestInstanceInviteTimeoutClosesTheEmptyLayer
================
*/
func TestInstanceInviteTimeoutClosesTheEmptyLayer(t *testing.T) {
	f := newInstanceInviteFixture(t, instanceInviteShareOptions)
	result := f.invite(t)
	if result.OpenStatus != instance.Success {
		t.Fatalf("invite = %+v", result)
	}
	// The leader leaves; nobody else came. Once the answers lapse the
	// layer has no reason to exist.
	if status := f.rt.ReturnCharacterFromInstance(testDivision, f.leader); status != instance.Success {
		t.Fatalf("leader return = %v", status)
	}
	f.clock.Advance(31 * time.Second)
	f.rt.tickInstanceWorlds(f.clock.now.UnixMilli())
	if _, open := f.rt.Monsters.PopulationLease(testDivision, result.Lease.ID); open {
		t.Fatal("layer still open after the answers lapsed on an empty instance")
	}
	// A late accept finds nothing outstanding and moves nobody.
	f.rt.InstanceInviteConsent().ApplyConsent(nil, testDivision, f.members[0], 1, 1)
	if f.inside(f.members[0]) {
		t.Fatal("MemberA moved on an expired invitation")
	}
}

/*
================
TestInstanceDeathAndPartyChangeReachTheRegistry
================
*/
func TestInstanceDeathAndPartyChangeReachTheRegistry(t *testing.T) {
	f := newInstanceInviteFixture(t, instanceInviteShareOptions)
	result := f.invite(t)
	f.rt.InstanceInviteConsent().ApplyConsent(nil, testDivision, f.members[0], 1, 1)
	before := len(f.events)

	f.rt.recordInstanceDeath(testDivision, f.members[0])
	f.rt.NotePartyChanged(testDivision, "MemberA")
	// A death outside any programme is not an instance event.
	f.rt.recordInstanceDeath(testDivision, f.members[1])

	got := f.kinds()[before:]
	if len(got) != 2 || got[0] != InstanceEventDead || got[1] != InstanceEventPartyChanged {
		t.Fatalf("events = %v, want dead then party-changed for the resident only", got)
	}
	if status := f.rt.ReturnCharacterFromInstance(testDivision, f.members[0]); status != instance.Success {
		t.Fatalf("member return = %v", status)
	}
	if last := f.events[len(f.events)-1]; last.Kind != InstanceEventLeave {
		t.Fatalf("last event = %v, want the member's leave", last.Kind)
	}
	_ = result
}
