/*
===========================================================================

cosownerindex.go - the pet gid to owner index

One question used to cost a walk of every character in a division:
whose pet is this gid? characterByCosGID answered it per attack and
per abnormal landing on a pet, so players times pets times hits grew
quadratically in a crowd (#2138; a Gourd wave is exactly that crowd).

The index answers from a map. It is maintained at the doors every
companion's runtime life already passes: bindCompanionSession notes
a pet when its session binds (world entry, summon, mercenary call),
and the retirement doors forget it (retireCosRuntime on a despawn or
death, forgetPetSession when the owner leaves). The character store
stays authoritative: a lookup verifies its hit against the owner's
own records and heals or falls back to the division walk when the
index cannot vouch for the gid, so a missed door costs one walk, not
a wrong owner. Gids are reused; a later pet must never inherit a
dead pet's owner.

===========================================================================
*/
package action

import (
	"sync"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
cosOwnerKey

A summoned pet's world identity: the division it stands in and the
gid it was given. The same gid in another division is another pet.
================
*/
type cosOwnerKey struct {
	division string
	gid      uint32
}

/*
================
cosOwnerIndex

Division-and-gid to owning character for every live summoned pet.
The mutex never spans a character door, a movement constraint or a
packet publish: each operation is one map touch. The zero value is
ready to use.
================
*/
type cosOwnerIndex struct {
	mu     sync.RWMutex
	owners map[cosOwnerKey]*enterworld.Character
}

/*
================
note

Record that owner's pet stands in the division under gid. Called
when a companion session binds, so entry, summon and mercenary pets
are all noted at the one door. A rebind of the same gid overwrites:
the latest binding is the live one.
================
*/
func (index *cosOwnerIndex) note(division string, gid uint32, owner *enterworld.Character) {
	if gid == 0 || owner == nil {
		return
	}
	key := cosOwnerKey{division: division, gid: gid}
	index.mu.Lock()
	if index.owners == nil {
		index.owners = make(map[cosOwnerKey]*enterworld.Character)
	}
	index.owners[key] = owner
	index.mu.Unlock()
}

/*
================
forget

One pet left the world. Retirement forgets unconditionally: a later
pet reusing the gid must not resolve to this entry.
================
*/
func (index *cosOwnerIndex) forget(division string, gid uint32) {
	if gid == 0 {
		return
	}
	index.mu.Lock()
	delete(index.owners, cosOwnerKey{division: division, gid: gid})
	index.mu.Unlock()
}

/*
================
forgetOwner

Every pet of one owner leaves a division (the owner logged out or
moved): drop each entry naming that character there. Runs at owner
teardown, never per tick, so the scan costs nothing measurable.
================
*/
func (index *cosOwnerIndex) forgetOwner(division string, owner *enterworld.Character) {
	if owner == nil {
		return
	}
	index.mu.Lock()
	for key, current := range index.owners {
		if key.division == division && current == owner {
			delete(index.owners, key)
		}
	}
	index.mu.Unlock()
}

/*
================
owner

The character the index holds for the gid, or nil. The caller
verifies the hit against the character's own records before acting
on it (characterByCosGID in cosabnormal.go).
================
*/
func (index *cosOwnerIndex) owner(division string, gid uint32) *enterworld.Character {
	if gid == 0 {
		return nil
	}
	index.mu.RLock()
	owner := index.owners[cosOwnerKey{division: division, gid: gid}]
	index.mu.RUnlock()
	return owner
}
