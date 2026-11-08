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
	"os"

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

/*
================
wireHwanLevel

The Blue Berserk level's seats: the quest owner derives it, the action lane
spends it (absorption, scroll requirements, the length of a Berserk), and the
port-only 0x3FBF frames tell the owner at world entry and when it rises.
================
*/
func (game *gameplayPlane) wireHwanLevel(quests *quest.Runtime) {
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
}
