/*
===========================================================================

entry.ts - the simulation worker's port to its page

The worker module's entry: it starts the simulation and owns the two
directions of its message port. Host messages go to the simulation as they
arrive. Worker messages are posted as the simulation hands them over, except
a world batch, whose walking paths leave as their change from the ones last
posted (foundation/gameplay/walking-path-wire.ts): the port is where a
structured clone is paid, so the port is where it is spared.

===========================================================================
*/
import { createSimulation } from "./simulation";
import type { HostMessage, WorkerMessage } from "@/engine/contracts/simulation";
import { createWalkingPathMemory, packWalkingPaths } from "@/engine/foundation/gameplay/walking-path-wire";

const port = globalThis as unknown as {
	onmessage: (( event: MessageEvent<HostMessage> ) => void) | null;
	postMessage( message: WorkerMessage, transfer: Transferable[] ): void;
};
// The walking paths as the host last received them, one per character.
const postedWalkingPaths = createWalkingPathMemory();

/*
================
sendToHost
================
*/
function sendToHost( message: WorkerMessage, transfer: Transferable[] ) {
	if ( message.kind === "world" ) {
		port.postMessage( { kind: "world", ...packWalkingPaths( message.batch, postedWalkingPaths ) }, transfer );
		return;
	}
	port.postMessage( message, transfer );
}

const simulation = createSimulation( sendToHost );
port.onmessage = event => simulation.receive( event.data );
