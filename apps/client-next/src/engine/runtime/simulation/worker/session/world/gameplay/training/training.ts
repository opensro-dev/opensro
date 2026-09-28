/*
===========================================================================

training.ts - one outstanding learned-rank transaction per session

Training and restoration share an unnumbered receipt channel. A timeout
cannot authorize a retry: the server may already have spent the resources.
Only a matching receipt or a new world session clears that uncertainty.

===========================================================================
*/
import { trainingTransition, type TrainingState } from "@/engine/foundation/gameplay/training";
import type { WireFrame } from "@/engine/contracts/network";

/*
================
createTraining
================
*/
export function createTraining( send: ( frame: WireFrame ) => void ) {
	let state: TrainingState = { phase: "idle" };
	return {
		/*
		================
		request

		The native response mirrors the request's low opcode bits.
		================
		*/
		request( frame: WireFrame, id: number, now: number ) {
			const next = trainingTransition( state, { type: "sent", opcode: frame.opcode + 0x4000, id, now } );
			send( frame );
			state = next;
			return frame;
		},
		/*
		================
		accepts

		Only the pending operation may authorize an ambiguous old-ID removal
		receipt. A repeated success must not remove the newly restored rank.
		================
		*/
		accepts( opcode: number, id?: number ) {
			return state.phase !== "idle" && state.opcode === opcode && (id === undefined || state.id === id);
		},
		/*
		================
		receipt
		================
		*/
		receipt( opcode: number, id?: number ) {
			state = trainingTransition( state, { type: "receipt", opcode, id } );
		},
		/*
		================
		step
		================
		*/
		step( now: number ) {
			const next = trainingTransition( state, { type: "tick", now } );
			if ( next === state ) return false;
			state = next;
			return true;
		},
		/*
		================
		state
		================
		*/
		state() {
			return {
				trainingPending: state.phase !== "idle",
				trainingError: state.phase === "uncertain" ?
					"Training result is unknown. Reconnect before trying again." :
					null
			};
		},
		/*
		================
		reset
		================
		*/
		reset() {
			state = trainingTransition( state, { type: "reset" } );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			state = { phase: "idle" };
		}
	};
}
