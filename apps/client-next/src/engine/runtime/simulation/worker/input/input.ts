/*
===========================================================================

input.ts - the worker's side of the input contract

Receives the display thread's sequenced key and focus-release batches,
validates each batch whole before taking any of it, and keeps the held-key
set the simulation reads. Camera input never reaches the worker
(contracts/input.ts).

===========================================================================
*/
import type { InputBatch, InputCommand, SimulationInput } from "@/engine/contracts/input";

const MAX_BATCH_COMMANDS = 512;
const MAX_PENDING_COMMANDS = 2048;
const MAX_KEY_CODE_LENGTH = 64;

/*
================
createSimulationInput
================
*/
export function createSimulationInput(): SimulationInput {
	let received = 0, accepted = 0;
	const pending: InputCommand[] = [];
	const keys = new Set<string>();
	return {
		/*
		================
		receive
		================
		*/
		receive( batch: InputBatch ) {
			if (
				!batch.commands.length || batch.commands.length > MAX_BATCH_COMMANDS ||
				pending.length + batch.commands.length > MAX_PENDING_COMMANDS
			) throw new Error( "Invalid simulation input batch size" );
			if ( batch.first !== received + 1 || batch.last !== batch.first + batch.commands.length - 1 ) {
				throw new Error( "Input sequence gap" );
			}
			for ( let i = 0; i < batch.commands.length; i++ ) {
				const c = batch.commands[i]!;
				if ( c.sequence !== batch.first + i || !Number.isFinite( c.timeMs ) ) {
					throw new Error( "Invalid input command" );
				}
				if ( c.kind === "key" ) {
					if (
						typeof c.code !== "string" || c.code.length > MAX_KEY_CODE_LENGTH || typeof c.down !== "boolean"
					) {
						throw new Error( "Invalid key input" );
					}
				} else if ( c.kind !== "release" ) throw new Error( "Unknown input kind" );
			}
			pending.push( ...batch.commands );
			received = batch.last;
		},
		/*
		================
		commit
		================
		*/
		commit() {
			for ( const command of pending ) {
				if ( command.kind === "release" ) keys.clear();
				else if ( command.down ) keys.add( command.code );
				else keys.delete( command.code );
				accepted = command.sequence;
			}
			pending.length = 0;
			return accepted;
		},
		lastAccepted: () => accepted
	};
}
