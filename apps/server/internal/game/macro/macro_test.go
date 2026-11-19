/*
===========================================================================

macro_test.go - the SRO Macro settings lane against the v1.696 shapes

===========================================================================
*/
package macro

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
savePayload

A client save: action 1, kind, u16 version, the leading byte, the block.
================
*/
func savePayload(kind uint8, version uint16, block []byte) []byte {
	out := []byte{actionSave, kind}
	out = binary.LittleEndian.AppendUint16(out, version)
	out = append(out, 0xEE)
	return append(out, block...)
}

/*
================
TestEnabledIsOffUnlessSwitchedOn
================
*/
func TestEnabledIsOffUnlessSwitchedOn(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, " TRUE ": true, "1": true} {
		if got := Enabled(func(string) string { return value }); got != want {
			t.Errorf("Enabled(%q) = %v, want %v", value, got, want)
		}
	}
}

/*
================
TestSwitchStoresStateAndEchoesIt
================
*/
func TestSwitchStoresStateAndEchoesIt(t *testing.T) {
	got := Apply(domain.MacroSettings{}, []byte{actionSwitch, KindHunt, 1})
	if !got.Changed || !got.Settings.HuntOn || got.Settings.PotionOn || got.Settings.SkillOn {
		t.Fatalf("switch hunt on: %+v", got)
	}
	if want := []byte{actionSwitch, KindHunt, 1, 0, 1}; !bytes.Equal(got.Payload, want) {
		t.Fatalf("answer % x, want % x", got.Payload, want)
	}
	again := Apply(got.Settings, []byte{actionSwitch, KindHunt, 1})
	if again.Changed {
		t.Fatalf("repeating a switch must not rewrite the character")
	}
}

/*
================
TestRefusedRequestsAnswerTheFailureWord
================
*/
func TestRefusedRequestsAnswerTheFailureWord(t *testing.T) {
	current := domain.MacroSettings{PotionOn: true}
	cases := []struct {
		name    string
		payload []byte
		want    []byte
	}{
		{"unknown kind", []byte{actionSwitch, 7, 1}, []byte{actionSwitch, 7, 0x06, 0x98}},
		{"state above one", []byte{actionSwitch, KindPotion, 2}, []byte{actionSwitch, KindPotion, 0x06, 0x98}},
		{"short potion block", savePayload(KindPotion, potionVersion, make([]byte, 10)), []byte{actionSave, KindPotion, 2, 0}},
		{"wrong hunt version", savePayload(KindHunt, 9, make([]byte, domain.MacroHuntBytes)), []byte{actionSave, KindHunt, 2, 0}},
	}
	for _, c := range cases {
		got := Apply(current, c.payload)
		if got.Changed || got.Settings != current || !bytes.Equal(got.Payload, c.want) {
			t.Errorf("%s: %+v, want answer % x and no change", c.name, got, c.want)
		}
	}
	if got := Apply(current, []byte{actionSave}); got.Payload != nil || got.Changed {
		t.Errorf("a request without a kind has no conversation: %+v", got)
	}
}

/*
================
TestPotionAndHuntSavesReplayAtEntry
================
*/
func TestPotionAndHuntSavesReplayAtEntry(t *testing.T) {
	potion := bytes.Repeat([]byte{0x11}, domain.MacroPotionBytes)
	hunt := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	s := Apply(domain.MacroSettings{}, savePayload(KindPotion, potionVersion, potion)).Settings
	s = Apply(s, savePayload(KindHunt, huntVersion, hunt)).Settings
	s = Apply(s, []byte{actionSwitch, KindPotion, 1}).Settings

	want := []byte{configFull, availableAll, kindCount, KindPotion, 1, 3, 0, domain.MacroPotionBytes, 0}
	want = append(want, potion...)
	want = append(want, KindSkill, 0, 2, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0)
	want = append(want, KindHunt, 0, 2, 0, domain.MacroHuntBytes, 0)
	want = append(want, hunt...)
	if got := ConfigPayload(s); !bytes.Equal(got, want) {
		t.Fatalf("config\n got % x\nwant % x", got, want)
	}
}

/*
================
TestUnsavedSectionsLetTheClientLoadDefaults
================
*/
func TestUnsavedSectionsLetTheClientLoadDefaults(t *testing.T) {
	want := []byte{configFull, availableAll, kindCount,
		KindPotion, 0, 0, 0, 0, 0,
		KindSkill, 0, 2, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0,
		KindHunt, 0, 0, 0, 0, 0}
	if got := ConfigPayload(domain.MacroSettings{}); !bytes.Equal(got, want) {
		t.Fatalf("config\n got % x\nwant % x", got, want)
	}
}

/*
================
TestSkillSaveMergesOnlyTheSentRecords

The client sends only the records that changed; the rest keep their
stored values.
================
*/
func TestSkillSaveMergesOnlyTheSentRecords(t *testing.T) {
	first := []byte{actionSave, KindSkill, 2, 0,
		0, 1, // tag 0
		2, 10, 0, 0, 0x21, 0x00, 0x00, 0x00, 1, 0x22, 0x00, 0x00, 0x00, // tag 2: two attack skills
		4, 1} // tag 4
	s := Apply(domain.MacroSettings{}, first)
	if !s.Changed || s.Settings.SkillFlags != [4]byte{1, 0, 0, 1} || s.Settings.SkillLists[0].Count != 2 {
		t.Fatalf("first save: %+v", s.Settings)
	}
	second := []byte{actionSave, KindSkill, 2, 0,
		5, 5, 0, 1, 0x40, 0x00, 0x00, 0x00, // tag 5: one buff skill
		2, 1, 0, 0} // tag 2: attack list emptied
	got := Apply(s.Settings, second).Settings
	if got.SkillFlags != [4]byte{1, 0, 0, 1} {
		t.Errorf("flags not sent must stay: %v", got.SkillFlags)
	}
	if got.SkillLists[0].Count != 0 || got.SkillLists[1].Count != 1 || got.SkillLists[1].Skills[0] != (domain.MacroSkill{Slot: 1, Skill: 0x40}) {
		t.Errorf("lists: %+v", got.SkillLists)
	}

	want := []byte{configFull, availableAll, kindCount, KindPotion, 0, 0, 0, 0, 0,
		KindSkill, 0, 2, 0, 1, 0, 1, 0, 0, 0, 1, 5, 0, 1, 0x40, 0, 0, 0,
		KindHunt, 0, 0, 0, 0, 0}
	if config := ConfigPayload(got); !bytes.Equal(config, want) {
		t.Fatalf("config\n got % x\nwant % x", config, want)
	}
}

/*
================
TestSkillSaveRefusesBrokenRecords
================
*/
func TestSkillSaveRefusesBrokenRecords(t *testing.T) {
	over := []byte{actionSave, KindSkill, 2, 0, 2}
	over = binary.LittleEndian.AppendUint16(over, uint16((domain.MacroSkillsPerList+1)*skillRecordBytes))
	over = append(over, make([]byte, (domain.MacroSkillsPerList+1)*skillRecordBytes)...)
	for name, payload := range map[string][]byte{
		"unknown tag":       {actionSave, KindSkill, 2, 0, 9, 1},
		"truncated flag":    {actionSave, KindSkill, 2, 0, 0},
		"ragged list":       {actionSave, KindSkill, 2, 0, 2, 4, 0, 1, 2, 3, 4},
		"short list":        {actionSave, KindSkill, 2, 0, 2, 10, 0, 1, 2, 3, 4, 5},
		"empty marker set":  {actionSave, KindSkill, 2, 0, 2, 1, 0, 7},
		"list over the cap": over,
	} {
		got := Apply(domain.MacroSettings{SkillOn: true}, payload)
		if got.Changed || !bytes.Equal(got.Payload, []byte{actionSave, KindSkill, 2, 0}) {
			t.Errorf("%s: %+v", name, got)
		}
	}
}
