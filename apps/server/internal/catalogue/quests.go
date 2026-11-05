/*
===========================================================================

quests.go - C3 quests.json: every quest the client lists, with its chain

The rows are questdata's display catalogue; the definitions are the
server's executable quests (quest.LoadDefinitions, the compiled-quest
pipeline). Status follows the quest audit (cmd/tools/quest-audit): a
listed quest with no definition is missing, one whose acceptance is
blocked is missing, one waiting on a predecessor the server lacks is
partial, and the rest are implemented. The chain is the prerequisites and
their reverse (what each quest unlocks), which IDEA-24 draws.

===========================================================================
*/
package catalogue

import (
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
)

// questIssue is where quest gaps are tracked.
const questIssue = "https://github.com/opensro-dev/opensro/issues?q=is%3Aissue+label%3Aquests"

/*
================
QuestNPC
================
*/
type QuestNPC struct {
	Codename string `json:"codename"`
	Name     string `json:"name"`
}

/*
================
QuestItem
================
*/
type QuestItem struct {
	ItemID uint32 `json:"itemId"`
	Count  uint32 `json:"count"`
}

/*
================
QuestRewards
================
*/
type QuestRewards struct {
	Exp      int64 `json:"exp"`
	SkillExp int64 `json:"skillExp"`
	// PaidExp is the EXP the beta growth actually pays for this quest at
	// its own level (progression.GrowthRates.PaidRewards); exp keeps the
	// native amount beside it. Absent when the beta is off or pays the
	// native amount, when exp alone is the truth.
	PaidExp int64 `json:"paidExp,omitempty" doc:"Port-only beta balance: the EXP actually paid for this quest at its own level after the beta growth band; exp keeps the native amount. Absent when the beta growth is off or pays the native amount."`
	// PaidSkillExp is PaidExp for skill EXP: the quest level's band, then
	// the flat skill EXP rate on top.
	PaidSkillExp int64         `json:"paidSkillExp,omitempty" doc:"Port-only beta balance: the skill EXP actually paid for this quest at its own level after the beta growth band and the flat skill EXP rate; skillExp keeps the native amount. Absent when the beta growth is off or pays the native amount."`
	Gold         int64         `json:"gold"`
	Items        []QuestItem   `json:"items"`
	Choices      [][]QuestItem `json:"choices,omitempty"`
	// BagSlots is the inventory slots the reward adds.
	BagSlots int `json:"bagSlots,omitempty"`
}

/*
================
Quest
================
*/
type Quest struct {
	ID         uint32       `json:"id"`
	Codename   string       `json:"codename"`
	Name       string       `json:"name"`
	Level      int          `json:"level"`
	MinLevel   int          `json:"minLevel,omitempty"`
	Giver      *QuestNPC    `json:"giver,omitempty"`
	Finisher   *QuestNPC    `json:"finisher,omitempty"`
	Requires   []uint32     `json:"requires"`
	Unlocks    []uint32     `json:"unlocks"`
	Repeatable bool         `json:"repeatable,omitempty"`
	Rewards    QuestRewards `json:"rewards"`
	Standing
}

/*
================
QuestsFile
================
*/
type QuestsFile struct {
	Header
	Quests []Quest `json:"quests"`
}

/*
================
QuestSource

The display catalogue, the executable definitions, the English text by
symbol, item ids by codename, and the level table the beta growth
prices a quest's paid rewards with.
================
*/
type QuestSource struct {
	Catalog     *quest.Catalog
	Definitions *quest.Definitions
	Text        func(symbol string) string
	ItemID      func(codename string) uint32
	// Levels prices the paid rewards (PaidRewards); nil prices every
	// pace band at the native pace, as a level without table rows does.
	Levels enterworld.LevelDataSource
	// Listed are the codenames questdata lists, in file order.
	Listed []string
}

/*
================
BuildQuests
================
*/
func BuildQuests(source QuestSource) []Quest {
	// The export describes the shard it is built for, so it reads the
	// same port-only beta growth the GameWorld grants with.
	beta := progression.BetaGrowthFromEnv()
	ids := map[string]uint32{}
	for _, codename := range source.Listed {
		if row, ok := source.Catalog.QuestByCodename(codename); ok {
			ids[codename] = row.ID
		}
	}
	quests := make([]Quest, 0, len(source.Listed))
	byID := map[uint32]int{}
	for _, codename := range source.Listed {
		row, ok := source.Catalog.QuestByCodename(codename)
		if !ok {
			continue
		}
		q := Quest{ID: row.ID, Codename: codename, Name: source.Text(row.TitleSymbol), Level: int(row.Level),
			Requires: []uint32{}, Unlocks: []uint32{}, Rewards: QuestRewards{Items: []QuestItem{}}}
		if q.Name == "" {
			q.Name = codename
		}
		def, defined := source.Definitions.ByCodename(codename)
		q.Standing = questStanding(def, defined, source.Definitions)
		if defined {
			q.MinLevel = int(def.MinLevel)
			q.Giver = questNPC(def.StartNpcCodename, source.Text)
			q.Finisher = questNPC(def.EndNpcCodename, source.Text)
			q.Repeatable = def.Repeatable
			for _, required := range def.RequiredQuests {
				if id, ok := ids[required]; ok {
					q.Requires = append(q.Requires, id)
				}
			}
			q.Rewards = questRewards(def, source.ItemID)
			// The website shows what the shard pays, not the native
			// reward (issue #2539): the beta growth priced at the
			// quest's own level by the same GrowthRates the grant
			// scales with. A band that pays the native amount leaves
			// the paid fields absent, so the page falls back to exp.
			if beta.Enabled {
				paidExp, paidSkillExp := beta.PaidRewards(source.Levels, int64(q.Level), q.Rewards.Exp, q.Rewards.SkillExp)
				if paidExp != q.Rewards.Exp {
					q.Rewards.PaidExp = paidExp
				}
				if paidSkillExp != q.Rewards.SkillExp {
					q.Rewards.PaidSkillExp = paidSkillExp
				}
			}
		}
		q.Standing = applyGaps("quest", q.ID, q.Standing)
		byID[q.ID] = len(quests)
		quests = append(quests, q)
	}
	for _, q := range quests {
		for _, required := range q.Requires {
			if i, ok := byID[required]; ok {
				quests[i].Unlocks = append(quests[i].Unlocks, q.ID)
			}
		}
	}
	sort.Slice(quests, func(i, j int) bool { return quests[i].ID < quests[j].ID })
	return quests
}

/*
================
questStanding

The quest audit's verdicts, in player words.
================
*/
func questStanding(def *quest.Definition, defined bool, defs *quest.Definitions) Standing {
	if !defined {
		return Standing{Status: StatusMissing, Gaps: []string{"This quest can't be taken yet."}, Issue: questIssue}
	}
	if def.AcceptanceUnavailable != "" {
		return Standing{Status: StatusMissing, Gaps: []string{"This quest is listed but can't be accepted yet."}, Issue: questIssue}
	}
	for _, required := range def.RequiredQuests {
		if _, ok := defs.ByCodename(required); !ok {
			return Standing{Status: StatusPartial, Gaps: []string{"It needs an earlier quest that isn't in the game yet."},
				Issue: questIssue}
		}
	}
	return Standing{Status: StatusImplemented}
}

/*
================
questNPC
================
*/
func questNPC(codename string, text func(string) string) *QuestNPC {
	if codename == "" {
		return nil
	}
	name := text("SN_" + codename)
	if name == "" {
		name = codename
	}
	return &QuestNPC{Codename: codename, Name: name}
}

/*
================
questRewards
================
*/
func questRewards(def *quest.Definition, itemID func(string) uint32) QuestRewards {
	items := func(leads []quest.RewardItemLead) []QuestItem {
		out := []QuestItem{}
		for _, lead := range leads {
			if id := itemID(lead.ItemCodename); id != 0 {
				out = append(out, QuestItem{ItemID: id, Count: lead.Count})
			}
		}
		return out
	}
	rewards := QuestRewards{Exp: def.RewardExp, SkillExp: def.RewardSkillExp, Gold: def.RewardGold,
		Items: items(def.RewardItems), BagSlots: int(def.RewardInventorySlots)}
	for _, choice := range def.RewardChoices {
		rewards.Choices = append(rewards.Choices, items(choice.Items))
	}
	return rewards
}

/*
================
QuestStandings
================
*/
func QuestStandings(quests []Quest) []Standing {
	out := make([]Standing, len(quests))
	for i, q := range quests {
		out[i] = q.Standing
	}
	return out
}
