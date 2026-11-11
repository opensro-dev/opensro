/*
===========================================================================

monster_statics.go - the division's static interest rows

A region's statics are its byRegion entries that have no mover: the
interest scan probes them per viewer answer (appendInterestStatics,
monster_worlds.go), so each region's row is built once under the
statics stamp and kept while the stamp stands. The stamp is the
division's mover roster generation plus its byRegion generation, so
any spawn, despawn, listing or unlisting anywhere retires every row
- and each row then rebuilds only on its own region's next read.

===========================================================================
*/

package simulation

/*
================
interestStaticsStamp

movers is compared too: a population can be handed a fresh storage whose
counters restart.
================
*/
type interestStaticsStamp struct {
	movers   *moverStorage
	roster   uint64
	byRegion uint64
}

/*
================
interestStaticsNow
================
*/
func (state *divisionMonsterState) interestStaticsNow() interestStaticsStamp {
	stamp := interestStaticsStamp{movers: state.movers, byRegion: state.byRegionGeneration}
	if state.movers != nil {
		stamp.roster = state.movers.blocks.rosterGeneration
	}
	return stamp
}

/*
================
regionStatics

The entries of byRegion[region] that have no mover. Most generated actors
have a mover once their population is active, so this keeps a scan from
probing each of them per viewer. A duplicate listing stays duplicated,
as byRegion lists it.

One region's row is built on its first read under the current statics
stamp and kept while the stamp stands; a moved stamp drops every row
unbuilt. The stamp moves on any roster or listing change anywhere in
the division, and rebuilding every region on the first read after such
a move probed the whole division's listings for the sake of the few
regions a viewer's ring actually asks (#3027).
================
*/
func (state *divisionMonsterState) regionStatics(region uint16) []uint32 {
	now := state.interestStaticsNow()
	if state.interestStaticsStamp != now {
		state.interestStatics = nil
		state.interestStaticsStamp = now
	}
	if rows, ok := state.interestStatics[region]; ok {
		return rows
	}
	var rows []uint32
	for _, gid := range state.byRegion[region] {
		if !state.movers.has(gid) {
			rows = append(rows, gid)
		}
	}
	if state.interestStatics == nil {
		state.interestStatics = make(map[uint16][]uint32)
	}
	state.interestStatics[region] = rows
	return rows
}
