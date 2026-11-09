/*
===========================================================================

guildrecall_test.go - the Guild Recall Scroll conversation, end to end

#2999: the master's use spends one scroll and asks the online members
(0x77C0); an accept inside the window brings the member to where the
master stood (0xB178 {1}); a decline or a lapsed window moves nobody;
a forbidden member hears the recall error (0xB178 {2, 0xC1}). The
member is parked away from the master so the teleport is observable.

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
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// The bag slot the fixture parks the scroll in.
const recallScrollSlot = 21

/*
================
guildRecallFixture

A guild master holding two recall scrolls and one joined member, both
admitted to the world, with every pushed frame recorded by name.
================
*/
type guildRecallFixture struct {
	d        *doorRuntime
	master   *enterworld.Character
	member   *enterworld.Character
	guildID  int64
	masterAt simulation.Spawn
	pushes   map[string][]wire.Frame
}

func newGuildRecallFixture(t *testing.T) *guildRecallFixture {
	t.Helper()
	d := guildManagerFixture(t)
	master := d.character
	if master.GuildID == nil {
		t.Fatal("fixture: master has no guild")
	}
	guildID := *master.GuildID

	member := testCharacter()
	member.Name = "RecallMember"
	if err := d.authority.CreateCharacter(testDivision, "recall-member", member); err != nil {
		t.Fatal(err)
	}
	if _, refusal := d.authority.Guilds().AddGuildMemberAs(testDivision, guildID, master.ID, 0,
		domain.GuildMemberRecord{CharID: member.ID, JID: 2, Name: member.Name, Grade: guild.JoinerGrade}); refusal.Refused() {
		t.Fatalf("fixture member join: %v", refusal)
	}
	if member.GuildID == nil {
		t.Fatal("fixture: member has no guild")
	}

	refs := testItems()
	refs["ITEM_ETC_GUILD_RECALL"] = &enterworld.ItemRef{Codename: "ITEM_ETC_GUILD_RECALL", RefObjID: 9267, Country: 3,
		TypeIDs: [4]int64{3, 3, 12, 3},
		NativeFields: enterworld.NewNativeFields(map[string]float64{
			"canUse": 1, "itemParam1_29c": 3600000, "itemParam2_2a0": 30000, "itemParam18_2e0": -1})}
	d.rt.deps.(*enterworld.Deps).Items = refs
	scroll := refs["ITEM_ETC_GUILD_RECALL"]
	giveScroll := func(c *enterworld.Character) {
		d.authority.UpdateCharacter(c, "guild-recall-fixture", func() bool {
			c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: recallScrollSlot,
				RefObjID: 9267, Codename: "ITEM_ETC_GUILD_RECALL", TypeFlags: scroll.TypeFlags(), StackCount: 2})
			return true
		})
	}
	giveScroll(master)
	giveScroll(member)

	deps := d.rt.deps.(*enterworld.Deps)
	deps.CanEnterWorldRegion = func(*enterworld.Character, uint16) bool { return true }
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	d.rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	d.rt.Monsters.StartDivision(testDivision)
	for session, c := range []*enterworld.Character{master, member} {
		if err := d.rt.admitPopulationSession(testDivision, c.Name, uint64(session+11)); err != nil {
			t.Fatal(err)
		}
	}

	f := &guildRecallFixture{d: d, master: master, member: member, guildID: guildID,
		masterAt: simulation.Spawn{RegionID: 0x62A8, X: 640, Y: 20, Z: 812, Angle: 90},
		pushes:   map[string][]wire.Frame{}}
	f.d.rt.PushCharacterFrames = func(_ string, name string, frames []wire.Frame) {
		f.pushes[name] = append(f.pushes[name], frames...)
	}
	// The master stands somewhere the member is not.
	f.d.rt.Worlds.Update(simulation.WorldKey(testDivision, master.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(master) },
		func(w *simulation.WorldState) {
			w.Spawn = f.masterAt
			w.SpawnSet = true
			w.MovementSourceSeeded = true
		})
	return f
}

/*
================
useRecall

The ordinary 0x75BD use of the fixture's bag-slot scroll.
================
*/
func (f *guildRecallFixture) useRecall(t *testing.T, c *enterworld.Character) OpResult {
	t.Helper()
	ref := f.d.rt.deps.ItemReferences().(staticItemSource)["ITEM_ETC_GUILD_RECALL"]
	return f.d.rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(recallScrollSlot).U16(ref.TypeFlags()).Payload())
}

/*
================
answerRecall

One 0x7178 answer with the given echo and accept byte value.
================
*/
func (f *guildRecallFixture) answerRecall(c *enterworld.Character, echoA, echoB uint32, accept byte) OpResult {
	body := wire.NewWriter(9).U32(echoA).U32(echoB).U8(accept).Payload()
	return f.d.rt.HandleGuildRecallAnswer(testDivision, c, body)
}

/*
================
summonAsk

The 0x77C0 the named character was pushed, decoded (echo, window).
================
*/
func (f *guildRecallFixture) summonAsk(t *testing.T, name string) (uint32, uint32, uint32) {
	t.Helper()
	for _, frame := range f.pushes[name] {
		if frame.Opcode == guild.OpGuildRecallSummon {
			if len(frame.Payload) != 12 {
				t.Fatalf("summon payload %x", frame.Payload)
			}
			return binary.LittleEndian.Uint32(frame.Payload[0:]),
				binary.LittleEndian.Uint32(frame.Payload[4:]),
				binary.LittleEndian.Uint32(frame.Payload[8:])
		}
	}
	t.Fatalf("no summon reached %s (pushes %+v)", name, f.pushes)
	return 0, 0, 0
}

/*
================
scrollStack

The scroll stack still in the character's bag (0 when spent).
================
*/
func scrollStack(c *enterworld.Character) int64 {
	if row := bagRowByCodename(c, "ITEM_ETC_GUILD_RECALL"); row != nil {
		return row.StackCount
	}
	return 0
}

/*
================
TestGuildRecallUseAsksTheMembersAndSpendsOne
================
*/
func TestGuildRecallUseAsksTheMembersAndSpendsOne(t *testing.T) {
	f := newGuildRecallFixture(t)
	result := f.useRecall(t, f.master)
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse ||
		len(result.Frames[0].Payload) < 1 || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("the master's use answered %+v", result.Frames)
	}
	if got := scrollStack(f.master); got != 1 {
		t.Fatalf("master stack %d, want 1 of 2 spent", got)
	}
	echoA, echoB, seconds := f.summonAsk(t, f.member.Name)
	if echoA != uint32(f.guildID) || echoB != enterworld.ObjectIDForCharacter(f.master) || seconds != 30 {
		t.Fatalf("summon echo %d/%d window %ds", echoA, echoB, seconds)
	}
	for _, frame := range f.pushes[f.master.Name] {
		if frame.Opcode == guild.OpGuildRecallSummon {
			t.Fatal("the master was asked to summon himself")
		}
	}
}

/*
================
TestGuildRecallAcceptBringsTheMemberToTheMaster
================
*/
func TestGuildRecallAcceptBringsTheMemberToTheMaster(t *testing.T) {
	f := newGuildRecallFixture(t)
	f.useRecall(t, f.master)
	echoA, echoB, _ := f.summonAsk(t, f.member.Name)
	before := spawnOf(f.member)
	if before == f.masterAt {
		t.Fatal("fixture: the member already stands at the master's point")
	}
	// A forged echo is not an answer this server sent: silent, and the
	// real ask stays open behind it.
	if out := f.answerRecall(f.member, echoA, echoB+1, 1); len(out.Frames) != 0 {
		t.Fatalf("a forged echo answered %+v", out.Frames)
	}
	out := f.answerRecall(f.member, echoA, echoB, 1)
	if len(out.Frames) != 1 || out.Frames[0].Opcode != guild.OpGuildRecallResult ||
		string(out.Frames[0].Payload) != string(guild.EncodeGuildRecallAccepted()) {
		t.Fatalf("the accept answered %+v", out.Frames)
	}
	if got := spawnOf(f.member); got != f.masterAt {
		t.Fatalf("the member stands at %+v, want the master's %+v", got, f.masterAt)
	}
	// The ask is spent: a second accept has nothing to answer.
	if out := f.answerRecall(f.member, echoA, echoB, 1); len(out.Frames) != 0 {
		t.Fatalf("a second accept answered %+v", out.Frames)
	}
}

/*
================
TestGuildRecallDeclineMovesNobody
================
*/
func TestGuildRecallDeclineMovesNobody(t *testing.T) {
	f := newGuildRecallFixture(t)
	f.useRecall(t, f.master)
	echoA, echoB, _ := f.summonAsk(t, f.member.Name)
	before := spawnOf(f.member)
	if out := f.answerRecall(f.member, echoA, echoB, 0); len(out.Frames) != 0 {
		t.Fatalf("a decline answered %+v", out.Frames)
	}
	if got := spawnOf(f.member); got != before {
		t.Fatalf("the decline moved the member to %+v", got)
	}
	if out := f.answerRecall(f.member, echoA, echoB, 1); len(out.Frames) != 0 {
		t.Fatalf("an accept after the decline answered %+v", out.Frames)
	}
}

/*
================
TestGuildRecallForbiddenMemberHearsTheRecallError
================
*/
func TestGuildRecallForbiddenMemberHearsTheRecallError(t *testing.T) {
	states := []struct {
		name  string
		spoil func(f *guildRecallFixture)
	}{
		{"dead", func(f *guildRecallFixture) {
			f.d.authority.UpdateCharacter(f.member, "guild-recall-test", func() bool {
				hp := int64(0)
				f.member.CurrentHP = &hp
				return true
			})
		}},
		{"in battle", func(f *guildRecallFixture) {
			f.d.authority.UpdateCharacter(f.member, "guild-recall-test", func() bool {
				f.member.BattleUntilMs = f.d.clock.NowMs() + 60_000
				return true
			})
		}},
		{"teleport in flight", func(f *guildRecallFixture) {
			f.d.authority.UpdateCharacter(f.member, "guild-recall-test", func() bool {
				f.member.NativeTeleportMode = 1
				return true
			})
		}},
	}
	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			f := newGuildRecallFixture(t)
			f.useRecall(t, f.master)
			echoA, echoB, _ := f.summonAsk(t, f.member.Name)
			state.spoil(f)
			before := spawnOf(f.member)
			out := f.answerRecall(f.member, echoA, echoB, 1)
			if len(out.Frames) != 1 || out.Frames[0].Opcode != guild.OpGuildRecallResult ||
				string(out.Frames[0].Payload) != string(guild.EncodeGuildRecallRefused(wire.ErrCodeGuildRecallRefused)) {
				t.Fatalf("the forbidden accept answered %+v", out.Frames)
			}
			if got := spawnOf(f.member); got != before {
				t.Fatalf("the refusal moved the member to %+v", got)
			}
		})
	}
}

/*
================
TestGuildRecallSecondUseWhileTheWindowStandsIsRefused
================
*/
func TestGuildRecallSecondUseWhileTheWindowStandsIsRefused(t *testing.T) {
	f := newGuildRecallFixture(t)
	f.useRecall(t, f.master)
	out := f.useRecall(t, f.master)
	if len(out.Frames) != 1 || out.Frames[0].Opcode != wire.OpItemUseResponse ||
		string(out.Frames[0].Payload) != string(wire.EncodeItemUseError(wire.ErrCodeItemReuseDelay)) {
		t.Fatalf("the second use answered %+v", out.Frames)
	}
	if got := scrollStack(f.master); got != 1 {
		t.Fatalf("master stack %d, want the refused use unspent", got)
	}
}

/*
================
TestGuildRecallAnswerAfterTheWindowLapsesMovesNobody
================
*/
func TestGuildRecallAnswerAfterTheWindowLapsesMovesNobody(t *testing.T) {
	f := newGuildRecallFixture(t)
	f.useRecall(t, f.master)
	echoA, echoB, _ := f.summonAsk(t, f.member.Name)
	f.d.clock.Advance(31 * time.Second)
	before := spawnOf(f.member)
	out := f.answerRecall(f.member, echoA, echoB, 1)
	if len(out.Frames) != 1 || out.Frames[0].Opcode != guild.OpGuildRecallResult ||
		string(out.Frames[0].Payload) != string(guild.EncodeGuildRecallClosed()) {
		t.Fatalf("the lapsed accept answered %+v", out.Frames)
	}
	if got := spawnOf(f.member); got != before {
		t.Fatalf("the lapsed accept moved the member to %+v", got)
	}
}

/*
================
TestGuildRecallPlainMemberUseIsRefused
================
*/
func TestGuildRecallPlainMemberUseIsRefused(t *testing.T) {
	f := newGuildRecallFixture(t)
	out := f.useRecall(t, f.member)
	if len(out.Frames) != 1 || out.Frames[0].Opcode != wire.OpItemUseResponse ||
		string(out.Frames[0].Payload) != string(wire.EncodeItemUseError(wire.ErrCodeGuildRecallNotMaster)) {
		t.Fatalf("a plain member's use answered %+v", out.Frames)
	}
	if got := scrollStack(f.member); got != 2 {
		t.Fatalf("member stack %d, want the refused use unspent", got)
	}
	for _, frame := range f.pushes[f.master.Name] {
		if frame.Opcode == guild.OpGuildRecallSummon {
			t.Fatal("a refused use still asked the guild")
		}
	}
}
