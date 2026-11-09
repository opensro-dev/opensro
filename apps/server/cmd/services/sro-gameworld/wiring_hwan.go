/*
===========================================================================

wiring_hwan.go - the Blue Berserk quest knobs on the quest runtime

Port-only, not native: SRO_HWAN_NIYA_CAPTURE_RATES, SRO_HWAN_SUN_RELIC and
SRO_HWAN_3_FAST_DROP (quest/hwanknobs.go). Unset keeps the original
behaviour; a malformed value refuses the boot, like SRO_LEVEL_CAP.

===========================================================================
*/
package main

import (
	"bytes"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
applyHwanKnobs

Reads the three knobs into the runtime and says so when one differs from the
original.
================
*/
func applyHwanKnobs(quests *quest.Runtime) error {
	knobs, err := quest.HwanKnobsFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	quests.Hwan = knobs
	if knobs.NiyaRatesSet {
		log.Infof("quest: Niya trap capture rates nothing,flutter,normal = %v (%s)", knobs.NiyaRates, quest.EnvHwanNiyaCaptureRates)
	}
	if knobs.SunRelicSet {
		log.Infof("quest: Sun God relic dig chance %d%% (%s)", knobs.SunRelicPercent, quest.EnvHwanSunRelic)
	}
	if _, hwan := quests.Defs.ByCodename("QNO_CH_HWAN_1_3"); hwan && !monster.QinShiTombFromEnv() {
		log.Warnf("quest: the Blue Berserk bell quest (QNO_*_HWAN_1_3) drops from the Ch'in Tomb stone ghosts, and %s is off: the 95 chain cannot pass its third step", monster.EnvQinShiTomb)
	}
	if knobs.FastDrop {
		log.Infof("quest: Alexandria ostracon steps drop %d items per kill (%s)", quest.FastDropCount, quest.EnvHwan3FastDrop)
	}
	return nil
}

// hwanReverseMapPoints are the reverse return scroll's extra map destinations of the Blue Berserk quest line
// (owner, 2026-11-10): the homes of the two ritual NPCs, whom the chains send the player to again and again. Their
// native spawn rows are npcpos.txt 2017 and 7505. The Exorcist stands inside her house, where the arrival rescue
// would move a player anyway, so her point is the open ground just outside it. They join the table only with
// SRO_REVERSE_RETURN_MAP on and the quest rows built in (SRO_CAP110_QUESTS).
var hwanReverseMapPoints = []enterworld.ReverseMapPoint{
	{Name: "Exorcist's Home", RegionID: 25253, X: 150, Y: 229.05, Z: 830},      // near NPC_CH_SHAMAN, Exorcist Miaoryeong, by Jangan
	{Name: "Sunset Witch", RegionID: 27728, X: 1835.89, Y: 743.89, Z: 1588.89}, // NPC_EU_WITCH, by Constantinople
}

/*
================
hwanQuestsBuilt

Whether the shard's game data carries the Blue Berserk quest rows (the SRO_CAP110_QUESTS build). Read
from questdata.txt directly because the reverse return table is configured, and published to the browser,
before the quest definitions are loaded.
================
*/
func hwanQuestsBuilt(textdataDir string) bool {
	data, err := os.ReadFile(filepath.Join(textdataDir, "questdata.txt"))
	if err != nil {
		return false
	}
	utf16 := []byte{'Q', 0, 'N', 0, 'O', 0, '_', 0, 'C', 0, 'H', 0, '_', 0, 'H', 0, 'W', 0, 'A', 0, 'N', 0, '_', 0, '1', 0, '_', 0, '1', 0}
	return bytes.Contains(data, []byte("QNO_CH_HWAN_1_1")) || bytes.Contains(data, utf16)
}

/*
================
wireHwanLevel

The Blue Berserk level's seats (and the operator knobs, which refuse a malformed value): the quest owner derives it, the action lane
spends it (absorption, scroll requirements, the length of a Berserk), and the
port-only 0x3FC0 frames tell the owner at world entry and when it rises.
================
*/
func (game *gameplayPlane) wireHwanLevel(quests *quest.Runtime) error {
	if err := applyHwanKnobs(quests); err != nil {
		return err
	}
	// The trial arenas exist only with the quest rows (the build switch); their sealed floor
	// must not be rescued away (water.ExemptStrandedRescue).
	if _, hwan := quests.Defs.ByCodename("QNO_CH_HWAN_1_1"); hwan {
		game.water.ExemptStrandedRescue(quest.HwanTrialRegions())
	}
	game.items.HwanLevel = quests.HwanLevel
	game.items.SetHwanLevel = quests.SetGMHwanLevel
	quests.HwanLevelFrames = game.items.HwanLevelFrames
	halloween := game.deps.EntryEventFrames
	game.deps.EntryEventFrames = func(division string, character *enterworld.Character) []enterworld.Packet {
		var packets []enterworld.Packet
		if halloween != nil {
			packets = halloween(division, character)
		}
		for _, frame := range game.items.HwanEntryFrames(character) {
			packets = append(packets, enterworld.NewPacket(frame.Opcode, frame.Payload))
		}
		return packets
	}
	return nil
}
