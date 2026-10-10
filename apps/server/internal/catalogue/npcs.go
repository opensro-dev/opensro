/*
===========================================================================

npcs.go - C5 npcs.json: every NPC the world places, its services and shop

The rows are the server's NPC roster (simulation.LoadNpcWorldRoster): the
npcpos positions joined with the reference, chat, shop and service data.
Services are the roster's option set (npcservice.go), the same bits the
talk handlers check. A shop's goods are the server's commerce catalogue
(commerce.Load), with the price and currency the buy path charges. Tab
labels are client text and are filled by the Node stage.

===========================================================================
*/
package catalogue

import (
	"sort"
	"strings"

	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/world/simulation"
)

// npcServiceNames are the service options the website names, keyed by the
// server's own option constants.
var npcServiceNames = map[uint8]string{
	simulation.NpcServiceShop:             "shop",
	simulation.NpcServiceTalk:             "talk",
	simulation.NpcServiceStorage:          "storage",
	simulation.NpcServiceRepair:           "repair",
	simulation.NpcServiceRecallPoint:      "recall-point",
	simulation.NpcServiceTeleport:         "teleport",
	simulation.NpcServiceResurrectPoint:   "resurrect-point",
	simulation.NpcServiceStable:           "stable",
	simulation.NpcServiceSpecialTrade:     "special-trade",
	simulation.NpcServiceThiefBuy:         "thief-buy",
	simulation.NpcServiceGeneral:          "general",
	simulation.NpcServiceGuild:            "guild",
	simulation.NpcServiceGachaMachine:     "gacha-machine",
	simulation.NpcServiceGachaOperator:    "gacha-operator",
	simulation.NpcServiceJobTrader:        "job-trader",
	simulation.NpcServiceJobThief:         "job-thief",
	simulation.NpcServiceJobHunter:        "job-hunter",
	simulation.NpcServiceFortressManager:  "fortress-manager",
	simulation.NpcServiceFortressOfficial: "fortress-official",
	simulation.NpcServiceFortressAide:     "fortress-aide",
	simulation.NpcServiceFortressSmith:    "fortress-smith",
	simulation.NpcServiceFortressTrainer:  "fortress-trainer",
	simulation.NpcServiceTeleportGate:     "teleport-gate",
	simulation.NpcServiceReverseReturn:    "reverse-return",
	simulation.NpcServiceGatePulley:       "gate-pulley",
	simulation.NpcServiceMagicOption:      "magic-option",
	simulation.NpcServiceArenaManager:     "arena-manager",
	simulation.NpcServiceArenaExchanger:   "arena-exchanger",
	simulation.NpcServiceOpenMarket:       "open-market",
	simulation.NpcServiceRecallParty:      "recall-party",
	simulation.NpcServiceSiegeTeleport:    "siege-teleport",
	simulation.NpcServiceInstanceExit:     "instance-exit",
}

var currencyEnum = []string{"gold", "honor"}

/*
================
NpcGood
================
*/
type NpcGood struct {
	ItemID   uint32 `json:"itemId"`
	Price    uint64 `json:"price"`
	Currency string `json:"currency" enum:"currency"`
	// Stack is the count one purchase gives, when more than one.
	Stack uint16 `json:"stack,omitempty"`
	Plus  uint8  `json:"plus,omitempty"`
}

/*
================
NpcTab
================
*/
type NpcTab struct {
	ID int32 `json:"id"`
	// Label is the tab's name; the Node stage fills it from LabelSymbol.
	Label       string    `json:"label"`
	LabelSymbol string    `json:"labelSymbol"`
	Goods       []NpcGood `json:"goods"`
}

/*
================
NpcPosition
================
*/
type NpcPosition struct {
	Region uint16  `json:"region"`
	X      float64 `json:"x"`
	Z      float64 `json:"z"`
}

/*
================
Npc

One NPC reference, with every position the roster places it at.
================
*/
type Npc struct {
	ID        uint32        `json:"id"`
	Codename  string        `json:"codename"`
	Name      string        `json:"name"`
	Model     string        `json:"model,omitempty"`
	Positions []NpcPosition `json:"positions"`
	Services  []string      `json:"services"`
	Tabs      []NpcTab      `json:"tabs"`
	Standing
}

/*
================
NpcsFile
================
*/
type NpcsFile struct {
	Header
	Npcs []Npc `json:"npcs"`
}

/*
================
NpcSource
================
*/
type NpcSource struct {
	Roster []simulation.NpcDef
	Shops  *commerce.Catalog
}

/*
================
BuildNpcs

One row per NPC reference, in id order. Teleport gates are structures the
atlas draws, not NPCs, and are left out.
================
*/
func BuildNpcs(source NpcSource) []Npc {
	byID := map[uint32]*Npc{}
	for _, def := range source.Roster {
		if def.Teleport != nil {
			continue
		}
		npc := byID[def.RefObjID]
		if npc == nil {
			npc = &Npc{ID: def.RefObjID, Codename: def.Codename, Name: def.Name,
				Model:    strings.ToLower(strings.ReplaceAll(def.ModelPath, "\\", "/")),
				Services: npcServices(def.Services), Tabs: npcTabs(def.NpcTalkStoreGroups, source.Shops),
				Positions: []NpcPosition{}, Standing: Standing{Status: StatusImplemented}}
			if npc.Name == "" {
				npc.Name = def.Codename
			}
			npc.Standing = applyGaps("npc", npc.ID, npc.Standing)
			byID[def.RefObjID] = npc
		}
		if def.AuthoredSpawn {
			npc.Positions = append(npc.Positions, NpcPosition{Region: def.Spawn.RegionID, X: def.Spawn.X, Z: def.Spawn.Z})
		}
	}
	npcs := make([]Npc, 0, len(byID))
	for _, npc := range byID {
		npcs = append(npcs, *npc)
	}
	sort.Slice(npcs, func(i, j int) bool { return npcs[i].ID < npcs[j].ID })
	return npcs
}

/*
================
npcServices

The named options of a service set, in option order.
================
*/
func npcServices(services simulation.NpcServices) []string {
	var options []uint8
	for option := range npcServiceNames {
		if services.Has(option) {
			options = append(options, option)
		}
	}
	sort.Slice(options, func(i, j int) bool { return options[i] < options[j] })
	out := []string{}
	for _, option := range options {
		out = append(out, npcServiceNames[option])
	}
	return out
}

/*
================
npcTabs

The NPC's shop tabs in its store groups' order, each with the goods the
commerce catalogue sells there.
================
*/
func npcTabs(groups []simulation.NpcTalkStoreGroup, shops *commerce.Catalog) []NpcTab {
	tabs := []NpcTab{}
	if shops == nil {
		return tabs
	}
	for _, group := range groups {
		for _, tab := range group.Tabs {
			out := NpcTab{ID: tab.TabID, LabelSymbol: tab.LabelSymbol, Goods: []NpcGood{}}
			for _, offer := range shops.Tabs[tab.TabID] {
				if offer.Ref == nil {
					continue
				}
				good := NpcGood{ItemID: offer.Ref.RefObjID, Price: offer.Price, Currency: currencyName(offer.Currency)}
				if offer.Stack > 1 {
					good.Stack = offer.Stack
				}
				if len(offer.Contents) == 1 {
					good.Plus = offer.Contents[0].Plus
				}
				out.Goods = append(out.Goods, good)
			}
			tabs = append(tabs, out)
		}
	}
	return tabs
}

/*
================
currencyName
================
*/
func currencyName(currency uint8) string {
	if currency == commerce.PaymentHonor {
		return "honor"
	}
	return "gold"
}
