/*
===========================================================================

party_summon_test.go - the party summon lane (0x34C6 / 0xB4C6)

The v1.696 client's party summon window (interface 214): the leader
stands inside an instanced world, reads a roster whose rows mark each
party member Possible (2) or Impossible (3) in the native refusal-text
order, summons the possible members into the layer, and each member
answers the invitation box. These tests pin the wire shapes and the
order of the eligibility ladder.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/temple"
)

// partySummonTownPose stands a character in Jangan: region 0x62A8 is a
// combat-denied (_RefRegion.IsBattleField) town region, so inTown holds.
// partySummonFieldPose is the battlefield beside it (0x62AA, combat
// allowed): standing there fails the summon's first eligibility line.
var (
	partySummonTownPose  = simulation.ChinaStartProfile()
	partySummonFieldPose = simulation.Spawn{RegionID: 0x62AA, X: 100, Y: 20, Z: 100}
)

/*
================
partySummonFixture

A leader and three members in one party. The members stand in town in
the default world; the leader is admitted, moved into an opened
instance layer, and every frame the lane pushes to a character is
captured.
================
*/
type partySummonFixture struct {
	rt      *Runtime
	clock   *fakeClock
	leader  *enterworld.Character
	members []*enterworld.Character
	pushed  map[string][]wire.Frame
	lease   instance.Lease
}

func newPartySummonFixture(t *testing.T, definition instance.DefinitionID) *partySummonFixture {
	t.Helper()
	leader := instanceWorldTestCharacter(1, "Leader")
	members := []*enterworld.Character{
		instanceWorldTestCharacter(2, "MemberA"),
		instanceWorldTestCharacter(3, "MemberB"),
		instanceWorldTestCharacter(4, "MemberC"),
	}
	all := append([]*enterworld.Character{leader}, members...)
	rt, clock := newInstanceWorldRuntime(t, all...)
	f := &partySummonFixture{
		rt: rt, clock: clock, leader: leader, members: members,
		pushed: make(map[string][]wire.Frame),
	}
	for i, c := range all {
		admitInstanceWorldCharacter(t, rt, c, uint64(200+i), partySummonTownPose)
	}
	rt.PushCharacterFrames = func(_ string, name string, frames []wire.Frame) {
		f.pushed[name] = append(f.pushed[name], frames...)
	}
	gids := make([]uint32, 0, len(all))
	for _, c := range all {
		gids = append(gids, enterworld.ObjectIDForCharacter(c))
	}
	// One offline gid rides the roster too: no row may name it.
	gids = append(gids, 0x0BADF00D)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{
			Order:    1,
			LeaderID: enterworld.ObjectIDForCharacter(leader),
			Members:  gids,
		}}
	}
	lease, status := rt.OpenInstanceWorld(InstanceOpenRequest{Division: testDivision, Definition: definition})
	if status != instance.Success {
		t.Fatalf("open %d: status %d", definition, status)
	}
	f.lease = lease
	if status := rt.TransferCharacterToInstance(testDivision, leader, lease, instanceWorldTestEntry); status != instance.Success {
		t.Fatalf("leader transfer: status %d", status)
	}
	// The transfer's own frames are the world's business; the capture
	// starts empty so each test counts only the summon lane's pushes.
	f.pushed = make(map[string][]wire.Frame)
	return f
}

func (f *partySummonFixture) roster(t *testing.T) []byte {
	t.Helper()
	result := f.rt.HandlePartySummon(testDivision, f.leader, []byte{partySummonActionRoster, 0})
	return singleSummonAnswer(t, result)
}

func (f *partySummonFixture) summon(t *testing.T) []byte {
	t.Helper()
	result := f.rt.HandlePartySummon(testDivision, f.leader, []byte{partySummonActionSummon, 0})
	return singleSummonAnswer(t, result)
}

func (f *partySummonFixture) inside(c *enterworld.Character) bool {
	return c.World != nil && c.World.InstanceReturn != nil
}

/*
================
singleSummonAnswer
================
*/
func singleSummonAnswer(t *testing.T, result OpResult) []byte {
	t.Helper()
	if len(result.Frames) != 1 || result.Frames[0].Opcode != opPartySummonAnswer {
		t.Fatalf("frames = %v, want one 0xB4C6 answer", result.Frames)
	}
	return result.Frames[0].Payload
}

/*
================
rosterRow
================
*/
func rosterRows(t *testing.T, payload []byte) (allowed uint16, summoned uint8, limitSeconds uint32, rows map[uint32][3]uint16) {
	t.Helper()
	if len(payload) < 9 || payload[0] != partySummonKindRoster {
		t.Fatalf("payload = %x, want a roster answer", payload)
	}
	allowed = binary.LittleEndian.Uint16(payload[1:3])
	summoned = payload[3]
	limitSeconds = binary.LittleEndian.Uint32(payload[4:8])
	count := int(payload[8])
	if len(payload) != 9+8*count {
		t.Fatalf("payload = %x, want %d rows", payload, count)
	}
	rows = make(map[uint32][3]uint16)
	for i := 0; i < count; i++ {
		row := payload[9+8*i:]
		gid := binary.LittleEndian.Uint32(row[0:4])
		rows[gid] = [3]uint16{uint16(row[4]), uint16(row[5]), binary.LittleEndian.Uint16(row[6:8])}
	}
	return allowed, summoned, limitSeconds, rows
}

/*
================
TestPartySummonRosterMarksPossibleMembers
================
*/
func TestPartySummonRosterMarksPossibleMembers(t *testing.T) {
	f := newPartySummonFixture(t, instanceWorldTestPartyDefinition)
	payload := f.roster(t)
	allowed, summoned, _, rows := rosterRows(t, payload)
	if allowed != 8 || summoned != 1 {
		t.Fatalf("allowed %d summoned %d, want 8 and 1 (the leader)", allowed, summoned)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %v, want the three online members only", rows)
	}
	for _, member := range f.members {
		row, ok := rows[enterworld.ObjectIDForCharacter(member)]
		if !ok {
			t.Fatalf("no row for %s", member.Name)
		}
		if row[0] != uint16(partySummonPossible) || row[1] != 0 || row[2] != 7 {
			t.Fatalf("%s row = %v, want possible, not entered, 7 entries left", member.Name, row)
		}
	}
}

/*
================
TestPartySummonRefusesNonLeader
================
*/
func TestPartySummonRefusesNonLeader(t *testing.T) {
	f := newPartySummonFixture(t, instanceWorldTestPartyDefinition)
	result := f.rt.HandlePartySummon(testDivision, f.members[0], []byte{partySummonActionRoster, 0})
	payload := singleSummonAnswer(t, result)
	if len(payload) < 3 || payload[0] != partySummonKindNotice ||
		string(payload[3:]) != "UIIT_MGS_ERR_PARTY_SUMMON_NOT" {
		t.Fatalf("payload = %x, want the leader-only notice", payload)
	}
	if int(binary.LittleEndian.Uint16(payload[1:3])) != len("UIIT_MGS_ERR_PARTY_SUMMON_NOT") {
		t.Fatalf("payload = %x, want a sized notice key", payload)
	}
}

/*
================
TestPartySummonLeaderOutsideInstance
================
*/
func TestPartySummonLeaderOutsideInstance(t *testing.T) {
	leader := instanceWorldTestCharacter(1, "Leader")
	member := instanceWorldTestCharacter(2, "MemberA")
	rt, _ := newInstanceWorldRuntime(t, leader, member)
	admitInstanceWorldCharacter(t, rt, leader, 300, partySummonTownPose)
	admitInstanceWorldCharacter(t, rt, member, 301, partySummonTownPose)
	leaderGid := enterworld.ObjectIDForCharacter(leader)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{
			Order:    1,
			LeaderID: leaderGid,
			Members:  []uint32{leaderGid, enterworld.ObjectIDForCharacter(member)},
		}}
	}
	payload := singleSummonAnswer(t, rt.HandlePartySummon(testDivision, leader, []byte{partySummonActionRoster, 0}))
	if len(payload) != 3 || payload[0] != partySummonKindRefusal ||
		binary.LittleEndian.Uint16(payload[1:3]) != partySummonCodeBusy {
		t.Fatalf("payload = %x, want the busy refusal", payload)
	}
}

/*
================
TestPartySummonEligibilityLadderOrder

The ladder answers in the native text order (_1.._9): a member who is
both out of town and dead reads Impossible for _1, and its wire code
at summon time is _1's, not _8's.
================
*/
func TestPartySummonEligibilityLadderOrder(t *testing.T) {
	f := newPartySummonFixture(t, instanceWorldTestPartyDefinition)
	// MemberA stands in the field and is dead: _1 (outside of a town)
	// is the first failing line. MemberB stands in town and is dead:
	// _8 (dead) is theirs.
	movePartySummonMember(t, f.rt, f.members[0], partySummonFieldPose)
	killPartySummonMember(f.members[0])
	killPartySummonMember(f.members[1])
	payload := f.roster(t)
	_, _, _, rows := rosterRows(t, payload)
	for _, member := range f.members[:2] {
		if rows[enterworld.ObjectIDForCharacter(member)][0] != uint16(partySummonImpossible) {
			t.Fatalf("%s must read impossible", member.Name)
		}
	}
	refusal := f.summon(t)
	if len(refusal) != 3 || refusal[0] != partySummonKindRefusal ||
		binary.LittleEndian.Uint16(refusal[1:3]) != partySummonCodeOutsideTown {
		t.Fatalf("refusal = %x, want _1's code (the first member's first failure)", refusal)
	}
	// MemberC, the only possible member, was prompted.
	if len(f.pushed["MemberC"]) != 1 || f.pushed["MemberC"][0].Opcode != opPartySummonAnswer {
		t.Fatalf("MemberC pushes = %v, want the prompt", f.pushed["MemberC"])
	}
	if len(f.pushed["MemberA"]) != 0 || len(f.pushed["MemberB"]) != 0 {
		t.Fatal("impossible members must not be prompted")
	}
}

/*
================
TestPartySummonPromptsAnswersAndCooldown
================
*/
func TestPartySummonPromptsAnswersAndCooldown(t *testing.T) {
	f := newPartySummonFixture(t, instanceWorldTestPartyDefinition)
	f.summon(t)
	for _, member := range f.members {
		pushed := f.pushed[member.Name]
		if len(pushed) != 1 || pushed[0].Opcode != opPartySummonAnswer {
			t.Fatalf("%s pushes = %v, want one prompt", member.Name, pushed)
		}
		prompt := pushed[0].Payload
		if len(prompt) < 7 || prompt[0] != partySummonKindPrompt ||
			binary.LittleEndian.Uint32(prompt[1:5]) != enterworld.ObjectIDForCharacter(f.leader) ||
			string(prompt[7:]) != f.leader.Name {
			t.Fatalf("%s prompt = %x", member.Name, prompt)
		}
	}
	// A second summon while the requests are pending: WAIT_ERR.
	refusal := f.summon(t)
	if len(refusal) != 3 || refusal[0] != partySummonKindRefusal ||
		binary.LittleEndian.Uint16(refusal[1:3]) != partySummonCodePending {
		t.Fatalf("refusal = %x, want the pending code", refusal)
	}
	// MemberA accepts: transferred into the leader's layer, and the
	// leader's window refreshes with MemberA entered.
	answer := f.rt.HandlePartySummon(testDivision, f.members[0], []byte{partySummonActionAnswer, 1})
	if len(answer.Frames) != 0 {
		t.Fatalf("answer frames = %v, want none for the member", answer.Frames)
	}
	if !f.inside(f.members[0]) {
		t.Fatal("MemberA not inside after accepting")
	}
	leaderPushes := f.pushed["Leader"]
	if len(leaderPushes) != 1 || leaderPushes[0].Opcode != opPartySummonAnswer {
		t.Fatalf("leader pushes = %v, want the roster refresh", leaderPushes)
	}
	_, summoned, _, rows := rosterRows(t, leaderPushes[0].Payload)
	if summoned != 2 {
		t.Fatalf("summoned %d, want 2 after MemberA entered", summoned)
	}
	if row := rows[enterworld.ObjectIDForCharacter(f.members[0])]; row[1] != 1 {
		t.Fatalf("MemberA row = %v, want entered", row)
	}
	// MemberB and MemberC decline: both stay outside, and no request
	// is pending any more.
	f.rt.HandlePartySummon(testDivision, f.members[1], []byte{partySummonActionAnswer, 0})
	if f.inside(f.members[1]) {
		t.Fatal("MemberB inside after declining")
	}
	f.rt.HandlePartySummon(testDivision, f.members[2], []byte{partySummonActionAnswer, 0})
	// The leader's next summon inside 5 s of the first reads the
	// button wait (the native BUTTON line).
	notice := f.summon(t)
	if len(notice) < 3 || notice[0] != partySummonKindNotice ||
		string(notice[3:]) != "UIIT_STT_PARTY_SUMMON_BUTTON" {
		t.Fatalf("notice = %x, want the 5 second button wait", notice)
	}
	// Past the wait the summon prompts the two who stayed outside.
	f.clock.now = f.clock.now.Add(time.Duration(partySummonCooldownMs) * time.Millisecond)
	f.summon(t)
	if got := len(f.pushed["MemberB"]); got != 2 {
		t.Fatalf("MemberB pushes = %d, want a second prompt", got)
	}
	if got := len(f.pushed["MemberC"]); got != 2 {
		t.Fatalf("MemberC pushes = %d, want a second prompt", got)
	}
	// MemberA stands inside already: no new prompt may name them.
	for _, frame := range f.pushed["MemberA"][1:] {
		if len(frame.Payload) > 0 && frame.Payload[0] == partySummonKindPrompt {
			t.Fatal("MemberA prompted again while already inside")
		}
	}
}

/*
================
TestPartySummonAnswerWindowLapses
================
*/
func TestPartySummonAnswerWindowLapses(t *testing.T) {
	f := newPartySummonFixture(t, instanceWorldTestPartyDefinition)
	f.summon(t)
	f.clock.now = f.clock.now.Add((partySummonAnswerWindowMs + 1) * time.Millisecond)
	f.rt.HandlePartySummon(testDivision, f.members[0], []byte{partySummonActionAnswer, 1})
	if f.inside(f.members[0]) {
		t.Fatal("a lapsed answer must not transfer")
	}
}

/*
================
TestPartySummonFullLayer
================
*/
func TestPartySummonFullLayer(t *testing.T) {
	f := newPartySummonFixture(t, instanceWorldTestSingleDefinition)
	payload := f.roster(t)
	allowed, summoned, _, rows := rosterRows(t, payload)
	if allowed != 1 || summoned != 1 {
		t.Fatalf("allowed %d summoned %d, want 1 and 1", allowed, summoned)
	}
	for _, member := range f.members {
		row := rows[enterworld.ObjectIDForCharacter(member)]
		if row[0] != uint16(partySummonImpossible) || row[2] != 0 {
			t.Fatalf("%s row = %v, want impossible with no entries left", member.Name, row)
		}
	}
	refusal := f.summon(t)
	if len(refusal) != 3 || refusal[0] != partySummonKindRefusal ||
		binary.LittleEndian.Uint16(refusal[1:3]) != partySummonCodeFull {
		t.Fatalf("refusal = %x, want the full code", refusal)
	}
	for _, member := range f.members {
		if len(f.pushed[member.Name]) != 0 {
			t.Fatalf("%s prompted into a full layer", member.Name)
		}
	}
}

/*
================
TestPartySummonJobOutfit
================
*/
func TestPartySummonJobOutfit(t *testing.T) {
	f := newPartySummonFixture(t, instanceWorldTestPartyDefinition)
	// MemberA wears a trader suit (type 3/1/7, job 1) in socket 8.
	suit := domain.InventoryRow{Slot: enterworld.JobSuitSlot}
	suit.TypeFlags = 3<<2 | 1<<5 | 7<<7 | 1<<11
	f.members[0].MissionInventory = append(f.members[0].MissionInventory, suit)
	payload := f.roster(t)
	_, _, _, rows := rosterRows(t, payload)
	if rows[enterworld.ObjectIDForCharacter(f.members[0])][0] != uint16(partySummonImpossible) {
		t.Fatal("a member in a job outfit must read impossible")
	}
	refusal := f.summon(t)
	if len(refusal) != 3 || refusal[0] != partySummonKindRefusal ||
		binary.LittleEndian.Uint16(refusal[1:3]) != partySummonCodeJobOutfit {
		t.Fatalf("refusal = %x, want the job outfit code", refusal)
	}
}

/*
================
TestPartySummonTempleBandLevel

A temple band's layer carries its level band (_3): a member below the
band reads Impossible and the summon answers the level code.
================
*/
func TestPartySummonTempleBandLevel(t *testing.T) {
	if err := instance.RegisterPortOnly(instance.TempleDefinitions()); err != nil {
		t.Fatal(err)
	}
	var bandWorld temple.World
	for _, world := range temple.Worlds() {
		if world.MinLevel > 1 {
			bandWorld = world
			break
		}
	}
	if bandWorld.ID == 0 {
		t.Fatal("no temple band above level 1")
	}
	f := newPartySummonFixture(t, instance.DefinitionID(bandWorld.ID))
	payload := f.roster(t)
	_, _, _, rows := rosterRows(t, payload)
	for _, member := range f.members {
		if rows[enterworld.ObjectIDForCharacter(member)][0] != uint16(partySummonImpossible) {
			t.Fatalf("%s (level 1) must read impossible in a %d+ band", member.Name, bandWorld.MinLevel)
		}
	}
	refusal := f.summon(t)
	if len(refusal) != 3 || refusal[0] != partySummonKindRefusal ||
		binary.LittleEndian.Uint16(refusal[1:3]) != partySummonCodeLevel {
		t.Fatalf("refusal = %x, want the level code", refusal)
	}
}

/*
================
movePartySummonMember
================
*/
func movePartySummonMember(t *testing.T, rt *Runtime, c *enterworld.Character, pose simulation.Spawn) {
	t.Helper()
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = pose
			w.SpawnSet = true
		})
}

/*
================
killPartySummonMember
================
*/
func killPartySummonMember(c *enterworld.Character) {
	hp := int64(0)
	c.CurrentHP = &hp
}
