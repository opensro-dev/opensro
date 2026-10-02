/*
===========================================================================

definitions.go - validated version-local quest contracts

Shipped media owns identity and advertised rewards. Compiled native scripts
supply mechanics and NPC relationships; all references resolve before the
runtime can accept a quest. Research files are never loaded by the server.

===========================================================================
*/
package quest

// Version-aware quest definitions. v1.150 questdata/questcontentsdata and
// SN_CON/SN_PAYCON strings own display identity, targets' visible counts and
// advertised rewards. The newer server's compiled Lua scripts supply NPC and
// target-codename leads. Delivery quantities retain explicitly identified
// newer-script provenance where primary media provides no numeric count;
// numeric IDs are always resolved against v1.150.
//
// Implemented mechanics: talk, inventory collection, fatal-hit kill counters,
// atomic inventory/EXP/SP/gold rewards. Unported script mechanics remain explicit.

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
ObjectiveKind

Each kind has one progression owner and an explicit completion predicate.
================
*/
type ObjectiveKind int

const (
	// ObjectiveTalk has no counter and completes at an authored NPC conversation.
	// Delivery uses ObjectiveCollect and a separate item-producing NPC.
	ObjectiveTalk ObjectiveKind = iota
	// ObjectiveCollect is an inventory-collection objective: progress =
	// min(required, total held of ItemCodename), recomputed on every
	// inventory gain/loss through the action hook (runtime.go).
	ObjectiveCollect
	ObjectiveKill
	// ObjectiveParallel requires every authored mission; each has independent
	// persisted progress. It is not a sequence of separately rewarded stages.
	ObjectiveParallel
	// Delivery items are granted on acceptance and surrendered at the NPC.
	// Possession alone does not complete the displayed conversation objective.
	ObjectiveDelivery
)

/*
================
RewardItemLead

The inventory owner must admit the entire reward before scalar rewards commit.
================
*/
type RewardItemLead struct {
	ItemCodename string
	Count        uint32
}

/*
================
QuestSpec

Authored mechanics keyed by codename. Definition resolves media-owned fields.
================
*/
type QuestSpec struct {
	TimeLimitMinutes uint8
	TimeoutSymbol    string
	DayOrNight       uint8
	PeriodStartLimit uint32
	DeliveryItems    []RewardItemLead
	TravelBlockMask  uint32
	Objectives       []MissionSpec
	// Blocks new acceptance when the native prerequisite branch is unresolved.
	// Existing active quests keep their objective and reward contract.
	AcceptanceUnavailable string
	// CompletedBy names the quest whose completion also completes this one:
	// the last quest of a superseded chain that covered the same route. A
	// character who finished that chain is neither offered this quest again
	// nor shown it as undone.
	CompletedBy          []string
	MaxCompletions       uint32
	Stages               []QuestStage
	MonsterDrop          *MonsterDropRule
	Codename             string
	RequiredQuests       []string
	RequiredActiveQuests []string
	Repeatable           bool
	// KindByte is the wire u10 the CIFQuestReward content button
	// switches on (sub_5c26e0): 1/7/8 open the give-up window, 2 opens
	// the REWARD window (its action button composes 0x729A - the
	// turn-in this server can actually receive). DECLARED CHOICE
	// (grade D): retail's kind assignment never shipped; policy here is
	// kind 2 for collect quests (completable through the reward window)
	// and kind 1 for talk quests (give-up only until an NPC-talk plane
	// exists).
	KindByte uint8
	// Objective is the authored objective (grade D mechanics anchored
	// on the grade-A shipped display strings; counts grade B/C where a
	// shard row carries them).
	Objective ObjectiveKind
	// CollectItemCodename/CollectCount apply to ObjectiveCollect only.
	CollectItemCodename string
	CollectCount        uint32
	// Kill targets are codenames: v1.188 numeric RefObj IDs are not portable.
	KillMonsterCodenames []string
	KillRanks            []uint8
	KillCount            uint32
	// RewardExp/RewardGold follow the v1.150 advertised reward strings.
	// Unlabeled SQL word order is not evidence of EXP vs gold semantics.
	RewardExp      int64
	RewardGold     int64
	RewardSkillExp int64
	// RewardItems are committed atomically through the inventory owner.
	RewardItems []RewardItemLead
	// NPC/session fields are codename/symbol keyed because their numeric IDs
	// are version-local. They are curated only where shipped dialogue text
	// and the v1.188 mechanism establish a complete interaction segment.
	StartNpcCodename        string
	EndNpcCodename          string
	OfferPromptSymbol       string
	RepeatOfferPromptSymbol string
	AcceptResponseSymbol    string
	DenyResponseSymbol      string
	NotAchievedSymbol       string
	CompletePromptSymbol    string
	InventoryFullSymbol     string
	// AchievedNowSymbol is BASIC_MENUSTRING_ACHIEVED_NOW: the banner sent
	// when the objective first stands complete ("... report to <NPC>").
	// Script-backed quests take it from achieved_now_generated.json.
	AchievedNowSymbol    string
	DeliveryNpcCodename  string
	DeliveryPromptSymbol string
}

// curatedQuestSpecs is the curated table. SMALL BY DESIGN: the starter
// loop the prerequisites serve, every row evidence-graded above. Growing
// it is appending a spec whose codename resolves in the shipped
// questdata - LoadDefinitions fails loud on anything else.
var curatedQuestSpecs = append(append(starterQuestSpecs, banditChainSpecs...), collectionQuestSpecs...)

var starterQuestSpecs = []QuestSpec{
	{
		// Shipped EU starter quest 143. The display row, chain edge and
		// dialogue symbols are v1.150 media; ordinary NPC choices ride the
		// v1.188 Lua/session mechanism reconstructed through 0x3773.
		Codename:              "QNO_EU_TUTORIAL_1",
		AcceptanceUnavailable: qnoTutorialSuperseded,
		RewardExp:             60,
		KindByte:              1,
		Objective:             ObjectiveTalk,
		StartNpcCodename:      "NPC_EU_ADVICE",
		EndNpcCodename:        "NPC_EU_ADVICE",
		OfferPromptSymbol:     "SN_TALK_QNO_EU_TUTORIAL_1_01",
		CompletePromptSymbol:  "SN_TALK_QNO_EU_TUTORIAL_1_04",
	},
	{
		// The Chinese tutorial (shipped id 2). Creation-seeded (the
		// _RefCharDefault_Quest race-0 row, grade B - seed.go). The
		// shipped objective string is "Talk to Sonhyeon"
		// (SN_CON_QTUTORIAL_CH_01, grade A): a talk objective with no
		// counter. No _RefQuestReward row joins (grade B negative):
		// reward none. Kind 1: give-up-able, not reward-window
		// completable (grade D policy above).
		Codename:             "QTUTORIAL_CH",
		StartNpcCodename:     "NPC_CH_GENARAL",
		EndNpcCodename:       "NPC_CH_GENARAL",
		OfferPromptSymbol:    "SN_TALK_QTUTORIAL_CH_01",
		CompletePromptSymbol: "SN_TALK_QTUTORIAL_CH_35",
		Stages:               chineseTutorialStages(),
		KindByte:             1,
		Objective:            ObjectiveTalk,
	},
	{
		// The tutorial chain's next quest (shipped id 3; the
		// questcontentsdata col-3 chain names it from QTUTORIAL_CH,
		// grade A). "Send Weapon List" (SN_CON_QNO_CH_SMITH_1): a
		// delivery/talk objective. No shard reward row joins.
		Codename: "QNO_CH_SMITH_1", RequiredQuests: []string{"QTUTORIAL_CH"}, KindByte: 1, Objective: ObjectiveCollect,
		CollectItemCodename: "ITEM_QNO_CH_SMITH_1", CollectCount: 1,
		RewardExp: 225, RewardGold: 205,
		StartNpcCodename: "NPC_CH_SMITH", EndNpcCodename: "NPC_CH_SMITH",
		DeliveryNpcCodename: "NPC_CH_SOLDIER_SO1", DeliveryPromptSymbol: "SN_TALK_QNO_CH_SMITH_1_06",
		OfferPromptSymbol: "SN_TALK_QNO_CH_SMITH_1_01", CompletePromptSymbol: "SN_TALK_QNO_CH_SMITH_1_08",
		InventoryFullSymbol: "SN_TALK_QNO_CH_SMITH_1_05",
	},
	{
		// Missing child: v1.150 requires one child's shoe and advertises
		// 375 EXP / 475 gold. Newer Quest.sct supplies the two monster
		// codenames and 20-percent ground-drop chance, not numeric IDs.
		Codename: "QNO_CH_CHEF_1", KindByte: 1, Objective: ObjectiveCollect,
		CollectItemCodename: "ITEM_QNO_CH_CHEF_1", CollectCount: 1,
		MonsterDrop:      &MonsterDropRule{MonsterCodenames: []string{"MOB_CH_BIGEYEGHOST", "MOB_CH_BIGEYEGHOST_CLON"}, ChancePercent: 20},
		StartNpcCodename: "NPC_CH_CHEF", EndNpcCodename: "NPC_CH_CHEF", OfferPromptSymbol: "SN_TALK_QNO_CH_CHEF_1_01", CompletePromptSymbol: "SN_TALK_QNO_CH_CHEF_1_05",
		RewardExp:  375,
		RewardGold: 475,
	},
	{
		// v1.150 item rows: _01=Resuscitation Potion, _02=Cursed Heart.
		// SN_PAYCON and SN_TALK explicitly specify a repeatable 10:1 exchange.
		Codename: "QSP_ALL_POTION_1", KindByte: 2, Objective: ObjectiveCollect, Repeatable: true,
		CollectItemCodename: "ITEM_QSP_ALL_POTION_1_02", CollectCount: 10,
		MonsterDrop:      &MonsterDropRule{AnyMonster: true, ChancePercent: 5, MinPlayerLevel: 20, MaxHeld: 300},
		RewardItems:      []RewardItemLead{{ItemCodename: "ITEM_QSP_ALL_POTION_1_01", Count: 1}},
		StartNpcCodename: "NPC_CH_POTION", EndNpcCodename: "NPC_CH_POTION",
		OfferPromptSymbol: "SN_TALK_QSP_ALL_POTION_1_01", CompletePromptSymbol: "SN_TALK_QSP_ALL_POTION_1_04",
		InventoryFullSymbol: "SN_TALK_QSP_ALL_POTION_1_03",
	},
	{
		// v1.150 SN_CON/SN_PAYCON own 40 kills, 1900 EXP, 1000 gold.
		// Newer Quest.sct @0x17b09 supplies target/NPC mechanism only;
		// its 14 kills and 2752 EXP are explicitly NOT imported.
		Codename: "QNO_CH_SOLDIER_EA1_1", KindByte: 1, Objective: ObjectiveKill,
		KillMonsterCodenames: []string{"MOB_CH_GYO", "MOB_CH_GYO_CLON"}, KillCount: 40,
		RewardExp: 1900, RewardGold: 1000,
		StartNpcCodename: "NPC_CH_SOLDIER_EA1", EndNpcCodename: "NPC_CH_SOLDIER_EA1",
		OfferPromptSymbol: "SN_TALK_QNO_CH_SOLDIER_EA1_1_01", CompletePromptSymbol: "SN_TALK_QNO_CH_SOLDIER_EA1_1_05",
	},
	{
		// v1.150 advertises 50 Water Ghosts, 3800 EXP and 28 HP herbs.
		// Newer Quest.sct's 15 targets / 6160 EXP are not imported.
		Codename: "QNO_CH_POTION_1", KindByte: 1, Objective: ObjectiveKill,
		KillMonsterCodenames: []string{"MOB_CH_WATERGHOST", "MOB_CH_WATERGHOST_CLON"}, KillCount: 50,
		RewardExp: 3800, RewardItems: []RewardItemLead{{ItemCodename: "ITEM_ETC_HP_POTION_01", Count: 28}},
		StartNpcCodename: "NPC_CH_POTION", EndNpcCodename: "NPC_CH_POTION",
		OfferPromptSymbol: "SN_TALK_QNO_CH_POTION_1_01", CompletePromptSymbol: "SN_TALK_QNO_CH_POTION_1_06",
		InventoryFullSymbol: "SN_TALK_QNO_CH_POTION_1_05",
	},
	{
		// Same cross-version discipline: v1.150 requires 40, not 20 kills.
		Codename: "QNO_CH_SOLDIER_EA2_1", KindByte: 1, Objective: ObjectiveKill,
		KillMonsterCodenames: []string{"MOB_CH_STONEGHOST", "MOB_CH_STONEGHOST_CLON"}, KillCount: 40,
		RewardExp: 4500, RewardGold: 1900,
		StartNpcCodename: "NPC_CH_SOLDIER_EA2", EndNpcCodename: "NPC_CH_SOLDIER_EA2",
		OfferPromptSymbol: "SN_TALK_QNO_CH_SOLDIER_EA2_1_01", CompletePromptSymbol: "SN_TALK_QNO_CH_SOLDIER_EA2_1_05",
	},
}

/*
================
Definition

Version-local references resolved from one authored spec and shipped media.
================
*/
type Definition struct {
	deliveryRefs         []uint32
	requiredEquippedItem string
	stageIndex           uint16
	// endNpcRef/deliveryNpcRef are the RefObjIDs the journal target list
	// names (ResolveJournalNpcs); zero until the world roster resolves them.
	endNpcRef      uint32
	deliveryNpcRef uint32
	missionIndex   uint8
	QuestSpec
	// RefID/Level resolve from the shipped questdata row (grade A).
	RefID uint32
	Level uint8
	// ContentsSymbol is the first shipped SN_CON_* symbol (grade A) -
	// the wire SQuestContents description the client resolves through
	// its textquest slice and %d-formats with the objective values.
	ContentsSymbol string
	// GiveupWarnByte/CountryByte/NextQuests are the questcontentsdata
	// join (grade A; display/chain context, carried for future planes).
	GiveupWarnByte uint8
	CountryByte    uint8
	NextQuests     []string
	// CollectItemRefID resolves CollectItemCodename against the shipped
	// itemdata (grade A id for the grade-D objective's item).
	CollectItemRefID       uint32
	TitleSymbol            string
	RequiredQuestIDs       []uint32
	CompletedByIDs         []uint32
	RequiredActiveQuestIDs []uint32
}

/*
================
Definitions

Stable source order also owns the one-byte NPC choice order.
================
*/
type Definitions struct {
	ordered    []*Definition
	byCodename map[string]*Definition
	byRefID    map[uint32]*Definition
	// Primary media IDs of predecessors whose execution is not ported yet.
	// They are still valid persisted completion IDs, never invented quests.
	externalPrerequisites map[uint32]bool
}

// LoadDefinitions resolves the curated table against the shipped
// catalogs. FAIL LOUD contract (the DefaultSkillRows posture): with a
// PRESENT questdata catalog, any curated codename that does not resolve
// - in questdata, questcontentsdata, or (for collect objectives) the
// shipped itemdata - errors with the codename named; the caller must
// refuse rather than run a short table. An ABSENT catalog (no media in
// this checkout) returns an EMPTY definition set instead: the
// TextdataSkills degradation - the server boots, the quest plane
// refuses every id, and the load already warned loud.
/*
================
LoadDefinitions

Present catalogs must resolve every contract. An absent catalog admits no
quests; a partially resolved catalog is an error, never a shortened rule set.
================
*/
func LoadDefinitions(catalog *Catalog, items enterworld.ItemRefSource) (*Definitions, error) {
	defs := &Definitions{
		byCodename:            map[string]*Definition{},
		byRefID:               map[uint32]*Definition{},
		externalPrerequisites: map[uint32]bool{},
	}
	if catalog.Len() == 0 {
		return defs, nil
	}
	specs, err := catalogSpecs(catalog)
	if err != nil {
		return nil, err
	}
	for _, spec := range specs {
		if spec.DayOrNight > 2 || spec.PeriodStartLimit > 0 && spec.DayOrNight == 0 || spec.DayOrNight > 0 && len(spec.Stages) > 0 {
			return nil, fmt.Errorf("quest %s has unsupported calendar conditions", spec.Codename)
		}
		if spec.TimeLimitMinutes > 0 && (spec.TimeoutSymbol == "" || len(spec.Stages) > 0) {
			return nil, fmt.Errorf("quest %s timed lifecycle lacks an authored abort contract", spec.Codename)
		}
		if spec.MaxCompletions > 15 {
			return nil, fmt.Errorf("quest %s repeat limit exceeds native title capacity", spec.Codename)
		}
		// A new enum value needs an objective owner and lifecycle acceptance;
		// it must never inherit the no-counter talk fallback by accident.
		if spec.Objective != ObjectiveTalk && spec.Objective != ObjectiveCollect && spec.Objective != ObjectiveKill && spec.Objective != ObjectiveParallel && spec.Objective != ObjectiveDelivery {
			return nil, fmt.Errorf("quest %s unsupported objective kind %d", spec.Codename, spec.Objective)
		}
		row, ok := catalog.QuestByCodename(spec.Codename)
		if !ok {
			return nil, fmt.Errorf("quest definitions: codename %s does not resolve in the shipped questdata (missing or renamed row - refusing to load a short table)", spec.Codename)
		}
		contents, ok := catalog.ContentsByCodename(spec.Codename)
		if !ok {
			return nil, fmt.Errorf("quest definitions: codename %s has no shipped questcontentsdata row - the wire contents symbol would be invented", spec.Codename)
		}
		if len(contents.ContentsSymbols) == 0 {
			return nil, fmt.Errorf("quest definitions: codename %s ships no SN_CON_* contents symbol - the wire contents node would be invented", spec.Codename)
		}
		def := &Definition{
			QuestSpec:      spec,
			RefID:          row.ID,
			Level:          row.Level,
			ContentsSymbol: contents.ContentsSymbols[0],
			GiveupWarnByte: contents.GiveupWarnByte,
			CountryByte:    contents.CountryByte,
			NextQuests:     contents.NextQuests,
			TitleSymbol:    row.TitleSymbol,
		}
		if spec.RewardExp < 0 || spec.RewardGold < 0 || spec.RewardSkillExp < 0 {
			return nil, fmt.Errorf("quest %s negative reward", spec.Codename)
		}
		if spec.DeliveryNpcCodename != "" && (spec.Objective != ObjectiveCollect || spec.DeliveryPromptSymbol == "" || spec.InventoryFullSymbol == "") {
			return nil, fmt.Errorf("quest %s incomplete delivery contract", spec.Codename)
		}
		for _, reward := range spec.RewardItems {
			if reward.Count == 0 || reward.Count > 65535 || items == nil {
				return nil, fmt.Errorf("quest %s invalid reward item contract", spec.Codename)
			}
			if ref, ok := items.ItemRefByCodename(reward.ItemCodename); !ok || ref == nil {
				return nil, fmt.Errorf("quest %s unresolved reward %s", spec.Codename, reward.ItemCodename)
			}
		}
		if spec.Objective == ObjectiveCollect {
			if spec.CollectItemCodename == "" || spec.CollectCount == 0 {
				return nil, fmt.Errorf("quest definitions: codename %s declares a collect objective without an item codename and count", spec.Codename)
			}
			if items == nil {
				return nil, fmt.Errorf("quest definitions: codename %s needs the shipped itemdata to resolve %s, but no ItemRefSource is wired", spec.Codename, spec.CollectItemCodename)
			}
			ref, ok := items.ItemRefByCodename(spec.CollectItemCodename)
			if !ok || ref == nil {
				return nil, fmt.Errorf("quest definitions: codename %s collect item %s does not resolve in the shipped itemdata", spec.Codename, spec.CollectItemCodename)
			}
			def.CollectItemRefID = ref.RefObjID
		}
		if spec.StartNpcCodename != "" {
			if def.TitleSymbol == "" || spec.OfferPromptSymbol == "" || spec.EndNpcCodename == "" || spec.CompletePromptSymbol == "" {
				return nil, fmt.Errorf("quest definitions: codename %s has an incomplete NPC-dialog contract", spec.Codename)
			}
		}
		if spec.TravelBlockMask & ^uint32(0x60000) != 0 {
			return nil, fmt.Errorf("quest %s unknown travel block bits", spec.Codename)
		}
		if err := validateKillRanks(spec); err != nil {
			return nil, err
		}
		if spec.Objective == ObjectiveKill && (spec.KillCount == 0 || len(spec.KillMonsterCodenames) == 0) {
			return nil, fmt.Errorf("quest definitions: %s has no kill target/count", spec.Codename)
		}
		if err := validateMonsterDrop(spec); err != nil {
			return nil, err
		}
		if spec.MonsterDrop != nil && spec.MonsterDrop.ItemCodename != "" {
			if items == nil {
				return nil, fmt.Errorf("quest %s requires a drop item catalog", spec.Codename)
			}
			if ref, exists := items.ItemRefByCodename(spec.MonsterDrop.ItemCodename); !exists || ref == nil {
				return nil, fmt.Errorf("quest %s has an unresolved tool drop", spec.Codename)
			}
		}
		if err := loadMissions(def, contents.ContentsSymbols, items); err != nil {
			return nil, err
		}
		if err := loadDelivery(def, items); err != nil {
			return nil, err
		}
		if err := loadStages(def, items); err != nil {
			return nil, err
		}

		if _, dup := defs.byCodename[spec.Codename]; dup {
			return nil, fmt.Errorf("quest definitions: codename %s is curated twice", spec.Codename)
		}
		defs.byCodename[spec.Codename] = def
		defs.byRefID[def.RefID] = def
		defs.ordered = append(defs.ordered, def)
	}
	for _, def := range defs.byCodename {
		for _, code := range def.RequiredActiveQuests {
			parent, ok := defs.byCodename[code]
			if !ok {
				return nil, fmt.Errorf("quest %s requires unavailable active quest %s", def.Codename, code)
			}
			def.RequiredActiveQuestIDs = append(def.RequiredActiveQuestIDs, parent.RefID)
		}
		for _, code := range def.CompletedBy {
			predecessor, ok := defs.byCodename[code]
			if !ok {
				return nil, fmt.Errorf("quest %s is completed by unavailable quest %s", def.Codename, code)
			}
			def.CompletedByIDs = append(def.CompletedByIDs, predecessor.RefID)
		}
		for _, code := range def.RequiredQuests {
			parent, ok := catalog.QuestByCodename(code)
			if !ok {
				return nil, fmt.Errorf("quest %s requires unavailable predecessor %s", def.Codename, code)
			}
			def.RequiredQuestIDs = append(def.RequiredQuestIDs, parent.ID)
			if _, loaded := defs.byCodename[code]; !loaded {
				defs.externalPrerequisites[parent.ID] = true
			}
		}
	}
	if err := validateQuestChains(defs); err != nil {
		return nil, err
	}
	return defs, nil
}

/*
================
ByCodename

Resolve the version-stable key used by authored scripts.
================
*/
func (d *Definitions) ByCodename(codename string) (*Definition, bool) {
	def, ok := d.byCodename[codename]
	return def, ok
}

/*
================
ByRefID

Resolve the version-local reference carried by native requests.
================
*/
func (d *Definitions) ByRefID(refID uint32) (*Definition, bool) {
	def, ok := d.byRefID[refID]
	return def, ok
}

/*
================
Len
================
*/
func (d *Definitions) Len() int {
	return len(d.byCodename)
}

/*
================
All

Return a detached list in the source order used by NPC choices.
================
*/
func (d *Definitions) All() []*Definition {
	return append([]*Definition(nil), d.ordered...)
}
