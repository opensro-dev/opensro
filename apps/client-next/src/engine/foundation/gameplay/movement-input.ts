/*
===========================================================================

movement-input.ts - the main thread's movement input sequence

Render-thread movement (epic #1437, part #1441a). Every movement input the
main thread sends to the simulation worker - a ground click ("ground-move")
or a direct destination ("move") - carries a monotonically increasing
sequence number. The worker records the last one it processed and
publishes it with the local movement state (movementInputSeq), so the
main-thread mover can later tell which of its inputs the published pose
already includes and replay only the rest (Unreal's saved moves).

The sequence is main-to-worker only: the server wire is unchanged.

===========================================================================
*/
import type { GameplayCommand } from "@/engine/contracts/gameplay";

/*
================
isMovementInput

Whether a gameplay command is a movement input the sequence numbers.
================
*/
export function isMovementInput( command: GameplayCommand ): command is Extract<
	GameplayCommand,
	{ kind: "ground-move" | "move"; }
> {
	return command.kind === "ground-move" || command.kind === "move";
}

/*
================
createMovementInputSequence

One owner per simulation host. stamp() returns the command with its input
sequence number when it is a movement input, and the command unchanged
otherwise. last() is the newest number issued (0 before any).
================
*/
export function createMovementInputSequence() {
	let last = 0;
	return {
		stamp( command: GameplayCommand ): GameplayCommand {
			if ( !isMovementInput( command ) ) return command;
			return { ...command, inputSeq: ++last };
		},
		last(): number {
			return last;
		}
	};
}
