/*
===========================================================================

roc.go - the Roc range's database rewards (#3399 step 5b)

Port-only, not native: SRO_ROC's. The v1.150 loot catalogue has no rule
for the Roc kinds, because the v1.150 client lacks them. roc-rewards.txt
(scripts/roc_rewards.py) carries the rules the vSRO backup authors for
them, cut at build time to the items the variant installs, in the
cap110-rewards.txt shape. InitRoc joins it after InitCap110, so the Roc
rules ride beside the cap110 overlay's and change no rule either owns.

===========================================================================
*/

package loot

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const rocRewardsFile = "roc-rewards.txt"

/*
================
InitRoc

Startup only, once, after InitCap110 and before gameplay owners start. Off
leaves the catalogue as InitCap110 left it. On, a missing file means the
variant installs none of the rewards' items, so the Roc kinds drop what
the native passes roll; a file that is present must join whole.
================
*/
func InitRoc(textdataDir string, enabled bool) error {
	if !enabled {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, rocRewardsFile))
	if os.IsNotExist(err) {
		log.Printf("roc rewards: no %s; the Roc kinds roll only the native passes", rocRewardsFile)
		return nil
	}
	if err != nil {
		return fmt.Errorf("roc rewards: %w", err)
	}
	next, err := joinRewardOverlay("roc rewards", consumables, data)
	if err != nil {
		return err
	}
	consumables = next
	return nil
}
