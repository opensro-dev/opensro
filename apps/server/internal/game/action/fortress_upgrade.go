/*
===========================================================================

fortress_upgrade.go - the fortress manager raises a structure a level

0x71E1 action 0x0B from the fortress map window: build a defensive
position, raise a gate or tower a level, or turn a guard tower into one
of its four element towers. An empty defensive position is a level-0
structure, so building one is its upgrade to level 1, type 0. The
original takes the target from the client's siege structure upgrade table
(siegestructupgradedata.txt: per structure, up to four targets, "xxx"
for none), charges the member the current structure's gold and the guild
its guild points, and swaps the zone's structure for the target. A
structure must stand its build minutes after it was made before it may
be raised again; the made time survives a restart in the structure row.

===========================================================================
*/
package action

import (
	"fmt"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/monster"
)

const (
	// Low bytes of the upgrade refusals, in the original's check order.
	fortressErrUpgradeNoReference uint8 = 0x21 // the zone's structure has no reference
	fortressErrUpgradeKind        uint8 = 0x22 // a barricade cannot be raised
	fortressErrUpgradeFortress    uint8 = 0x23 // the zone belongs to another fortress
	fortressErrUpgradeIndex       uint8 = 0x24 // a type index past 3
	fortressErrUpgradeRole        uint8 = 0x33 // not a commander or battle commander
	fortressErrUpgradeLevel       uint8 = 0x34 // below the target's level
	fortressErrUpgradeGold        uint8 = 0x35 // not enough gold
	fortressErrUpgradeGuildPoints uint8 = 0x36 // not enough guild points
	fortressErrUpgradeTooSoon     uint8 = 0x37 // still within its build minutes
	fortressErrUpgradeDamaged     uint8 = 0x39 // UIIT_MSG_FORT_BATTLEAIDE_UPGRADE_FAIL4

	// structureUpgradeTargets is how many targets a table row names.
	structureUpgradeTargets = 4
	// structureUpgradeNone is the table's empty target.
	structureUpgradeNone = "xxx"
	// structureUpgradeFile is the client's siege structure upgrade table.
	structureUpgradeFile = "siegestructupgradedata.txt"
	// structureUpgradeTargetColumn is the first target's column.
	structureUpgradeTargetColumn = 3
	// msPerMinute converts a made time to the build minutes it is held to.
	msPerMinute = 60000
)

/*
================
structureUpgradeTable

Each enabled row's targets by the structure's codename; an empty string
is no target.
================
*/
type structureUpgradeTable map[string][structureUpgradeTargets]string

/*
================
loadStructureUpgrades

The enabled rows of siegestructupgradedata.txt: Service, the structure's
codename, its level-0 base, then the four targets.
================
*/
func loadStructureUpgrades(dir string) (structureUpgradeTable, error) {
	rows := enterworld.ReadTextdataFile(filepath.Join(dir, structureUpgradeFile))
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s is absent or empty", structureUpgradeFile)
	}
	table := structureUpgradeTable{}
	for i, r := range rows {
		if r[0] != "1" {
			continue
		}
		if len(r) < structureUpgradeTargetColumn+structureUpgradeTargets {
			return nil, fmt.Errorf("%s row %d is truncated", structureUpgradeFile, i+1)
		}
		var targets [structureUpgradeTargets]string
		for t := range targets {
			if code := strings.TrimSpace(r[structureUpgradeTargetColumn+t]); code != structureUpgradeNone {
				targets[t] = code
			}
		}
		table[strings.TrimSpace(r[1])] = targets
	}
	return table, nil
}

/*
================
structureUpgradeTarget

The reference the structure becomes at type index, or false when the table
names none (the original answers 3).
================
*/
func (rt *Runtime) structureUpgradeTarget(current monster.MonsterRef, index uint8) (monster.MonsterRef, bool) {
	targets, ok := rt.structureUpgrades[current.Codename]
	if !ok || int(index) >= len(targets) || targets[index] == "" {
		return monster.MonsterRef{}, false
	}
	return rt.Monsters.ReferenceByCodename(targets[index])
}

/*
================
fortressStructureUpgrade

The original's checks in order: an unknown zone (3), a zone structure with
no reference, a barricade, a war in progress for anything but the
headquarters, a type past 3, another fortress, no structure standing on the
zone (3), a damaged structure, no target in the table (3), a member who is
not commander or battle commander, below the target's level, short of the
current structure's gold, a guild short of its guild points, and a
structure still within its build minutes. Then the guild pays the points,
the member the gold, the target replaces the structure at full hit points
and is made now, and the reply names the fortress, the zone and the type.

INFERENCE: the level check compares the target's level column with the
member's level, the only level the target row carries.
================
*/
func (rt *Runtime) fortressStructureUpgrade(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	refuse := func(code uint8) OpResult { return fortressRefusal(siege.ActionUpgrade, code) }
	if rt.Fortresses == nil || rt.Monsters == nil {
		return refuse(fortressErrUnknown)
	}
	zone, index := request.Reference, request.Value8
	for fortressID, world := range rt.fortressWorlds() {
		base, occupied, found := rt.Monsters.StructureZone(division, world, zone)
		if !found {
			continue
		}
		switch {
		case base.RefObjID == 0:
			return refuse(fortressErrUpgradeNoReference)
		case base.TypeID4 == structureKindBarricade:
			return refuse(fortressErrUpgradeKind)
		case base.TypeID4 != structureKindHeadquarters && rt.Fortresses.WarActive(division):
			return refuse(fortressErrWarActive)
		case index > structureUpgradeTargets-1:
			return refuse(fortressErrUpgradeIndex)
		case fortressID != request.Fortress:
			return refuse(fortressErrUpgradeFortress)
		case !occupied:
			return refuse(fortressErrInvalid)
		}
		row, ok := rt.structureOnZone(division, world, zone)
		if !ok || row.Gid == 0 || row.CurrentHP == 0 || row.StructureState&structureDestroyedMask != 0 {
			return refuse(fortressErrInvalid)
		}
		if row.CurrentHP != row.Ref.MaxHP {
			return refuse(fortressErrUpgradeDamaged)
		}
		target, ok := rt.structureUpgradeTarget(row.Ref, index)
		if !ok || c == nil {
			return refuse(fortressErrInvalid)
		}
		if role := rt.guildFortressRole(division, c); role != fortressRoleCommander && role != fortressRoleBattleCommander {
			return refuse(fortressErrUpgradeRole)
		}
		if int64(target.Level) > rewardLevel(c) {
			return refuse(fortressErrUpgradeLevel)
		}
		if c.Gold == nil || *c.Gold < int64(row.Ref.UpgradeGold) {
			return refuse(fortressErrUpgradeGold)
		}
		if rt.guildPointsOf(division, c) < uint64(row.Ref.UpgradeGuildPoints) {
			return refuse(fortressErrUpgradeGuildPoints)
		}
		now := rt.Now().UnixMilli()
		if minutes := (now - rt.structureMadeAtMs(division, fortressID, zone)) / msPerMinute; minutes < int64(row.Ref.BuildMinutes) {
			return refuse(fortressErrUpgradeTooSoon)
		}
		paid, refusal := rt.payStructureUpgrade(division, c, row.Ref)
		if refusal != 0 {
			return refuse(refusal)
		}
		if !rt.Monsters.SetStructureOccupant(division, world, zone, target.RefObjID, now) {
			return prependOpResult(paid, refuse(fortressErrUnknown))
		}
		built, _ := rt.structureOnZone(division, world, zone)
		record, _ := rt.Fortresses.Get(division, fortressID)
		rt.storeStructureRow(division, domain.FortressStructureRecord{FortressID: fortressID, EventStructID: zone,
			RefObjID: target.RefObjID, OwnerGuildID: record.Holder(), HP: built.CurrentHP, MadeAtMs: now})
		// The fortress world hears the new structure, as after the reference
		// server's upgrade result (fortress_roster.go).
		rt.pushStructureRoster(division, fortressID, world)
		reply := wire.NewWriter(11).U8(siege.ActionUpgrade).U8(1).U32(fortressID).U32(zone).U8(index)
		return prependOpResult(paid, OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: reply.Payload()}}})
	}
	return refuse(fortressErrInvalid)
}

/*
================
upgradeSucceeded

Whether an upgrade's answer is a success (the GM command reports it).
================
*/
func upgradeSucceeded(res OpResult) bool {
	for _, f := range res.Frames {
		if f.Opcode == opFortressInteractionResult && len(f.Payload) >= 2 && f.Payload[0] == siege.ActionUpgrade {
			return f.Payload[1] == 1
		}
	}
	return false
}

/*
================
payStructureUpgrade

The guild pays the current structure's guild points and the member its
gold. The points go first: a guild that can no longer pay refuses before
any gold moves, and gold that can no longer be paid gives the points back.
================
*/
func (rt *Runtime) payStructureUpgrade(division string, c *enterworld.Character, current monster.MonsterRef) (OpResult, uint8) {
	var paid OpResult
	if current.UpgradeGuildPoints > 0 {
		var ok bool
		if paid, ok = rt.adjustGuildPoints(division, c, -int64(current.UpgradeGuildPoints)); !ok {
			return OpResult{}, fortressErrUpgradeGuildPoints
		}
	}
	cost := int64(current.UpgradeGold)
	if !rt.deps.Update(c, "fortress-structure-upgrade", func() bool {
		if c.Gold == nil || *c.Gold < cost {
			return false
		}
		left := *c.Gold - cost
		c.Gold = &left
		return true
	}) {
		if current.UpgradeGuildPoints > 0 {
			rt.adjustGuildPoints(division, c, int64(current.UpgradeGuildPoints))
		}
		return OpResult{}, fortressErrUpgradeGold
	}
	paid.Frames = append([]wire.Frame{goldFrame(rt.characterSnapshot(division, c))}, paid.Frames...)
	return paid, 0
}

/*
================
guildPointsOf

The guild points of the member's guild, 0 without one.
================
*/
func (rt *Runtime) guildPointsOf(division string, c *enterworld.Character) uint64 {
	store := rt.guildStore()
	if store == nil || c.GuildID == nil || *c.GuildID == 0 {
		return 0
	}
	record, _, ok := store.Guild(division, *c.GuildID)
	if !ok {
		return 0
	}
	return uint64(record.GP)
}

/*
================
adjustGuildPoints

Moves the member's guild points by delta (a debit refuses below zero) and
tells the members; false when nothing moved.
================
*/
func (rt *Runtime) adjustGuildPoints(division string, c *enterworld.Character, delta int64) (OpResult, bool) {
	store := rt.guildStore()
	if store == nil {
		return OpResult{}, false
	}
	snapshot, refusal := store.UpdateGuildAs(division, c.ID, "fortress-structure-upgrade", domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			next := int64(g.GP) + delta
			if next < 0 {
				return g, m, false
			}
			g.GP = uint32(next)
			return g, m, true
		})
	if refusal.Refused() {
		return OpResult{}, false
	}
	update := wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeGuildGp3B29(snapshot.Guild.GP)}
	rt.pushGuildMembers(division, snapshot.Guild.ID, snapshot.Members, c.ID, []wire.Frame{update})
	return OpResult{Frames: []wire.Frame{update}}, true
}

/*
================
structureMadeAtMs

When the structure on a zone was made, from its stored row; 0 (long ago)
for a structure that was never raised.
================
*/
func (rt *Runtime) structureMadeAtMs(division string, fortressID, zone uint32) int64 {
	rt.fortressPersistMu.Lock()
	defer rt.fortressPersistMu.Unlock()
	return rt.fortressPersist.saved[savedStructureKey{division, fortressID, zone}].MadeAtMs
}
