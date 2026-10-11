/*
===========================================================================

catalogue_test.go - status derivation, gap notes, type paths and schemas

===========================================================================
*/
package catalogue

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestEveryGapHasAnIssue

The only hand-written catalogue input must point at the issue tracking it.
================
*/
func TestEveryGapHasAnIssue(t *testing.T) {
	for key, notes := range gapNotes {
		for _, note := range notes {
			if strings.TrimSpace(note.Text) == "" || !strings.HasPrefix(note.Issue, "https://github.com/") {
				t.Errorf("gap note %s %+v needs player-word text and a GitHub issue link", key, note)
			}
		}
	}
}

/*
================
TestItemStatusComesFromTheServersRegistry
================
*/
func TestItemStatusComesFromTheServersRegistry(t *testing.T) {
	sword := &enterworld.ItemRef{RefObjID: 71, Codename: "ITEM_CH_SWORD_01_A", Name: "Copper Sword", TypeIDs: [4]int64{3, 1, 6, 2}}
	herb := &enterworld.ItemRef{RefObjID: 4, Codename: "ITEM_ETC_HP_POTION_01", Name: "HP recovery herb", TypeIDs: [4]int64{3, 3, 1, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	ticket := &enterworld.ItemRef{RefObjID: 9, Codename: "ITEM_MALL_TICKET", Name: "Ticket", TypeIDs: [4]int64{3, 3, 13, 9},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	silk := &enterworld.ItemRef{RefObjID: 2147, Codename: "ITEM_ETC_TRADE_CH_01", Name: "White Silk", TypeIDs: [4]int64{3, 3, 8, 1}}
	gold := &enterworld.ItemRef{RefObjID: 1, Codename: "ITEM_ETC_GOLD_01", Name: "Gold", TypeIDs: [4]int64{3, 3, 5, 0}}
	usable := func(ref *enterworld.ItemRef) bool { return ref.RefObjID == 4 }
	items := BuildItems(ItemSource{Refs: []*enterworld.ItemRef{ticket, silk, gold, herb, sword}, Usable: usable})
	want := map[uint32]Status{4: StatusImplemented, 9: StatusMissing, 71: StatusImplemented, 2147: StatusUnverified}
	if len(items) != len(want) {
		t.Fatalf("exported %d items, want %d (gold left out)", len(items), len(want))
	}
	for i, item := range items {
		if i > 0 && items[i-1].ID >= item.ID {
			t.Fatalf("items not in id order: %d before %d", items[i-1].ID, item.ID)
		}
		if item.Status != want[item.ID] {
			t.Errorf("%s status %s, want %s", item.Codename, item.Status, want[item.ID])
		}
		if item.Status == StatusMissing && (len(item.Gaps) == 0 || item.Issue == "") {
			t.Errorf("%s is missing without a gap and an issue", item.Codename)
		}
	}
}

/*
================
TestGapNoteMakesAnImplementedEntityPartial
================
*/
func TestGapNoteMakesAnImplementedEntityPartial(t *testing.T) {
	gapNotes["item:71"] = []Gap{{Text: "Its glow is not drawn yet.", Issue: "https://github.com/opensro-dev/opensro/issues/1"}}
	defer delete(gapNotes, "item:71")
	standing := applyGaps("item", 71, Standing{Status: StatusImplemented})
	if standing.Status != StatusPartial || len(standing.Gaps) != 1 || standing.Issue == "" {
		t.Fatalf("standing %+v, want partial with the note and its issue", standing)
	}
	if missing := applyGaps("item", 71, Standing{Status: StatusMissing}); missing.Status != StatusMissing {
		t.Fatalf("a gap note turned a missing entity into %s", missing.Status)
	}
}

/*
================
TestItemTypePathsFollowTheTooltipNames
================
*/
func TestItemTypePathsFollowTheTooltipNames(t *testing.T) {
	for ids, want := range map[[4]int64]string{
		{3, 1, 6, 2}:  "weapon/sword",
		{3, 1, 6, 14}: "weapon/harp",
		{3, 1, 3, 1}:  "armor/heavy/head",
		{3, 1, 10, 6}: "armor/eu-light/feet",
		{3, 1, 12, 3}: "accessory/ring",
		{3, 1, 4, 1}:  "shield",
		{3, 1, 13, 2}: "avatar/dress",
		{3, 3, 8, 2}:  "etc/trade-goods",
		{3, 3, 8, 8}:  "etc/8-8",
		{3, 3, 9, 0}:  "etc/quest",
		{3, 3, 1, 1}:  "etc/potion-hp",
		{3, 3, 10, 2}: "etc/alchemy/lucky-powder",
		{3, 3, 11, 5}: "etc/alchemy/element",
	} {
		if got := itemTypePath(ids); got != want {
			t.Errorf("%v -> %q, want %q", ids, got, want)
		}
	}
}

/*
================
TestSchemaCarriesTheStatusEnumAndRequiredFields
================
*/
func TestSchemaCarriesTheStatusEnumAndRequiredFields(t *testing.T) {
	schema := Schema("items.json", ItemsFile{})
	item := schema["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)
	properties := item["properties"].(map[string]any)
	status := properties["status"].(map[string]any)
	if enum, ok := status["enum"].([]string); !ok || len(enum) != 4 || enum[3] != "unverified" {
		t.Fatalf("status schema %+v", status)
	}
	required := strings.Join(item["required"].([]string), ",")
	for _, field := range []string{"id", "name", "type", "status", "tooltip"} {
		if !strings.Contains(","+required+",", ","+field+",") {
			t.Errorf("%s is not required (%s)", field, required)
		}
	}
	if strings.Contains(","+required+",", ",gaps,") || strings.Contains(","+required+",", ",degree,") {
		t.Errorf("an omitempty field is required: %s", required)
	}
}

/*
================
TestSummaryKeepsUnverifiedAsItsOwnBand
================
*/
func TestSummaryKeepsUnverifiedAsItsOwnBand(t *testing.T) {
	counts := countStatus([]Standing{{Status: StatusImplemented}, {Status: StatusUnverified}, {Status: StatusMissing}, {Status: StatusPartial}, {Status: StatusUnverified}})
	if counts != (StatusCounts{Implemented: 1, Partial: 1, Missing: 1, Unverified: 2, Total: 5}) {
		t.Fatalf("counts %+v", counts)
	}
}

/*
================
TestSkillStatusComesFromThePressRouter

Chain stages and non-player rows are left out; a rank the router does not
run is missing unless it is a passive the learn applies.
================
*/
func TestSkillStatusComesFromThePressRouter(t *testing.T) {
	rows := []enterworld.SkillRow{
		{ID: 1, Codename: "SKILL_CH_SWORD_SMASH_A_01", Group: 10, Level: 1},
		{ID: 2, Codename: "SKILL_CH_SWORD_SMASH_A_01_2", Group: 10, ChainSub: true},
		{ID: 3, Codename: "SKILL_EU_ROG_DAGGERA_01", Group: 11, Level: 1},
		{ID: 4, Codename: "SKILL_EU_WARRIOR_PASSIVE_01", Group: 12, Level: 1},
		{ID: 5, Codename: "MSKILL_TIGER_ATTACK", Group: 13, Level: 1},
	}
	skills := BuildSkills(SkillSource{Rows: rows,
		Handled: func(row enterworld.SkillRow) bool { return row.ID == 1 },
		Plan: func(id uint32) enterworld.SkillExecutionKind {
			if id == 4 {
				return enterworld.SkillExecutionPassive
			}
			return enterworld.SkillExecutionUnsupported
		}})
	want := map[uint32]Status{1: StatusImplemented, 3: StatusMissing, 4: StatusImplemented}
	if len(skills) != len(want) {
		t.Fatalf("exported %d skills, want %d", len(skills), len(want))
	}
	for _, s := range skills {
		if s.Status != want[s.ID] {
			t.Errorf("%s status %s, want %s", s.Codename, s.Status, want[s.ID])
		}
	}
}

/*
================
TestItemRarityNamesTheSeal

A sealed equipment's class within its degree names the seal; unsealed
equipment is normal and other items have no rarity.
================
*/
func TestItemRarityNamesTheSeal(t *testing.T) {
	ref := func(rarity, class float64, ids [4]int64) *enterworld.ItemRef {
		return &enterworld.ItemRef{TypeIDs: ids, NativeFields: enterworld.NewNativeFields(map[string]float64{"rarity": rarity, "itemClass": class})}
	}
	sword := [4]int64{3, 1, 6, 2}
	for _, tc := range []struct {
		rarity, class float64
		want          string
	}{{0, 4, "normal"}, {2, 4, "star"}, {2, 5, "moon"}, {2, 6, "sun"}, {2, 30, "sun"}} {
		if got := itemRarity(ref(tc.rarity, tc.class, sword)); got != tc.want {
			t.Errorf("rarity %v class %v -> %q, want %q", tc.rarity, tc.class, got, tc.want)
		}
	}
	if got := itemRarity(ref(2, 4, [4]int64{3, 3, 1, 1})); got != "" {
		t.Errorf("a potion has rarity %q", got)
	}
	if got := itemDegree(ref(0, 7, [4]int64{3, 3, 11, 5})); got != 7 {
		t.Errorf("an element of class 7 has degree %d", got)
	}
}

/*
================
TestSchemaCarriesFieldDocs

A doc tag ships as the property's description, so the site reads the
rule beside the data.
================
*/
func TestSchemaCarriesFieldDocs(t *testing.T) {
	schema := Schema("alchemy.json", AlchemyFile{})
	rules := schema["properties"].(map[string]any)["rules"].(map[string]any)["properties"].(map[string]any)
	if doc, _ := rules["minChance"].(map[string]any)["description"].(string); !strings.Contains(doc, "clamped") {
		t.Fatalf("minChance description %q", doc)
	}
}
