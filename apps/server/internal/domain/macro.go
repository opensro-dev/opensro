/*
===========================================================================

macro.go - the stored SRO Macro settings of one character (#3907)

Port-only, not native to v1.150: the v1.696 client keeps its macro
configuration on the server (opcode 0x3555 replays it at entry, 0x7554
saves it) and the port stores the same three sections here. Every field is
a fixed-size value so the record copies by assignment and compares with ==;
CloneCharacter needs no rule for it.

===========================================================================
*/
package domain

const (
	// MacroPotionBytes is the v1.696 potion block: nine 8-byte rows
	// (client 00db1650 writes it, 00cfcd80 reads it for version 3).
	MacroPotionBytes = 0x48
	// MacroHuntBytes is the v1.696 hunt block (client 00da8bb0, version 2).
	MacroHuntBytes = 0x0C
	// MacroSkillFlags are the four single-byte skill records, tags 0, 1, 3
	// and 4 of the client's save (00694170).
	MacroSkillFlags = 4
	// MacroSkillLists are the attack (tag 2) and buff (tag 5) lists.
	MacroSkillLists = 2
	// MacroSkillsPerList caps one list. The client grows a vector without
	// a visible bound; 32 is the inferred limit, above the slot grid of
	// res_ui\macro_2.2dt, so a native-sized list always fits.
	MacroSkillsPerList = 32
)

/*
================
MacroSkill

One registered skill: the weapon/shield tab byte and the skill id, the
5-byte record of the client's lists.
================
*/
type MacroSkill struct {
	Slot  uint8  `json:"slot"`
	Skill uint32 `json:"skill"`
}

/*
================
MacroSkillList
================
*/
type MacroSkillList struct {
	Count  uint8                          `json:"count"`
	Skills [MacroSkillsPerList]MacroSkill `json:"skills"`
}

/*
================
MacroSettings

The three sections and their ON states. HasPotion and HasHunt tell an
unsaved section (the client loads its defaults) from a saved one.
================
*/
type MacroSettings struct {
	PotionOn   bool                            `json:"potionOn,omitempty"`
	SkillOn    bool                            `json:"skillOn,omitempty"`
	HuntOn     bool                            `json:"huntOn,omitempty"`
	HasPotion  bool                            `json:"hasPotion,omitempty"`
	HasHunt    bool                            `json:"hasHunt,omitempty"`
	Potion     [MacroPotionBytes]byte          `json:"potion,omitzero"`
	Hunt       [MacroHuntBytes]byte            `json:"hunt,omitzero"`
	SkillFlags [MacroSkillFlags]byte           `json:"skillFlags,omitzero"`
	SkillLists [MacroSkillLists]MacroSkillList `json:"skillLists,omitzero"`
}
