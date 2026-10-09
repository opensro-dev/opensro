/*
===========================================================================

ended_test.go - the Khotan blacksmith's fork and the ended quest state

Exercise the shipped definitions: SMITH_2 and ACCESSORY_2 open after the
third ACCESSORY_1; refusing SMITH_2's first page ends the blacksmith line
and opens the accessory line; pressing Accept ends the accessory line,
even when the acceptance itself is refused (8A77A0). An ended quest that
was completed keeps its completion, its count and its login wire row.

===========================================================================
*/
package quest

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"slices"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

const (
	ktAccessory1 = "QNO_KT_ACCESSORY_1"
	ktSmith2     = "QNO_KT_SMITH_2"
	ktSmith3     = "QNO_KT_SMITH_3"
	ktAccessory2 = "QNO_KT_ACCESSORY_2"
	ktAccessory3 = "QNO_KT_ACCESSORY_3"
)

/*
================
ktForkFixture

A level-70 character (the v1.150 rows are level 68) that has completed
ACCESSORY_1 completions times.
================
*/
func ktForkFixture(t *testing.T, completions uint32) (*Runtime, *enterworld.Character) {
	t.Helper()
	licensed.RequireGameData(t)
	rt := captureCatalogRuntime(t)
	c := questCharacter()
	*c.Level = 70
	experience := int64(0)
	c.Experience = &experience
	first := ktID(t, rt, ktAccessory1)
	c.CompletedQuestIds = []uint32{first}
	c.QuestCompletionCounts = map[uint32]uint32{first: completions}
	return rt, c
}

/*
================
ktID
================
*/
func ktID(t *testing.T, rt *Runtime, code string) uint32 {
	t.Helper()
	def, ok := rt.Defs.ByCodename(code)
	if !ok {
		t.Fatalf("%s is not loaded", code)
	}
	return def.RefID
}

/*
================
offered

Whether npc offers code as a new quest.
================
*/
func offered(rt *Runtime, c *enterworld.Character, code, npc string) bool {
	row, ok := npcRow(rt, c, code, npc)
	return ok && !row.Complete && !row.Informational
}

/*
================
TestKhotanForkNeedsThreeAccessoryRuns

ACCESSORY_1 must be completed three times (+0x418 = 3) before either the
blacksmith or the accessory merchant opens the next quest.
================
*/
func TestKhotanForkNeedsThreeAccessoryRuns(t *testing.T) {
	rt, c := ktForkFixture(t, 2)
	if offered(rt, c, ktSmith2, "NPC_KT_SMITH") {
		t.Fatal("SMITH_2 offered after two ACCESSORY_1 runs")
	}
	if _, err := rt.StartQuest(c, ktSmith2); err == nil {
		t.Fatal("SMITH_2 accepted after two ACCESSORY_1 runs")
	}
	c.QuestCompletionCounts[ktID(t, rt, ktAccessory1)] = 3
	row, ok := npcRow(rt, c, ktSmith2, "NPC_KT_SMITH")
	if !ok || len(row.Pages) != 1 || row.Pages[0].RefuseSymbol != "SN_TALK_QNO_KT_SMITH_2_04" {
		t.Fatalf("SMITH_2 offer after three runs = %+v, %v; want its _01 page with the _04 refusal", row, ok)
	}
	// ACCESSORY_2 also needs the blacksmith line ended, which it is not.
	if offered(rt, c, ktAccessory2, "NPC_KT_ACCESSORY") {
		t.Fatal("ACCESSORY_2 offered while the blacksmith line is open")
	}
}

/*
================
TestKhotanRefusalEndsTheBlacksmithLine
================
*/
func TestKhotanRefusalEndsTheBlacksmithLine(t *testing.T) {
	rt, c := ktForkFixture(t, 3)
	if _, err := rt.RefuseQuestOffer(c, ktSmith2); err != nil {
		t.Fatal(err)
	}
	want := []uint32{ktID(t, rt, ktSmith2), ktID(t, rt, ktSmith3)}
	if !slices.Equal(c.EndedQuestIds, want) {
		t.Fatalf("ended = %v, want %v", c.EndedQuestIds, want)
	}
	if offered(rt, c, ktSmith2, "NPC_KT_SMITH") {
		t.Fatal("an ended SMITH_2 is still offered")
	}
	if _, err := rt.StartQuest(c, ktSmith2); err == nil {
		t.Fatal("an ended SMITH_2 was accepted")
	}
	if !offered(rt, c, ktAccessory2, "NPC_KT_ACCESSORY") {
		t.Fatal("ACCESSORY_2 not offered once the blacksmith line ended")
	}
	// A second refusal is no longer on offer and ends nothing more.
	if _, err := rt.RefuseQuestOffer(c, ktSmith2); err == nil {
		t.Fatal("a refusal of an ended quest was accepted")
	}
	if len(c.EndedQuestIds) != 2 || len(c.CompletedQuestIds) != 1 {
		t.Fatalf("refusal changed history: ended %v completed %v", c.EndedQuestIds, c.CompletedQuestIds)
	}
}

/*
================
TestKhotanRefusalNeedsALiveOffer

A forged refusal ends nothing: the offer must stand.
================
*/
func TestKhotanRefusalNeedsALiveOffer(t *testing.T) {
	rt, c := ktForkFixture(t, 2)
	if _, err := rt.RefuseQuestOffer(c, ktSmith2); err == nil {
		t.Fatal("refused SMITH_2 before it was on offer")
	}
	if _, err := rt.RefuseQuestOffer(c, ktSmith3); err == nil {
		t.Fatal("refused SMITH_3, which has no refusal row")
	}
	if len(c.EndedQuestIds) != 0 {
		t.Fatalf("a refused refusal ended %v", c.EndedQuestIds)
	}
}

/*
================
TestKhotanAcceptEndsTheAccessoryLine

Accepting SMITH_2 ends ACCESSORY_2 and ACCESSORY_3; so does an Accept the
acceptance refuses (8A7857 skips only the start).
================
*/
func TestKhotanAcceptEndsTheAccessoryLine(t *testing.T) {
	want := func(rt *Runtime) []uint32 {
		return []uint32{ktID(t, rt, ktAccessory2), ktID(t, rt, ktAccessory3)}
	}
	t.Run("accepted", func(t *testing.T) {
		rt, c := ktForkFixture(t, 3)
		if _, err := rt.StartQuest(c, ktSmith2); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(c.EndedQuestIds, want(rt)) || activeQuestIndex(c, ktID(t, rt, ktSmith2)) < 0 {
			t.Fatalf("ended %v, SMITH_2 active %v", c.EndedQuestIds, activeQuestIndex(c, ktID(t, rt, ktSmith2)) >= 0)
		}
	})
	t.Run("acceptance refused", func(t *testing.T) {
		rt, c := ktForkFixture(t, 3)
		for id := uint32(1); len(c.ActiveQuests) < maxQuestWireRecords; id++ {
			c.ActiveQuests = append(c.ActiveQuests, enterworld.ActiveQuestRecord{RefID: 0xF0000000 + id})
		}
		if _, err := rt.StartQuest(c, ktSmith2); err == nil {
			t.Fatal("SMITH_2 accepted with a full quest list")
		}
		if !slices.Equal(c.EndedQuestIds, want(rt)) {
			t.Fatalf("ended %v, want %v", c.EndedQuestIds, want(rt))
		}
	})
}

/*
================
TestEndedCompletedQuestKeepsItsCompletion

Native SetQuestState(5) overwrites an existing record and keeps its count.
A completed quest marked ended stays completed, keeps its count and its
row in the login completed list; the ended list never reaches the wire.
================
*/
func TestEndedCompletedQuestKeepsItsCompletion(t *testing.T) {
	rt, c := ktForkFixture(t, 3)
	first := ktID(t, rt, ktAccessory1)
	if !markQuestsEnded(c, []uint32{first}) || markQuestsEnded(c, []uint32{first}) {
		t.Fatal("marking ended is not once-only")
	}
	if !questCompleted(c, first) || completionCount(c, first) != 3 {
		t.Fatalf("ended ACCESSORY_1: completed %v count %d, want true 3", questCompleted(c, first), completionCount(c, first))
	}
	payload := enterworld.BuildLocalPlayerEntryPayload(c, &enterworld.LocalPlayerEntry{}, 0, nil)
	section := []byte{0x00, 0x02, 0x01}
	section = binary.LittleEndian.AppendUint32(section, first)
	section = append(section, 0x00)
	if !bytes.Contains(payload, section) {
		t.Fatalf("login quest block lacks the completed row % X", section)
	}
}

/*
================
TestEndedQuestsSurviveJSON
================
*/
func TestEndedQuestsSurviveJSON(t *testing.T) {
	c := &enterworld.Character{Name: "ended", EndedQuestIds: []uint32{7, 9}}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back enterworld.Character
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(back.EndedQuestIds, []uint32{7, 9}) {
		t.Fatalf("ended quests after reload = %v", back.EndedQuestIds)
	}
}
