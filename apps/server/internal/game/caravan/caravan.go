/*
===========================================================================

caravan.go - trade caravans and the bandit ambushes they draw

Package caravan owns the server's caravan registry and the native rules
that decide when and how strongly a loaded trade transport is ambushed
(the reference server's caravan manager and caravan rules).
A caravan is registered when its trader summons a transport, moves cargo
or registers as a trader; every one to two minutes its timer fires, the
trader and vehicle are validated, and in a battlefield region bandits
spawn around the vehicle. This package holds the registry and the pure
formulas; the action owner supplies characters, cargo and spawning.

===========================================================================
*/

package caravan

import (
	"math"
	"sort"
)

const (
	// timerBaseMs and timerSpreadMs: ResetSpawnTimer draws the next ambush
	// at 60..120 seconds (float32(rand/32767) * 60000 + 60000).
	timerBaseMs   = 60000
	timerSpreadMs = 60000
	// randomMaximum is CRT RAND_MAX.
	randomMaximum = 32767
	// tradeStarDivisor and the tier thresholds are the server's caravan defaults
	// (TRADE_MON_FACTOR, TRADESCALE1..5_AMOUNT_MAX); no refdata row
	// overrides them in our data.
	tradeStarDivisor = 306
	// cargoUnitFactor and cargoSlotFactor are the cargo value's constants.
	cargoUnitFactor = 348.0
	cargoSlotFactor = 40.0
	// specialGoodsFactor is the star rating's float for 3.3.8.2 cargo.
	specialGoodsFactor = 1.5499999523162842
	// cargoLevelMin and cargoLevelMax clamp the trader's max level, as the cargo value does.
	cargoLevelMin = 20
	cargoLevelMax = 140
	// banditLevelFloor and banditLevelOffset are the bandit level's constants.
	banditLevelFloor  = 16
	banditLevelOffset = 4
	// thiefTacticsBase and hunterTacticsBase: the bandit spawn picks 2001.. for a
	// trader's thieves, 2011.. for everyone else's hunters.
	thiefTacticsBase  = 2001
	hunterTacticsBase = 2011
	// percentRare is the 2% (rand%100 < 2) every doubling and champion uses.
	percentRare = 2
	// traderJob is the player's job state 1.
	traderJob = 1
)

// tierThresholds is the server's trade tier threshold table; index 5 caps.
var tierThresholds = [...]uint32{0, 408, 918, 1428, 2142, math.MaxUint32}

/*
================
Roll

One CRT rand() draw, 0..32767. The action owner injects the source.
================
*/
type Roll func() (uint32, error)

/*
================
Entry

One registered caravan: its trader and the native spawn timer.
================
*/
type Entry struct {
	Division, Character string
	ElapsedMs, NextMs   int64
}

/*
================
Registry

The registered caravans by trader (CaravanManager's map). The action
runtime owns one and serializes every call.
================
*/
type Registry struct {
	entries map[string]*Entry
}

/*
================
NewRegistry
================
*/
func NewRegistry() *Registry {
	return &Registry{entries: map[string]*Entry{}}
}

/*
================
key
================
*/
func key(division, character string) string {
	return division + "\x00" + character
}

/*
================
Register

The caravan manager: an existing caravan is kept with its running timer; a new one
starts a fresh timer.
================
*/
func (r *Registry) Register(division, character string, roll Roll) error {
	k := key(division, character)
	if _, exists := r.entries[k]; exists {
		return nil
	}
	entry := &Entry{Division: division, Character: character}
	if err := resetTimer(entry, roll); err != nil {
		return err
	}
	r.entries[k] = entry
	return nil
}

/*
================
Remove
================
*/
func (r *Registry) Remove(division, character string) {
	delete(r.entries, key(division, character))
}

/*
================
Len
================
*/
func (r *Registry) Len() int {
	return len(r.entries)
}

/*
================
Due

The caravan timer, per caravan: the elapsed time grows until it reaches the drawn
delay; then the timer restarts and the caravan is returned to be fired.
The caller validates each one outside its own lock and removes the
invalid ones (Remove). Caravans are visited in key order, so a tick is
deterministic.
================
*/
func (r *Registry) Due(deltaMs int64, roll Roll) ([]Entry, error) {
	keys := make([]string, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var due []Entry
	for _, k := range keys {
		entry := r.entries[k]
		entry.ElapsedMs += deltaMs
		if entry.ElapsedMs < entry.NextMs {
			continue
		}
		if err := resetTimer(entry, roll); err != nil {
			return due, err
		}
		due = append(due, *entry)
	}
	return due, nil
}

/*
================
resetTimer

The reference server's spawn-timer reset: elapsed 0, and the next
interval is 60000 plus the truncation of float32(rand/32767) * 60000.
================
*/
func resetTimer(entry *Entry, roll Roll) error {
	fraction, err := floatDraw(roll)
	if err != nil {
		return err
	}
	entry.ElapsedMs = 0
	entry.NextMs = int64(float64(fraction)*timerSpreadMs) + timerBaseMs
	return nil
}

/*
================
floatDraw

float32(rand() / 32767.0), the native's stored draw.
================
*/
func floatDraw(roll Roll) (float32, error) {
	value, err := roll()
	if err != nil {
		return 0, err
	}
	return float32(float64(value) / randomMaximum), nil
}

/*
================
percentDraw

rand() % 100 < 2.
================
*/
func percentDraw(roll Roll) (bool, error) {
	value, err := roll()
	if err != nil {
		return false, err
	}
	return value%100 < percentRare, nil
}

/*
================
CargoCapacity

The server's transport slot capacity: rarity 1 holds 40, rarity 2 holds 30;
any other rarity logs and falls back to the vehicle's inventory size.
================
*/
func CargoCapacity(rarity uint8, inventory uint16) uint16 {
	switch rarity {
	case 1:
		return 40
	case 2:
		return 30
	}
	return inventory
}

/*
================
CargoValue

The server's cargo value: goods count x 348 over the base
death EXP of the trader's clamped max level, scaled by capacity / 40.
Zero when nothing is carried; otherwise at least 1.
================
*/
func CargoValue(goods uint32, capacity uint16, baseDeathExp int64) uint32 {
	if goods == 0 || baseDeathExp <= 0 || capacity == 0 {
		return 0
	}
	value := float64(goods) * cargoUnitFactor / float64(baseDeathExp)
	value /= cargoSlotFactor / float64(capacity)
	if !(0 < value) {
		return 0
	}
	if value < 1 {
		return 1
	}
	return uint32(value)
}

/*
================
CargoLevel

The cargo value clamps the trader's max level into 20..140 before the EXP basis.
================
*/
func CargoLevel(maxLevel int64) int64 {
	return min(max(maxLevel, cargoLevelMin), cargoLevelMax)
}

/*
================
BaseDeathExp

The server's base death EXP: trunc(GoldMin(level) x 10 x
0.125), from the drop-gold table.
================
*/
func BaseDeathExp(goldMinimum int64) int64 {
	return int64(math.Trunc(float64(goldMinimum*10) * 0.125))
}

/*
================
StarRating

The server's star rating. specialGoods is cargo holding
3.3.8.2 goods (the transport's TID4 2 goods test).
================
*/
func StarRating(value uint32, specialGoods bool) uint32 {
	if value == 0 {
		return 0
	}
	if !specialGoods {
		if value > tierThresholds[1] {
			return (value-1)/tradeStarDivisor + 1
		}
		return 1
	}
	return uint32((float64(value)*specialGoodsFactor-1)/tradeStarDivisor + 1)
}

/*
================
DifficultyTier

The server's trade difficulty tier: the first threshold the value
does not exceed, capped at 5.
================
*/
func DifficultyTier(value uint32) uint8 {
	tier := uint8(0)
	for value > tierThresholds[tier] {
		tier++
		if tier >= 5 {
			return 5
		}
	}
	return tier
}

/*
================
SpawnCount

The server's bandit spawn count: the stars (at least 1), one
more on a float draw of at least 0.5, doubled on a 2% draw.
================
*/
func SpawnCount(stars uint32, roll Roll) (int, error) {
	count := int(stars)
	if count == 0 {
		count = 1
	}
	fraction, err := floatDraw(roll)
	if err != nil {
		return 0, err
	}
	if fraction >= 0.5 {
		count++
	}
	rare, err := percentDraw(roll)
	if err != nil {
		return 0, err
	}
	if rare {
		count *= 2
	}
	return count, nil
}

/*
================
BanditLevel

The server's bandit level: the tier (at least 1, doubled on a
2% draw) on top of the trader's level or best mastery less 4 (at least 16),
plus 0..3 from a float draw.
================
*/
func BanditLevel(tier uint8, level, mastery uint8, roll Roll) (int, error) {
	boost := int(tier)
	if boost <= 0 {
		boost = 1
	}
	rare, err := percentDraw(roll)
	if err != nil {
		return 0, err
	}
	if rare {
		boost *= 2
	}
	base := int(max(level, mastery)) - banditLevelOffset
	if base < banditLevelFloor {
		base = banditLevelFloor
	}
	fraction, err := floatDraw(roll)
	if err != nil {
		return 0, err
	}
	return base + int(float64(fraction)*banditLevelOffset) + boost - 1, nil
}

/*
================
TacticsID

The bandit spawn: an odd draw picks rand & 3, an even one 3; thieves (a trader's
ambush) start at 2001, hunters at 2011.
================
*/
func TacticsID(job uint8, roll Roll) (uint32, error) {
	parity, err := roll()
	if err != nil {
		return 0, err
	}
	offset := uint32(3)
	if parity&1 != 0 {
		draw, err := roll()
		if err != nil {
			return 0, err
		}
		offset = draw & 3
	}
	if job == traderJob {
		return thiefTacticsBase + offset, nil
	}
	return hunterTacticsBase + offset, nil
}

/*
================
Thieves

A trader's caravan draws thieves; anyone else's draws hunters.
================
*/
func Thieves(job uint8) bool {
	return job == traderJob
}

/*
================
HeadingRadians

The bandit spawn's per-bandit angle: float32(float32(rand/32767) x 2PI).
================
*/
func HeadingRadians(roll Roll) (float32, error) {
	fraction, err := floatDraw(roll)
	if err != nil {
		return 0, err
	}
	return float32(float64(fraction) * 6.2831854820251465), nil
}

/*
================
ChampionRarity

The bandit spawn: a 2% draw spawns the bandit as a champion (rarity nibble 1).
================
*/
func ChampionRarity(roll Roll) (uint8, error) {
	rare, err := percentDraw(roll)
	if err != nil || !rare {
		return 0, err
	}
	return 1, nil
}
