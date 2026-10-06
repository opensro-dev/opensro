/*
===========================================================================

tradecatalog.go - imported merchant quotations joined by native reference IDs

===========================================================================
*/
package commerce

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
)

//go:embed .generated/item-quotations.json
var tradeQuotationData []byte

/*
================
loadTradeCatalog

All shipped quotation rows have equal lower/upper rates. Stock writes cannot
change their price, so the port needs no second persistent stock authority.
Reject a new variable-rate dataset instead of silently dropping that state.
================
*/
func (c *Catalog) loadTradeCatalog(dir string) error {
	var document struct {
		Rows []struct {
			TradeQuotation
			NPC, Item uint32
		}
	}
	if err := json.Unmarshal(tradeQuotationData, &document); err != nil {
		return err
	}
	c.TradeQuotations = make(map[[2]uint32]TradeQuotation, len(document.Rows))
	for _, row := range document.Rows {
		if row.NPC == 0 || row.Item == 0 || row.Lower != row.Upper {
			return fmt.Errorf("commerce: quotation requires variable stock authority")
		}
		if _, err := TradeQuotationPrice(1, row.TradeQuotation); err != nil {
			return err
		}
		key := [2]uint32{row.NPC, row.Item}
		if _, exists := c.TradeQuotations[key]; exists {
			return fmt.Errorf("commerce: duplicate trade quotation")
		}
		c.TradeQuotations[key] = row.TradeQuotation
	}
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
