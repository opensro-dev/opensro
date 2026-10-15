/*
===========================================================================

automaxgear.go - automatic best-gear on level-up (port-only, not native)

Port-only, not native. The original game changes no equipment on a level
increase. With SRO_AUTO_MAXGEAR on, a level increase upgrades every worn
piece to the best "<tier>_RARE" variant the level and the equip gate allow
and fills an empty armour or accessory socket. Unset, and any value other
than "on", "1" or "true", leaves the level-up path native and writes no
inventory frame.

SRO_AUTO_MAXGEAR_SEAL names the seal tier (sun, moon or star; default sun)
and SRO_AUTO_MAXGEAR_PLUS the plus (0..12; default 12). The upgrade reuses
the operator socket-grant path, so a candidate the equip gate refuses is
skipped and the next-lower one is tried, a worn piece is never downgraded,
and no weapon, shield or job-dress socket is ever created.

The rule fires on the experience update, for every level crossed since the
last update, inside the character transaction (the caller owns the door).

===========================================================================
*/
package main

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
)

// The environment variables of the feature. SRO_AUTO_MAXGEAR unset is native.
const (
	EnvAutoMaxGear     = "SRO_AUTO_MAXGEAR"
	EnvAutoMaxGearSeal = "SRO_AUTO_MAXGEAR_SEAL"
	EnvAutoMaxGearPlus = "SRO_AUTO_MAXGEAR_PLUS"
)

// autoMaxGearDefaultPlus is the plus of a seated grant, and
// autoMaxGearMaxPlus its bound (the GM console limit).
const (
	autoMaxGearDefaultPlus = 12
	autoMaxGearMaxPlus     = 12
)

/*
================
autoMaxGearConfig
================

The resolved setting: whether the feature runs, the seal tier letter ("C"
sun, "B" moon, "A" star) and the plus.
*/
type autoMaxGearConfig struct {
	enabled bool
	tier    string
	plus    uint8
}

// sealTierLetter maps an operator seal name to the codename letter.
func sealTierLetter(seal string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(seal)) {
	case "sun", "c":
		return "C", true
	case "moon", "b":
		return "B", true
	case "star", "a":
		return "A", true
	}
	return "", false
}

/*
================
autoMaxGearConfigFromEnv
================

Reads the three variables. Unset SRO_AUTO_MAXGEAR is native (off); an
unknown seal name or a plus outside 0..12 keeps the default.
*/
func autoMaxGearConfigFromEnv() autoMaxGearConfig {
	cfg := autoMaxGearConfig{enabled: false, tier: "C", plus: autoMaxGearDefaultPlus}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAutoMaxGear))) {
	case "on", "1", "true":
		cfg.enabled = true
	}
	if tier, ok := sealTierLetter(os.Getenv(EnvAutoMaxGearSeal)); ok {
		cfg.tier = tier
	}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(EnvAutoMaxGearPlus))); err == nil && n >= 0 && n <= autoMaxGearMaxPlus {
		cfg.plus = uint8(n)
	}
	return cfg
}

// characterLevelOf reads the character's level with the same absent-field
// fallback the rest of the server uses (an absent level reads as 1).
func characterLevelOf(c *enterworld.Character) int64 {
	if c == nil || c.Level == nil {
		return 1
	}
	return *c.Level
}

/*
================
applyAutoMaxGear

Upgrades the worn gear and fills the empty armour and accessory sockets of
a character that just gained a level. Returns the actor frames to publish;
the peer frames go out through the item plane. Native (no frame) when the
feature is off.
================
*/
func (g *gameplayPlane) applyAutoMaxGear(c *enterworld.Character) []wire.Frame {
	if g == nil || g.items == nil || c == nil {
		return nil
	}
	cfg := autoMaxGearConfigFromEnv()
	if !cfg.enabled {
		return nil
	}
	rows, peer, actor := g.applyGearRule(c, cfg.tier, cfg.plus)
	if len(actor) == 0 && len(peer) == 0 {
		return nil
	}
	c.MissionInventory = rows
	if len(peer) > 0 && g.items.PushDivisionPeerFrames != nil {
		g.items.PushDivisionPeerFrames(g.divisionID, c.Name, peer)
	}
	return actor
}

// sealPick is one candidate "<tier>_RARE" piece: its level and codename.
type sealPick struct {
	level    int64
	codename string
}

// partKey identifies an armour pool: race, gender, armour class and socket.
type partKey struct {
	race   string
	gender string
	class  string
	socket int
}

// accKey identifies an accessory pool: race and socket.
type accKey struct {
	race   string
	socket int
}

// partSocketByToken maps an armour codename part (head..foot) to its socket,
// the same mapping the operator "max-gear" uses.
var partSocketByToken = map[string]int{"CA": 0, "BA": 1, "SA": 2, "AA": 3, "LA": 4, "FA": 5}

// accSocketByToken maps an accessory codename part to its socket.
var accSocketByToken = map[string]int{"EARRING": 9, "NECKLACE": 10, "RING": 11}

/*
================
autoMaxGearPools

Groups every catalog "<tier>_RARE" row at or below level into the worn-piece
pool (keyed by the piece), the armour pool (race, gender, class, socket) and
the accessory pool (race, socket). Each pool is ordered strongest first.
================
*/
func autoMaxGearPools(level int64, tier string) (map[string][]sealPick, map[partKey][]sealPick, map[accKey][]sealPick) {
	wornPool := map[string][]sealPick{}
	partPool := map[partKey][]sealPick{}
	accPool := map[accKey][]sealPick{}
	for _, r := range loot.SealRefs(tier) {
		if int64(r.Level) > level {
			continue
		}
		pick := sealPick{level: int64(r.Level), codename: r.Codename}
		key := gearPieceKey(r.Codename)
		wornPool[key] = append(wornPool[key], pick)
		tokens := strings.Split(strings.TrimSuffix(r.Codename, "_"+tier+"_RARE"), "_")
		switch {
		case len(tokens) == 6 && (tokens[2] == "M" || tokens[2] == "W"):
			if socket, ok := partSocketByToken[tokens[5]]; ok {
				k := partKey{race: tokens[1], gender: tokens[2], class: tokens[3], socket: socket}
				partPool[k] = append(partPool[k], pick)
			}
		case len(tokens) == 4:
			if socket, ok := accSocketByToken[tokens[2]]; ok {
				k := accKey{race: tokens[1], socket: socket}
				accPool[k] = append(accPool[k], pick)
			}
		}
	}
	for _, pool := range wornPool {
		sortSealPicks(pool)
	}
	for _, pool := range partPool {
		sortSealPicks(pool)
	}
	for _, pool := range accPool {
		sortSealPicks(pool)
	}
	return wornPool, partPool, accPool
}

/*
================
applyGearRule

Upgrades every worn piece to the best piece of the configured seal tier
that the level and equip gate allow, and fills an empty armour or accessory
socket with the best the character may wear. It mirrors the operator
"max-gear": a candidate the character cannot wear (level, mastery, gender,
country) is skipped and the next-lower one is tried. It never downgrades a
worn piece and never creates a weapon, shield or job-dress socket. It runs
in the caller's transaction.
================
*/
func (g *gameplayPlane) applyGearRule(c *enterworld.Character, tier string, plus uint8) ([]enterworld.InventoryRow, []wire.Frame, []wire.Frame) {
	level := characterLevelOf(c)
	wornPool, partPool, accPool := autoMaxGearPools(level, tier)

	race, gender, class := wornIdentity(c)
	var peerFrames, actorFrames []wire.Frame
	add := func(peer, actor []wire.Frame) {
		peerFrames = append(peerFrames, peer...)
		actorFrames = append(actorFrames, actor...)
	}
	for socket := int64(0); socket < 13; socket++ {
		old := wornCodename(c, socket)
		if old != "" {
			add(g.seatGear(c, socket, wornPool[gearPieceKey(old)], old, plus))
			continue
		}
		switch {
		case socket < 6 && race != "" && gender != "" && class != "":
			add(g.seatGear(c, socket, partPool[partKey{race: race, gender: gender, class: class, socket: int(socket)}], "", plus))
		case socket >= 9 && socket <= 12 && race != "":
			accSocket := int(socket)
			if socket == 12 {
				accSocket = 11
			}
			add(g.seatGear(c, socket, accPool[accKey{race: race, socket: accSocket}], "", plus))
		}
	}
	return c.MissionInventory, peerFrames, actorFrames
}

// seatGear seats the first candidate the character may wear into the socket
// and returns its peer and actor frames. A candidate at or below the worn
// piece's degree is skipped, so a worn piece is never downgraded.
func (g *gameplayPlane) seatGear(c *enterworld.Character, socket int64, pool []sealPick, old string, plus uint8) ([]wire.Frame, []wire.Frame) {
	slot := uint8(socket)
	for _, pick := range pool {
		if pick.codename == old {
			continue
		}
		if old != "" && gearDowngrade(old, pick.codename) {
			continue
		}
		rows, peer, actor, err := g.items.SeatOperatorEquipment(c, g.divisionID, c.MissionInventory, []inventory.ItemAmount{{
			Codename: pick.codename, Count: 1, Plus: plus, PerfectStats: true, PerfectMagic: true, Slot: &slot,
		}})
		if err != nil {
			continue
		}
		c.MissionInventory = rows
		return peer, actor
	}
	return nil, nil
}

// sortSealPicks orders candidates strongest first: by level, then codename,
// matching the operator "max-gear" pick.
func sortSealPicks(picks []sealPick) {
	sort.Slice(picks, func(i, j int) bool {
		if picks[i].level != picks[j].level {
			return picks[i].level > picks[j].level
		}
		return picks[i].codename > picks[j].codename
	})
}

// wornIdentity reads race, gender and armour class from the worn set, the
// same signal the operator "max-gear" uses to choose a fill for an empty
// socket.
func wornIdentity(c *enterworld.Character) (race, gender, class string) {
	for socket := int64(0); socket < 13; socket++ {
		tokens := strings.Split(strings.TrimSuffix(wornCodename(c, socket), "_DEF"), "_")
		if n := len(tokens); n > 0 && tokens[n-1] == "RARE" {
			tokens = tokens[:n-1]
		}
		if n := len(tokens); n > 0 && (tokens[n-1] == "A" || tokens[n-1] == "B" || tokens[n-1] == "C") {
			tokens = tokens[:n-1]
		}
		if len(tokens) < 2 {
			continue
		}
		if race == "" && (tokens[1] == "CH" || tokens[1] == "EU") {
			race = tokens[1]
		}
		if gender == "" && class == "" && len(tokens) == 6 && (tokens[1] == "CH" || tokens[1] == "EU") && (tokens[2] == "M" || tokens[2] == "W") {
			gender, class = tokens[2], tokens[3]
		}
	}
	return race, gender, class
}

// wornCodename is the codename in an equipment socket (0..12), or "".
func wornCodename(c *enterworld.Character, socket int64) string {
	for _, row := range c.MissionInventory {
		if row.Slot == socket {
			return row.Codename
		}
	}
	return ""
}

// gearPieceKey mirrors the operator tool: drop a trailing RARE and a tier
// letter, then blank a two-digit degree token so every variant shares a key.
func gearPieceKey(codename string) string {
	parts := strings.Split(codename, "_")
	// A "_DEF" armor variant has no Sun sibling of its own. Drop the suffix
	// first so it maps to the same "_C_RARE" as the other pieces of the part.
	if n := len(parts); n > 0 && parts[n-1] == "DEF" {
		parts = parts[:n-1]
	}
	if n := len(parts); n > 0 && parts[n-1] == "RARE" {
		parts = parts[:n-1]
	}
	if n := len(parts); n > 0 && (parts[n-1] == "A" || parts[n-1] == "B" || parts[n-1] == "C") {
		parts = parts[:n-1]
	}
	for i, p := range parts {
		if len(p) == 2 && isTwoDigits(p) {
			parts[i] = "DD"
		}
	}
	return strings.Join(parts, "_")
}

// gearDowngrade is true when target has a lower degree than old; an
// already-worn piece above the equip gate must stay.
func gearDowngrade(old, target string) bool {
	o, okO := gearDegree(old)
	t, okT := gearDegree(target)
	return okO && okT && t < o
}

// gearDegree is the two-digit degree token, or 0/false.
func gearDegree(codename string) (int, bool) {
	for _, p := range strings.Split(codename, "_") {
		if len(p) == 2 && isTwoDigits(p) {
			return int(p[0]-'0')*10 + int(p[1]-'0'), true
		}
	}
	return 0, false
}

func isTwoDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
