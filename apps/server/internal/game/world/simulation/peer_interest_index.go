package simulation

import (
	worldgeom "opensro.online/server/internal/game/world"
	"sort"
)

type peerInterestKey struct {
	division string
	world    uint32
	block    worldgeom.InterestBlock
}

type peerVisibilityKey struct {
	session          string
	character        int64
	interest         peerInterestKey
	appearance, live bool
}

// Spawn/despawn visibility only changes with membership, instance or block.
// Movement packets and combat still run every tick in their existing lanes.
// This exact key comparison avoids hashing collisions and stale cross-tick poses.
func (state *divisionTickState) peerVisibilityChanged(sessions []SessionSnapshot, now int64, live map[string]bool) bool {
	previous := len(state.peerVisibilityKeys)
	changed := !state.peerVisibilityValid || previous != len(sessions)
	for i := range sessions {
		s := &sessions[i]
		key := peerVisibilityKey{s.SessionID, s.CharacterID, peerInterestKey{s.DivisionID, s.WorldInstance, peerInterestBlock(s.World.LiveSpawnAt(now))}, s.Appearance != nil, live[s.SessionID]}
		if i < previous {
			if state.peerVisibilityKeys[i] != key {
				changed = true
				state.peerVisibilityKeys[i] = key
			}
		} else {
			state.peerVisibilityKeys = append(state.peerVisibilityKeys, key)
		}
	}
	if len(sessions) < previous {
		clear(state.peerVisibilityKeys[len(sessions):])
		state.peerVisibilityKeys = state.peerVisibilityKeys[:len(sessions)]
	}
	return changed
}

// Tick-local derived index over detached session snapshots. Links are input
// indexes plus one (zero is absent), avoiding one allocation per occupied cell.
// Nothing survives the visibility pass or owns mutable character state.
type peerInterestIndex struct {
	heads map[peerInterestKey]int
	next  []int
	poses []Spawn
}

func buildPeerInterestIndex(sessions []SessionSnapshot, now int64, pets bool) peerInterestIndex {
	var index peerInterestIndex
	for i := len(sessions) - 1; i >= 0; i-- {
		s := &sessions[i]
		if s.Appearance == nil {
			continue
		}
		world := s.World
		if pets {
			if s.COS == nil || s.COS.Row.Gid == 0 {
				continue
			}
			world = s.COS.World
		}
		if index.heads == nil {
			index = peerInterestIndex{heads: make(map[peerInterestKey]int), next: make([]int, len(sessions)), poses: make([]Spawn, len(sessions))}
		}
		pose := world.LiveSpawnAt(now)
		index.poses[i] = pose
		key := peerInterestKey{s.DivisionID, s.WorldInstance, peerInterestBlock(pose)}
		index.next[i] = index.heads[key]
		index.heads[key] = i + 1
	}
	return index
}

func peerInterestBlock(p Spawn) worldgeom.InterestBlock {
	return worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})
}

func (index peerInterestIndex) candidates(viewer *SessionSnapshot, pose Spawn, scratch []int) []int {
	scratch = scratch[:0]
	if len(index.heads) == 0 {
		return scratch
	}
	key := peerInterestKey{viewer.DivisionID, viewer.WorldInstance, peerInterestBlock(pose)}
	appendCell := func(key peerInterestKey) {
		for link := index.heads[key]; link != 0; link = index.next[link-1] {
			scratch = append(scratch, link-1)
		}
	}
	// The same relation as worldgeom.InterestBlock.Adjacent, cell by cell:
	// the 3x3 block neighbourhood outdoors and in a gridded dungeon. A Whole
	// dungeon block (no grid cell) neighbours its whole region both ways, so
	// a Whole viewer reads every cell of the region and a placed viewer also
	// reads the region's Whole cell.
	switch {
	case key.block.Whole:
		for other := range index.heads {
			if other.division == key.division && other.world == key.world &&
				other.block.Dungeon == key.block.Dungeon {
				appendCell(other)
			}
		}
	default:
		for z := -1; z <= 1; z++ {
			for x := -1; x <= 1; x++ {
				neighbor := key
				neighbor.block.X += x
				neighbor.block.Z += z
				appendCell(neighbor)
			}
		}
		if key.block.Dungeon != 0 {
			whole := key
			whole.block = worldgeom.InterestBlock{Dungeon: key.block.Dungeon, Whole: true}
			appendCell(whole)
		}
	}
	// Preserve the original snapshot order across cell boundaries.
	sort.Ints(scratch)
	return scratch
}
