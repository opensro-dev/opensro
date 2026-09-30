/*
===========================================================================

reservation.ts - the ground move a player clicked during their own cast

The server drops a ground command while the mover's skill action holds the
casting instance (CGObjChar_HandleMoveCommand 4B0EA0 via
CGObjPC_IsMotionChangeLocked 4EF880 and CGObjChar_IsAttackLocked 4AAB40).
Retail players click through a cast and walk as soon as it ends, so the
client holds the newest click here instead of predicting a move the server
will refuse, and gameplay replays it once the cast releases. Only the most
recent click is kept; death, travel and a world reset discard it.

===========================================================================
*/
import type { GameplayCommand } from "@/engine/contracts/gameplay";

type GroundCommand = Extract<GameplayCommand, { kind: "ground-move" | "move"; }>;

/*
================
createMoveReservation
================
*/
export function createMoveReservation() {
	let held: GroundCommand | null = null, failure: string | null = null;
	return {
		/*
================
hold

Replaces any earlier held click: the player's last choice wins.
================
		*/
		hold( command: GroundCommand ) {
			held = command;
			failure = null;
		},
		/*
================
take

Returns and releases the held click, or null.
================
		*/
		take(): GroundCommand | null {
			const command = held;
			held = null;
			return command;
		},
		/*
================
clear
================
		*/
		clear() {
			held = null;
			failure = null;
		},
		/*
================
fail

Records why the replayed click was refused, for the player's error line.
================
		*/
		fail( message: string ) {
			failure = message;
		},
		error: () => failure,
		holding: () => held !== null
	};
}
