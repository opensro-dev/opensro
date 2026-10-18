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
skipped and the next-lower one is tried, and no weapon, shield or job-dress
socket is ever created.

The swapped-out piece is DESTROYED. It is not put in the bag and it does not
drop: the socket-grant path removes the old row from the authoritative
inventory. The rule only swaps on a proven improvement (gearUpgrade): the
candidate must be no worse in degree, authored level, seal tier, plus, whites
and blues, and better in at least one. A piece the player enhanced or
rerolled above the configured values therefore stays.

The rule fires on the experience update, for every level crossed since the
last update, inside the character transaction (the caller owns the door).
Native behaviour is an INFERENCE, not verified against the binaries: the
level transition writes experience, stats and the level-up effect, and shows
no equipment write.

The rule bypasses the normal enhancement and reroll process. The configured
plus and its maximum whites and blues are a feature choice, not evidence of
a native Alchemy maximum.

===========================================================================
*/
package main

import (
	"math"
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
//
// +12 is a feature choice, not a native ceiling: the native reinforcement
// path refuses at plus 250 (alchemy.nativeReinforceCeiling), and this shard's
// own SRO_ALCHEMY_MAX_PLUS caps normal play at +11.
const (
	autoMaxGearDefaultPlus = 12
	autoMaxGearMaxPlus     = 12
)

// The white-stat packing: one five-bit field per attribute, 31 is 100%.
const (
	varianceFieldWidth = 5
	varianceFieldMask  = 0x1f
	varianceFieldMax   = 31
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

// autoMaxGearSealName is the operator-facing name of a tier letter, for the
// rules page.
func autoMaxGearSealName(tier string) string {
	switch tier {
	case "C":
		return "sun"
	case "B":
		return "moon"
	case "A":
		return "star"
	}
	return tier
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
experienceWithAutoMaxGear

Wrap the stat authority's experience updater so a level INCREASE also fires
the port-only, not-native auto max-gear rule, inside the same character
transaction (the caller owns the door). With SRO_AUTO_MAXGEAR unset the
wrapper adds nothing and the path is native.

INFERENCE, not verified against the native binaries: the v1.150 level
transition writes experience, stats and the level-up effect, and shows no
equipment write.
================
*/
func (g *gameplayPlane) experienceWithAutoMaxGear(
	update func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool),
) func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
	return func(character *enterworld.Character, expDelta, skillExpDelta int64, sourceGid uint32) ([]wire.Frame, bool) {
		before := characterLevelOf(character)
		frames, ok := update(character, expDelta, skillExpDelta, sourceGid)
		if ok && characterLevelOf(character) > before {
			frames = append(frames, g.applyAutoMaxGear(character)...)
		}
		return frames, ok
	}
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
		old, worn := wornRow(c, socket)
		if old != "" {
			add(g.seatGear(c, socket, wornPool[gearPieceKey(old)], worn, plus))
			continue
		}
		switch {
		case socket < 6 && race != "" && gender != "" && class != "":
			add(g.seatGear(c, socket, partPool[partKey{race: race, gender: gender, class: class, socket: int(socket)}], worn, plus))
		case socket >= 9 && socket <= 12 && race != "":
			accSocket := int(socket)
			if socket == 12 {
				accSocket = 11
			}
			add(g.seatGear(c, socket, accPool[accKey{race: race, socket: accSocket}], worn, plus))
		}
	}
	return c.MissionInventory, peerFrames, actorFrames
}

// seatGear seats the first candidate the character may wear into the socket
// and returns its peer and actor frames. A candidate that is not a proven
// upgrade over the worn piece is skipped, so a worn piece is never replaced
// by an equal or weaker one.
//
// When the swap happens, the worn piece is DESTROYED: the socket-grant path
// removes the old row from the authoritative inventory (no ground spawn, no
// bag slot), so the player loses it. gearUpgrade is the guard that keeps that
// loss to a real improvement.
func (g *gameplayPlane) seatGear(c *enterworld.Character, socket int64, pool []sealPick, worn enterworld.InventoryRow, plus uint8) ([]wire.Frame, []wire.Frame) {
	slot := uint8(socket)
	old := worn.Codename
	for _, pick := range pool {
		if pick.codename == old {
			continue
		}
		if old != "" && !g.gearUpgrade(worn, pick, plus) {
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
		codename, _ := wornRow(c, socket)
		tokens := strings.Split(strings.TrimSuffix(codename, "_DEF"), "_")
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

// wornRow is the inventory row in an equipment socket (0..12), or the zero
// row. The row carries the worn plus, whites and blues the upgrade guard
// compares.
func wornRow(c *enterworld.Character, socket int64) (string, enterworld.InventoryRow) {
	for _, row := range c.MissionInventory {
		if row.Slot == socket {
			return row.Codename, row
		}
	}
	return "", enterworld.InventoryRow{Slot: socket}
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

/*
================
gearUpgrade

gearUpgrade reports whether the candidate replaces the worn piece. The swap
DESTROYS the worn piece, so the answer is true only on a proven improvement:
the candidate must not be worse in ANY property and must be better in at
least one. The compared properties are:

 1. Degree. Degree and level are separate properties: a +8 weapon stays in
    its degree.
 2. Authored level requirement. A degree spans several levels (a D2 sword at
    8, 10 and 13), and armour unlocks a slot at a time (D2 heavy legs at 8,
    chest at 13).
 3. Seal tier, A (star) < B (moon) < C (sun).
 4. The enhancement plus.
 5. The white-stat variance bits.
 6. The blue options.

A worn piece the player enhanced or rerolled above the configuration
therefore stays. A same-degree update to a later variant still passes: the
minted piece carries the configured plus and maximum whites and blues, so it
matches those fields and wins on the level.

Alchemy materials are not involved: a stone belongs to the item's degree, and
this rule creates no stones.
================
*/
func (g *gameplayPlane) gearUpgrade(worn enterworld.InventoryRow, pick sealPick, plus uint8) bool {
	// Unknown references keep the worn piece: the guard must never authorize
	// a burn it cannot measure.
	candidate, ok := g.candidateFields(pick.codename)
	if !ok {
		return false
	}
	oldDegree, _ := gearDegree(worn.Codename)
	newDegree, _ := gearDegree(pick.codename)
	oldLevel, _ := loot.CatalogLevel(worn.Codename)
	newLevel, _ := loot.CatalogLevel(pick.codename)
	strengths := []struct{ worn, candidate int64 }{
		{int64(oldDegree), int64(newDegree)},
		{int64(oldLevel), int64(newLevel)},
		{int64(sealRank(worn.Codename)), int64(sealRank(pick.codename))},
		{worn.Plus, int64(plus)},
		{wornVarianceStrength(worn, candidate.fields), candidate.variance},
		{magicStrength(worn.MagicOptions), candidate.magic},
	}
	better := false
	for _, s := range strengths {
		if s.worn > s.candidate {
			return false
		}
		if s.candidate > s.worn {
			better = true
		}
	}
	// Equal in every property: the swap would destroy the worn piece for an
	// identical one. Keep it.
	return better
}

// sealRank orders the seal tiers: A (star) < B (moon) < C (sun), and 0 for a
// codename without a tier letter.
func sealRank(codename string) int {
	for _, token := range strings.Split(codename, "_") {
		switch token {
		case "A":
			return 1
		case "B":
			return 2
		case "C":
			return 3
		}
	}
	return 0
}

// candidateFields is what a minted candidate is worth, measured from the
// catalog so the comparison never assumes a field count.
type candidateFields struct {
	fields   int
	variance int64
	magic    int64
}

// candidateFields reads the equipment reference and its best option set.
func (g *gameplayPlane) candidateFields(codename string) (candidateFields, bool) {
	if g == nil || g.items == nil {
		return candidateFields{}, false
	}
	if g.deps == nil || g.deps.ItemReferences() == nil {
		return candidateFields{}, false
	}
	ref, ok := g.deps.ItemReferences().ItemRefByCodename(codename)
	if !ok || ref == nil {
		return candidateFields{}, false
	}
	fields := varianceFieldCount(ref)
	if fields == 0 {
		return candidateFields{}, false
	}
	return candidateFields{
		fields:   fields,
		variance: int64(fields) * varianceFieldMax,
		magic:    magicStrength(loot.BestEquipmentMagic(codename)),
	}, true
}

// varianceFieldCount is the number of white-stat fields the item type carries:
// weapons 7, CH/EU accessories 2, armour and shields 6. It mirrors the reward
// planner's rule for the same packing.
func varianceFieldCount(ref *enterworld.ItemRef) int {
	if ref == nil {
		return 0
	}
	switch ref.TypeIDs[2] {
	case 6: // weapons
		return 7
	case 5, 12: // CH/EU accessories
		return 2
	case 1, 2, 3, 4, 9, 10, 11: // armour and shields
		return 6
	}
	return 0
}

// wornVarianceStrength is a comparable score for the white-stat bits: the sum
// of the five-bit fields, so a reroll that lowers one field and raises
// another cannot pass as a tie.
func wornVarianceStrength(row enterworld.InventoryRow, fields int) int64 {
	bits, err := strconv.ParseUint(row.VarianceBits, 10, 64)
	if err != nil {
		// A malformed field string is not measurable: keep the worn piece.
		return math.MaxInt64
	}
	var total int64
	for field := 0; field < fields; field++ {
		total += int64(bits >> (field * varianceFieldWidth) & varianceFieldMask)
	}
	return total
}

// magicStrength scores an option set: the sum of the magnitudes. Option ids
// differ between items, so only the magnitudes compare.
func magicStrength(options []uint64) int64 {
	var total int64
	for _, option := range options {
		total += int64(option >> 32)
	}
	return total
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
