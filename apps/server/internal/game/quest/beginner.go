/*
===========================================================================

beginner.go - the optional potion merchant's level 8-64 beginner track

Port-only, not native. One embedded content table also supplies the browser
journal and dialogue asset. Chinese and European routes have distinct IDs;
each character sees thirteen quests and may choose either country's weapon.
Native quests, creation seeds and rewards are unchanged when the flag is off.

===========================================================================
*/
package quest

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

const EnvBeginnerQuests = "SRO_BEGINNER_QUESTS"
const beginnerRouteStride = 100
const beginnerFirstRegionalLevel = 24
const beginnerHotanLevel = 42

//go:embed beginner_quests.json
var beginnerContentJSON string

/*
================
beginnerContent
================
*/
type beginnerContent struct {
	FirstRefID uint32              `json:"firstRefId"`
	Weapons    []string            `json:"weapons"`
	Milestones []beginnerMilestone `json:"milestones"`
}

/*
================
beginnerMilestone
================
*/
type beginnerMilestone struct {
	Level       uint8  `json:"level"`
	Degree      int    `json:"degree"`
	Kills       uint32 `json:"kills"`
	Exp         int64  `json:"exp"`
	SkillExp    int64  `json:"skillExp"`
	PotionGrade int    `json:"potionGrade"`
	Potions     uint32 `json:"potions"`
	CH          string `json:"ch"`
	EU          string `json:"eu"`
}

/*
================
BeginnerQuestsFromEnv

An unset switch keeps the native default. Invalid operator input refuses
definition loading instead of silently disabling an intended quest track.
================
*/
func BeginnerQuestsFromEnv() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBeginnerQuests))) {
	case "", "off", "0", "false":
		return false, nil
	case "on", "1", "true":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be on or off", EnvBeginnerQuests)
	}
}

/*
================
beginnerQuestContent

The JSON is compiled data, shared with the asset publisher. Quest mechanics
continue through the ordinary validated loader and transaction owners.
================
*/
func beginnerQuestContent() (beginnerContent, error) {
	var content beginnerContent
	if err := json.Unmarshal([]byte(beginnerContentJSON), &content); err != nil {
		return content, fmt.Errorf("beginner quest content: %w", err)
	}
	return content, nil
}

/*
================
beginnerQuestRows

Country-specific IDs make completion durable across travel and reconnects.
There is no prerequisite on optional support quests or earlier milestones.
================
*/
func beginnerQuestRows(content beginnerContent) []CatalogRow {
	rows := make([]CatalogRow, 0, len(content.Milestones)*2)
	for route, race := range []string{"CH", "EU"} {
		for _, milestone := range content.Milestones {
			code := fmt.Sprintf("QPORT_BEGINNER_%s_%02d", race, milestone.Level)
			rows = append(rows, CatalogRow{
				ID:       content.FirstRefID + uint32(route*beginnerRouteStride) + uint32(milestone.Level),
				Codename: code, Level: milestone.Level, TitleSymbol: "SN_" + code,
			})
		}
	}
	return rows
}

/*
================
addBeginnerRows

The lazy catalog owner calls this before publishing its maps. Custom IDs
must never replace a native row, including an unrelated ID collision.
================
*/
func (c *Catalog) addBeginnerRows() error {
	content, err := beginnerQuestContent()
	if err != nil {
		return err
	}
	for _, row := range beginnerQuestRows(content) {
		if _, exists := c.rows[row.Codename]; exists {
			return fmt.Errorf("duplicate beginner quest %s", row.Codename)
		}
		if _, exists := c.rowsByID[row.ID]; exists {
			return fmt.Errorf("duplicate beginner quest id %d", row.ID)
		}
		country := uint8(0)
		if strings.Contains(row.Codename, "_EU_") {
			country = 1
		}
		c.rows[row.Codename] = row
		c.rowsByID[row.ID] = row.Codename
		c.contents[row.Codename] = ContentsRow{Codename: row.Codename, CountryByte: country, GiveupWarnByte: 1, ContentsSymbols: []string{"SN_CON_" + row.Codename}}
		c.loadedRows++
	}
	return nil
}

/*
================
beginnerQuestSpecs

Both countries' fourteen weapon families remain available. Seal and degree
validation reads the actual item reference rather than trusting its suffix.
================
*/
func beginnerQuestSpecs(items enterworld.ItemRefSource) ([]QuestSpec, error) {
	content, err := beginnerQuestContent()
	if err != nil {
		return nil, err
	}
	if items == nil {
		return nil, fmt.Errorf("beginner quests require item references")
	}
	rows := beginnerQuestRows(content)
	specs := make([]QuestSpec, 0, len(rows))
	for i, row := range rows {
		m := content.Milestones[i%len(content.Milestones)]
		european := i >= len(content.Milestones)
		npc, target := "NPC_CH_POTION", m.CH
		if european {
			npc, target = "NPC_EU_POTION", m.EU
		}
		if row.Level >= beginnerFirstRegionalLevel {
			npc = "NPC_WC_POTION"
			if european {
				npc = "NPC_CA_POTION"
			}
		}
		if row.Level >= beginnerHotanLevel {
			npc = "NPC_KT_POTION"
		}
		spec := QuestSpec{
			Codename: row.Codename, KindByte: 1, MinLevel: row.Level, MaxCompletions: 1,
			// Both regular and field-clone identities exist in primary media.
			Objective: ObjectiveKill, KillMonsterCodenames: []string{target, target + "_CLON"}, KillCount: m.Kills,
			StartNpcCodename: npc, EndNpcCodename: npc,
			OfferPromptSymbol:    "SN_TALK_" + row.Codename + "_OFFER",
			CompletePromptSymbol: "SN_TALK_" + row.Codename + "_COMPLETE",
			NotAchievedSymbol:    "SN_TALK_" + row.Codename + "_PROGRESS",
			InventoryFullSymbol:  "SN_TALK_" + row.Codename + "_FULL",
			RewardExp:            m.Exp, RewardSkillExp: m.SkillExp, BoundRewards: true,
		}
		if m.Degree > 0 {
			for _, family := range content.Weapons {
				code := fmt.Sprintf("ITEM_%s_%02d_B_RARE", family, m.Degree)
				ref, exists := items.ItemRefByCodename(code)
				tier, sealed := enterworld.EquipmentSeal(ref)
				if !exists || !sealed || tier != enterworld.SealMoon || enterworld.EquipmentDegree(ref) != m.Degree || ref.TypeIDs[2] != 6 {
					return nil, fmt.Errorf("beginner quest %s requires a degree %d Moon weapon: %s", row.Codename, m.Degree, code)
				}
				spec.RewardChoices = append(spec.RewardChoices, RewardChoice{
					TitleSymbol:          fmt.Sprintf("SN_PORT_BEGINNER_%s_%d", family, m.Degree),
					CompletePromptSymbol: fmt.Sprintf("SN_PORT_BEGINNER_CONFIRM_%s_%d", family, m.Degree),
					Items:                []RewardItemLead{{ItemCodename: code, Count: 1}},
				})
			}
		} else {
			for _, kind := range []string{"HP", "MP"} {
				spec.RewardItems = append(spec.RewardItems, RewardItemLead{ItemCodename: fmt.Sprintf("ITEM_ETC_%s_POTION_%02d", kind, m.PotionGrade), Count: m.Potions})
			}
		}
		specs = append(specs, spec)
	}
	return specs, nil
}
