/*
===========================================================================

movement-input.ts - the main thread's gameplay input sequence

Render-thread movement (epic #1437, part #1441a). Every gameplay command
the main thread sends to the simulation worker carries a monotonically
increasing sequence number on its session envelope: not only ground
clicks, because a select, a pickup, a mount or an attack or skill run-up
can start a local walk too. The worker records the number where it first
takes the command - before any readiness gate can drop it - and publishes
the last one with the local movement state (movementInputSeq), so the
main-thread mover can tell which of its inputs the published pose already
reflects and replay only the rest (Unreal's saved moves).

The sequence is main-to-worker only: the server wire is unchanged.

===========================================================================
*/

/*
================
createInputSequence

One owner per simulation host. next() issues the following number;
last() is the newest issued (0 before any).
================
*/
export function createInputSequence() {
	let last = 0;
	return {
		next(): number {
			return ++last;
		},
		last(): number {
			return last;
		}
	};
}
