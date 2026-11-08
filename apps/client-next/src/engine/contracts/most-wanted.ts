/*
===========================================================================

most-wanted.ts - the local player's most-wanted state

Port-only, not native (server SRO_MOST_WANTED). The worker decodes the
port-only 0x3FBA frame into this shape (runtime/.../most-wanted/most-wanted.ts);
the gameplay snapshot carries the latest one and the buff board draws
its row. It lives apart from gameplay.ts because nothing else in the
gameplay plane reads it: one optional feature, one contract.

===========================================================================
*/

/*
================
MostWantedState

The local player's wanted state as the latest frame carried it
(port-only 0x3FBA; most-wanted.ts). Level 0 (or no state yet) draws no
buff-board row. killsIntoLevel counts toward the next level and equals
killsPerLevel at the level cap; bounty is the gold a claim on this
level pays, priced by the server; decayInMs is how long remained
until the next decay step when the frame was sent (0 when none is
stamped), counted down from receivedAtMs on the worker's frame clock.
No wall clock is ever consulted: the server owns the decay, and the
row only changes when a new state arrives.
================
*/
export interface MostWantedState {
	readonly level: number;
	readonly killsIntoLevel: number;
	// The job suit worn as the frame was sent (0 none, 1 trader,
	// 2 thief, 3 hunter); the row wears that job's icon.
	readonly dressedJob: number;
	readonly killsPerLevel: number;
	readonly bounty: number;
	readonly decayInMs: number;
	readonly receivedAtMs: number;
}
