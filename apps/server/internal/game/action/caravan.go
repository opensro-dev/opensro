/*
===========================================================================

caravan.go - the action owner's side of trade caravan ambushes

The caravan package holds the registry and the native formulas; this file
supplies what only the runtime can: the trader, the transport and its
cargo, the population the bandits join and the tick that drives them. A
caravan is registered when a transport carrying goods is summoned or cargo
enters a transport; each fire validates the trader and vehicle again, so
a stale registration simply drops out.

===========================================================================
*/

package action

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// cargoGoodsWildcard is the reference server's any-trade-goods query word 0x046C:
// TID 3.3.8 with a zero (wildcard) TID4.
const cargoGoodsWildcard uint16 = 0x046c

// cargoSpecialGoods is 0x146C: 3.3.8.2, the goods the x1.55 rating counts.
const cargoSpecialGoods uint16 = 0x146c

/*
================
caravanRoll
================
*/
func (rt *Runtime) caravanRoll() caravan.Roll {
	return caravan.Roll(rt.CaravanRoll)
}

/*
================
registerCaravan

The reference server registers a caravan through its callers: a summoned transport
already carrying goods, cargo picked up or loaded into a vehicle
(the item-move result). INFERENCE: v1.150 also loads a transport by buying into it or
moving goods from the bag, which the v1.188 caller list does not name;
registering there too is safe because an existing caravan keeps its timer
and every fire revalidates the cargo.
================
*/
func (rt *Runtime) registerCaravan(division string, c *enterworld.Character) {
	if c == nil || !vehicleCarriesGoods(rt, c) {
		return
	}
	rt.caravanMu.Lock()
	defer rt.caravanMu.Unlock()
	if err := rt.caravans.Register(division, c.Name, rt.caravanRoll()); err != nil {
		log.Warnf("caravan: register %s: %v", c.Name, err)
	}
}

/*
================
registerCaravanForMove

The cargo moves that put goods into a transport.
================
*/
func (rt *Runtime) registerCaravanForMove(division string, c *enterworld.Character, movement uint8) {
	switch movement {
	case wire.MoveTypeCosPickup, wire.MoveTypeCosShopBuy, wire.MoveTypePlayerToCos:
		rt.registerCaravan(division, c)
	}
}

/*
================
advanceCaravans

The reference server's caravan tick: advance every caravan by the elapsed time,
then fire the due ones outside the registry lock (they take division
locks) and drop those that no longer validate.
================
*/
func (rt *Runtime) advanceCaravans(nowMs int64) {
	due, err := rt.dueCaravans(nowMs)
	if err != nil {
		log.Warnf("caravan: timer draw: %v", err)
	}
	for _, entry := range due {
		if rt.fireCaravan(entry, nowMs) {
			continue
		}
		rt.caravanMu.Lock()
		rt.caravans.Remove(entry.Division, entry.Character)
		rt.caravanMu.Unlock()
	}
}

/*
================
dueCaravans
================
*/
func (rt *Runtime) dueCaravans(nowMs int64) ([]caravan.Entry, error) {
	rt.caravanMu.Lock()
	defer rt.caravanMu.Unlock()
	delta := int64(0)
	if rt.caravanTickMs != 0 && nowMs > rt.caravanTickMs {
		delta = nowMs - rt.caravanTickMs
	}
	rt.caravanTickMs = nowMs
	return rt.caravans.Due(delta, rt.caravanRoll())
}

/*
================
fireCaravan

The reference server's per-caravan tick after the timer: validate the trader and vehicle
and, when the vehicle stands in a battlefield region, spawn the
bandits. False removes the caravan.
================
*/
func (rt *Runtime) fireCaravan(entry caravan.Entry, nowMs int64) bool {
	c := rt.findCharacter(entry.Division, entry.Character)
	if c == nil || rt.Monsters == nil {
		return false
	}
	unlock := rt.lockDivision(entry.Division)
	defer unlock()
	if !enterworld.CharacterAlive(c) || !vehicleCarriesGoods(rt, c) {
		return false
	}
	vehicle := rt.cosLiveSpawn(entry.Division, c, nowMs)
	if battlefield, known := world.RegionPlayerCombat(vehicle.RegionID); !known || !battlefield {
		return true
	}
	if err := rt.spawnCaravanBandits(entry.Division, c, vehicle, nowMs); err != nil {
		log.Warnf("caravan: bandits for %s: %v", c.Name, err)
	}
	return true
}

/*
================
spawnCaravanBandits

The reference server's bandit spawn, in native draw order: the tier and star
rating from the cargo, the bandit count (capped by the shard's setting),
the tactics, then per bandit its level, reference, heading and champion
roll.
================
*/
func (rt *Runtime) spawnCaravanBandits(division string, c *enterworld.Character, vehicle simulation.Spawn, nowMs int64) error {
	roll := rt.caravanRoll()
	// Port-only, not native with SRO_TRADE_TIERS: both from cargo gold
	// (tradetiers.go).
	tier, stars := rt.caravanTier(c)
	count, err := caravan.SpawnCount(stars, roll)
	if err != nil || count == 0 {
		return err
	}
	// Port-only, not native: the shard may cap an ambush (caravan/banditcap.go).
	// The reference server draws each bandit's level, reference, heading and champion roll
	// inside the loop and nothing after it, so stopping early leaves every
	// remaining bandit's draws exactly where they fall natively.
	count = caravan.CappedSpawnCount(count, rt.CaravanBanditCap)
	if rt.CaravanStarAmbush {
		// Port-only, not native: the fixed per-star table replaces the
		// rolled and capped count (caravan/starambush.go). The native roll
		// above still ran, so every bandit's draws fall where natively.
		count = caravan.StarAmbushCount(stars)
	}
	job := c.Job.Type
	tacticsID, err := caravan.TacticsID(job, roll)
	if err != nil {
		return err
	}
	tactics, ok := monster.CaravanTactics(tacticsID)
	if !ok {
		return nil
	}
	lease, admitted := rt.EntryPopulationLease(division, c.Name)
	if !admitted {
		return nil
	}
	tables := rt.Monsters.BanditTables()
	player := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, nowMs)
	zone, known := world.RegionCaravanZone(player.RegionID)
	if !known {
		return fmt.Errorf("caravan region %#x has no continent evidence", player.RegionID)
	}
	for range count {
		level, err := caravan.BanditLevel(tier, characterLevelByte(c), maxMasteryByte(c), roll)
		if err != nil {
			return err
		}
		var drawErr error
		ref, found := tables.Pick(caravan.Thieves(job), zone, level, func() uint32 {
			var value uint32
			value, drawErr = roll()
			return value
		})
		if drawErr != nil {
			return drawErr
		}
		if !found {
			continue
		}
		heading, err := caravan.HeadingRadians(roll)
		if err != nil {
			return err
		}
		rarity, err := caravan.ChampionRarity(roll)
		if err != nil {
			return err
		}
		rt.Monsters.SpawnCaravanBandit(simulation.CaravanBanditSpawn{
			Division: division, Population: lease, Ref: ref, Vehicle: vehicle,
			HeadingRadians: heading, Tactics: tactics, Rarity: rarity, NowMs: nowMs,
		})
	}
	return nil
}

/*
================
caravanCargoValue

The reference server's cargo value over the active transport.
================
*/
func (rt *Runtime) caravanCargoValue(c *enterworld.Character) uint32 {
	ref, ok := rt.cosCharacterRef(c)
	if !ok {
		return 0
	}
	levels, ok := rt.deps.LevelData().(interface {
		WithdrawalGoldBasis(level int64) (int64, bool)
	})
	if !ok {
		return 0
	}
	maxLevel := int64(1)
	if c.MaxLevel != nil {
		maxLevel = *c.MaxLevel
	}
	gold, found := levels.WithdrawalGoldBasis(caravan.CargoLevel(maxLevel))
	if !found {
		return 0
	}
	// v1.150 transports all carry rarity 0, the native fallback to the
	// vehicle's inventory size.
	capacity := caravan.CargoCapacity(ref.Parameters.MonsterType&0x0f, uint16(ref.InventoryCapacity))
	return caravan.CargoValue(cargoGoodsCount(c), capacity, caravan.BaseDeathExp(gold))
}

/*
================
vehicleCarriesGoods

The reference server's vehicle check: a summoned transport (isVehicleCOS) whose
cargo holds any trade goods.
================
*/
func vehicleCarriesGoods(rt *Runtime, c *enterworld.Character) bool {
	pet := c.ActiveCOS
	if pet == nil || !pet.Summoned || pet.Container == nil {
		return false
	}
	ref, ok := rt.cosReference(pet)
	if !ok || !isVehicleCOS(ref.TidWord) {
		return false
	}
	return cargoHolds(c, cargoGoodsWildcard)
}

/*
================
isVehicleCOS

The reference server's transport test: a COS transport, TID 1.2.3 with TID4 2.
================
*/
func isVehicleCOS(tid uint16) bool {
	return tid&2 != 0 && tid&0x1c == 4 && tid&0x60 == 0x40 && tid&0x780 == 0x180 && tid&0xf800 == 0x1000
}

/*
================
cargoHolds

The reference server's TID-matching storage query: every zero TID component of the
query matches anything.
================
*/
func cargoHolds(c *enterworld.Character, query uint16) bool {
	if c.ActiveCOS == nil || c.ActiveCOS.Container == nil {
		return false
	}
	for _, row := range c.ActiveCOS.Container.Rows {
		if tidMatches(row.TypeFlags, query) {
			return true
		}
	}
	return false
}

/*
================
tidMatches
================
*/
func tidMatches(flags, query uint16) bool {
	for _, field := range [...]uint16{0x1c, 0x60, 0x780, 0xf800} {
		if want := query & field; want != 0 && flags&field != want {
			return false
		}
	}
	return true
}

/*
================
cargoGoodsCount

The reference server's cargo total: the stack count of every trade
goods row (TID 3.3.8.x) in the transport.
================
*/
func cargoGoodsCount(c *enterworld.Character) uint32 {
	// A vehicle without a bag (a riding horse) carries no goods. Reading
	// its missing bag panicked a trader's shop open (0x7338).
	if c.ActiveCOS == nil || c.ActiveCOS.Container == nil {
		return 0
	}
	total := int64(0)
	for _, row := range c.ActiveCOS.Container.Rows {
		if row.TypeFlags&2 == 0 && tidMatches(row.TypeFlags, cargoGoodsWildcard) && row.StackCount > 0 {
			total += row.StackCount
		}
	}
	return uint32(min(total, int64(^uint32(0))))
}

/*
================
characterLevelByte
================
*/
func characterLevelByte(c *enterworld.Character) uint8 {
	if c.Level == nil || *c.Level < 1 {
		return 1
	}
	return uint8(min(*c.Level, 255))
}

/*
================
maxMasteryByte

The reference server's maximum mastery level: the highest mastery level.
================
*/
func maxMasteryByte(c *enterworld.Character) uint8 {
	best := int64(0)
	for _, mastery := range c.Masteries {
		best = max(best, mastery.Level)
	}
	return uint8(min(best, 255))
}
