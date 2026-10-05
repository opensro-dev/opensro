/*
===========================================================================

monster_archive.go - dormant monsters, kept compact in memory

A monster in a region no player is near sleeps (monster_dormancy.go): its
resident row leaves the hot map for this cold form. The cold form is the
exact snapshot (put refuses anything it cannot reproduce: commands, wounds,
effects, any future Instance field) with the two large rows interned. The
reference row (MonsterRef) and the nest row (NestRow, about 2.8 KB as JSON
with its conditional skills) are shared by every monster of a kind and of a
nest, so a sleeper costs about 80 bytes.

It used to be a JSON record in an 8 KB slot of a temp file: 404 MB of disk
per start for the world's ~51,000 monsters, two allocations and a write
system call per sleeper, and a file that outlived every process that was
stopped rather than shut down (seven gigabytes of them filled the
development disk, after which no monster could sleep).

===========================================================================
*/

package simulation

import (
	"fmt"
	"unique"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
monsterArchive

The switch that lets populations put monsters to sleep
(EnableDormantStorage). It owns no data: each population's storage holds
its own cold rows.
================
*/
type monsterArchive struct{}

/*
================
archivedMonster

A sleeping monster. rarity, hp and maxHP answer metadata and targeting
reads without rebuilding the snapshot.
================
*/
type archivedMonster struct {
	ref       unique.Handle[monster.MonsterRef]
	nest      unique.Handle[monster.NestRow]
	spawn     monster.SpawnPoint
	heading   uint16
	rarity    uint8
	hp, maxHP uint32
}

/*
================
EnableDormantStorage
================
*/
func (s *MonsterState) EnableDormantStorage() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.archive != nil {
		return fmt.Errorf("dormant storage already installed")
	}
	s.archive = &monsterArchive{}
	for _, key := range s.populationKeys() {
		s.populationForLease(key.division, key.lease).instances.archive = s.archive
	}
	return nil
}

/*
================
Close

Nothing outlives the process; kept for the shutdown sequence that calls it.
================
*/
func (s *MonsterState) Close() error {
	return nil
}

/*
================
put

The cold form of row, or false when it would lose anything: a unique
(rarity 3) never sleeps, and neither does a monster with state beyond its
reference, nest, spawn, heading and HP.
================
*/
func (a *monsterArchive) put(row monster.Instance) (archivedMonster, bool) {
	if row.Rarity()&15 == 3 {
		return archivedMonster{}, false
	}
	cold := archivedMonster{
		ref:     unique.Make(row.Ref),
		nest:    unique.Make(row.Nest),
		spawn:   row.Spawn,
		heading: row.SpawnHeading,
		rarity:  row.Rarity(),
		hp:      row.CurrentHP,
		maxHP:   row.EffectiveMaxHP(),
	}
	if a.get(row.Gid, cold) != row {
		return archivedMonster{}, false
	}
	return cold, true
}

/*
================
get

The exact snapshot of the sleeping monster gid.
================
*/
func (a *monsterArchive) get(gid uint32, r archivedMonster) monster.Instance {
	return monster.Instance{
		Gid:          gid,
		Ref:          r.ref.Value(),
		Nest:         r.nest.Value(),
		Spawn:        r.spawn,
		SpawnHeading: r.heading,
		CurrentHP:    r.hp,
	}
}
