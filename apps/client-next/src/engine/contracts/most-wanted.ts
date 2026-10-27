/*
===========================================================================

most-wanted.ts - the local player's Most Wanted state

Port-only, not native, off by default (server SRO_MOST_WANTED, client
Experimental row). The worker decodes the port-only 0x3FB2 frame into
this shape (runtime/.../most-wanted/most-wanted.ts); the gameplay
snapshot carries the latest one and the buff board draws its row. It
lives apart from gameplay.ts because nothing else in the gameplay plane
reads it: one optional feature, one contract.

===========================================================================
*/

/*
================
MostWantedState

The local player's wanted state as the server last reported it: its
level (1-10), the counted kills toward the next level out of
killsPerLevel, the dressed job (1 trader, 2 thief, 3 hunter) whose icon
the row wears, and the damage dealt and reduced at this level. A cleared
streak never reaches this shape: the worker publishes null instead.
================
*/
export interface MostWantedState {
	readonly level: number;
	readonly killsIntoLevel: number;
	readonly killsPerLevel: number;
	readonly job: number;
	readonly damagePercent: number;
}
