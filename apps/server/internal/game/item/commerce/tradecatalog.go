/*
===========================================================================

tradecatalog.go - imported merchant quotations joined by native reference IDs

===========================================================================
*/
package commerce

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
)

// TradeQuotationsFile is the merchant quotation table in the projection's
// derived data (gamedata DerivedDir), imported from the operator's own
// source-data. Without it every trade good sells at its base sell price,
// the native path for a good with no quotation entry (tradesale.go).
const TradeQuotationsFile = "commerce/item-quotations.json"

/*
================
loadTradeCatalog

The trade quests named in questdata; quotations load separately
(LoadTradeQuotations).
================
*/
func (c *Catalog) loadTradeCatalog(dir string) error {
	c.TradeQuotations = map[[2]uint32]TradeQuotation{}
	c.TradeQuests = map[string]uint32{}
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(dir, "questdata.txt")) {
		if len(row) < 3 || row[0] != "1" {
			continue
		}
		id, err := strconv.ParseUint(row[1], 10, 32)
		if err == nil && id != 0 {
			c.TradeQuests[row[2]] = uint32(id)
		}
	}
	return nil
}

/*
================
LoadTradeQuotations

The quotation table from derivedDir, or false when there is none (an empty
dir or an absent file). All shipped rows have equal lower/upper rates:
stock writes cannot change their price, so the port needs no second
persistent stock authority. A variable-rate dataset is rejected instead of
silently dropping that state. Publishes only a fully admitted table.
================
*/
func (c *Catalog) LoadTradeQuotations(derivedDir string) (bool, error) {
	if derivedDir == "" {
		return false, nil
	}
	data, err := os.ReadFile(filepath.Join(derivedDir, filepath.FromSlash(TradeQuotationsFile)))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("commerce: %s: %w", TradeQuotationsFile, err)
	}
	quotations, err := parseTradeQuotations(data)
	if err != nil {
		return false, err
	}
	c.TradeQuotations = quotations
	return true, nil
}

/*
================
parseTradeQuotations
================
*/
func parseTradeQuotations(data []byte) (map[[2]uint32]TradeQuotation, error) {
	var document struct {
		Rows []struct {
			TradeQuotation
			NPC, Item uint32
		}
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	quotations := make(map[[2]uint32]TradeQuotation, len(document.Rows))
	for _, row := range document.Rows {
		if row.NPC == 0 || row.Item == 0 || row.Lower != row.Upper {
			return nil, fmt.Errorf("commerce: quotation requires variable stock authority")
		}
		if _, err := TradeQuotationPrice(1, row.TradeQuotation); err != nil {
			return nil, err
		}
		key := [2]uint32{row.NPC, row.Item}
		if _, exists := quotations[key]; exists {
			return nil, fmt.Errorf("commerce: duplicate trade quotation")
		}
		quotations[key] = row.TradeQuotation
	}
	return quotations, nil
}

/*
================
RequiredTradeQuest

CGObjNPC_RequiredTradeQuest (4C9110), exact codename comparisons.
================
*/
func RequiredTradeQuest(npc string) string {
	switch npc {
	case "NPC_CH_SPECIAL2":
		return "QNO_TRADE_CH_SPECIAL2_1"
	case "NPC_WC_SPECIAL2":
		return "QNO_TRADE_WC_SPECIAL2_1"
	case "NPC_TK_SPECIAL":
		return "QNO_TRADE_TK_SPECIAL_1"
	case "NPC_RM_SPECIAL":
		return "QNO_TRADE_RM_SPECIAL_1"
	case "NPC_AM_SPECIAL":
		return "QNO_TRADE_AM_SPECIAL_1"
	default:
		return ""
	}
}
