/*
===========================================================================

pvpserver.go - the PVP shard's switch and its fixed values

Port-only, not native. The fork runs a second shard, "pvp", built for
player combat alone: a character is created finished (the shard's level
cap, every mastery, every skill) in Samarkand, and Samarkand's merchants
sell the degree-11 rare gear already +12 and the degree-11 stones at one
gold (docs/superpowers/specs/2026-11-17-pvp-shard-design.md).

SRO_PVP_SERVER=on enables it, and only on the shard whose id is "pvp":
the switch landing in another shard's environment by mistake is ignored
with a warning, so it can never hand levels or gear out on the official
shard. Off is the native game.

This package is a leaf (domain, texttable, the standard library): the
NPC roster (simulation) and the shop catalog (commerce) both read the
shop layout from here, and neither may import the other.

===========================================================================
*/

package pvpserver

import (
	"fmt"
	"os"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
)

// EnvPvpServer names the switch.
const EnvPvpServer = "SRO_PVP_SERVER"

// ShardID is the one shard the switch may act on.
const ShardID = "pvp"

// envShardID is the GameWorld's own shard id (cmd/services/sro-gameworld).
const envShardID = "SRO_SHARD_ID"

// The spec's fixed values.
const (
	StartGold   int64  = 100_000_000
	ShopPrice   uint64 = 1
	ShopPlus    uint8  = 12
	StoneDegree        = 11
	TabCapacity        = 45
)

// Samarkand's arrival point: the GATE_CA row of teleportdata.txt.
const (
	samarkandRegionID int64   = 27244
	samarkandX        float64 = 270
	samarkandY        float64 = 180
	samarkandZ        float64 = 1421
	samarkandAngle    int64   = 0
)

// traderLineX is where the PVP merchants stand: a line across the plaza
// 90 units east of the arrival point, the way a character arriving faces.
// Every spot was checked walkable on the variant's navmesh
// (movement.WalkableSpawnHeightAt) on 2026-11-18.
const traderLineX float64 = 360

// traderAngle faces the merchants west, toward the arrival point (the
// gacha operator east of them faces the same way).
const traderAngle uint16 = 32768

// traderLineZ is each merchant's place on the line; the arrival's own
// Z (1421) stays free, so the walk straight ahead is not crowded.
var traderLineZ = map[string]float64{
	"NPC_CA_SMITH":     1341,
	"NPC_CA_ARMOR":     1381,
	"NPC_CA_ACCESSORY": 1461,
	"NPC_CA_POTION":    1501,
}

var warnOtherShard sync.Once

/*
================
Enabled

True when the switch is on and shardID is the PVP shard. An unreadable
value is an error, so a typo refuses startup instead of running native.
================
*/
func Enabled(shardID string, getenv func(string) string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(getenv(EnvPvpServer))) {
	case "", "0", "off", "false":
		return false, nil
	case "1", "on", "true":
	default:
		return false, fmt.Errorf("%s must be on or off, got %q", EnvPvpServer, getenv(EnvPvpServer))
	}
	if shardID != ShardID {
		warnOtherShard.Do(func() {
			log.Warnf("%s is on but this shard is %q, not %q: ignored", EnvPvpServer, shardID, ShardID)
		})
		return false, nil
	}
	return true, nil
}

/*
================
EnabledFromEnv

Enabled for the process's own shard. The roster and the shop catalog
call it at load; an unreadable value reads as off there, because the
GameWorld's wiring has already refused it at startup through Enabled.
================
*/
func EnabledFromEnv() bool {
	on, err := Enabled(os.Getenv(envShardID), os.Getenv)
	return on && err == nil
}

/*
================
TraderSpot

Where a PVP merchant stands instead of its authored place: Samarkand's
arrival region, on the line in front of the arrival point. False for
every other NPC.
================
*/
func TraderSpot(codename string) (regionID uint16, x, y, z float64, angle uint16, ok bool) {
	z, ok = traderLineZ[codename]
	if !ok {
		return 0, 0, 0, 0, 0, false
	}
	return uint16(samarkandRegionID), traderLineX, samarkandY, z, traderAngle, true
}

/*
================
SamarkandSpawn

A fresh copy of Samarkand's arrival point on every call: the record keeps
the pointers, so two characters must never share them.
================
*/
func SamarkandSpawn() domain.WorldSpawn {
	region, x, y, z, angle := samarkandRegionID, samarkandX, samarkandY, samarkandZ, samarkandAngle
	return domain.WorldSpawn{RegionID: &region, X: &x, Y: &y, Z: &z, Angle: &angle}
}
