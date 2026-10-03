/*
===========================================================================

world-replay.mjs - deterministic replay of the world core and presentation

Drives the worker's world core, the presentation projection and both input
owners from a journal of events (packets, commands, ticks, deliveries,
display input), so a recorded session can be checkpointed, restored and
compared witness for witness. Sources are bundled once and hashed, so a
checkpoint names the exact code it was taken with.

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { build } from "esbuild";
import { root } from "./project.mjs";
import { createHash } from "node:crypto";
const digest = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );

const limit = 64 << 20;
/*
================
canonical

Sorted-key, finite, plain-data form of a value, for hashing and witnesses.
================
*/
function canonical( value ) {
	if ( value === null || typeof value === "string" || typeof value === "boolean" ) return value;
	if ( typeof value === "number" ) {
		if ( !Number.isFinite( value ) ) throw new Error( "Non-finite replay value" );
		return value;
	}
	if ( ArrayBuffer.isView( value ) ) return Array.from( value );
	if ( Array.isArray( value ) ) return value.map( canonical );
	if ( value && typeof value === "object" ) {
		return Object.fromEntries(
			Object.keys( value ).sort().filter( key => value[key] !== undefined ).map(
				key => [ key, canonical( value[key] ) ]
			)
		);
	}
	throw new Error( "Non-serializable replay value" );
}
const encode = value => JSON.stringify( canonical( value ) );
let loaded;
/*
================
loadCore

Bundles the replayed modules once and hashes their sources.
================
*/
async function loadCore() {
	if ( !loaded ) {
		loaded = (async () => {
			const bundle = await build( {
				absWorkingDir: root,
				stdin: {
					contents:
						"export {createWorldCore} from './src/engine/runtime/simulation/worker/session/world/core'; export {createPresentation} from './src/engine/runtime/presentation/presentation'; export {createSimulationInput} from './src/engine/runtime/simulation/worker/input/input'; export {createInput} from './src/engine/runtime/input/input';",
					resolveDir: root,
					sourcefile: "replay-entry.ts"
				},
				bundle: true,
				platform: "node",
				format: "esm",
				write: false,
				metafile: true
			} );
			const sources = Object.keys( bundle.metafile.inputs ).filter( file => file !== "replay-entry.ts" ).sort()
				.map( file => [ file, digest( fs.readFileSync( path.resolve( root, file ) ) ) ] );
			const module = await import(
				"data:text/javascript;base64," + Buffer.from( bundle.outputFiles[0].contents ).toString( "base64" )
			);
			return {
				factory: module.createWorldCore,
				projection: module.createPresentation,
				inputFactory: module.createSimulationInput,
				cameraFactory: module.createInput,
				sourceHash: digest( encode( sources ) )
			};
		})();
	}
	return loaded;
}
// A checkpoint is a bounded genesis-to-cursor event prefix, including publication
// and acknowledgement order. Re-execution reconstructs private pending queues,
// movement IDs and clocks, rather than copying only the last visible snapshot.
/*
================
createWorldReplay

A replay, empty or restored from checkpoint.
================
*/
export async function createWorldReplay( checkpoint ) {
	const { factory, projection, inputFactory, cameraFactory, sourceHash } = await loadCore();
	let time = -Infinity, size = 0, disposed = false;
	const events = [], observations = [], sent = [], presented = [], native = [];
	const presentation = projection(), simulationInput = inputFactory(), cameraInput = cameraFactory();
	let presentedSequence = 0, pendingDelivery = null;
	/*
	================
	retain
	================
	*/
	function retain( list, value ) {
		const bytes = encode( value ).length * 2;
		if ( size + bytes > limit ) throw new Error( "Replay witness exceeds budget" );
		size += bytes;
		list.push( value );
	}
	const core = factory( frame =>
		retain( sent, { atMs: time, opcode: frame.opcode, payload: Array.from( frame.payload ) } )
	);
	/*
	================
	apply

	Applies one journal event; events must not go back in time.
	================
	*/
	function apply( input ) {
		if ( disposed ) throw new Error( "Replay disposed" );
		const text = encode( input );
		if ( size + text.length * 2 > limit || events.length >= 100000 ) {
			throw new Error( "Replay checkpoint exceeds budget" );
		}
		const event = JSON.parse( text );
		if ( !Number.isFinite( event.atMs ) || event.atMs < 0 || event.atMs < time ) {
			throw new Error( "Replay clock/order violation" );
		}
		size += text.length * 2;
		time = event.atMs;
		try {
			switch ( event.kind ) {
				case "bootstrap":
					core.bootstrap( event.value );
					break;
				case "packet": {
					if (
						!Number.isInteger( event.opcode ) || event.opcode < 0 || event.opcode > 65535 ||
						!Array.isArray( event.payload ) || event.payload.length > (1 << 20) || event.payload.some( n =>
							!Number.isInteger( n ) || n < 0 || n > 255
						)
					) throw new Error( "Invalid replay packet" );
					core.receive( { opcode: event.opcode, payload: Uint8Array.from( event.payload ) }, time );
					break;
				}
				case "command":
					core.command( event.command, time );
					break;
				case "input": {
					// Raw display input: the display owner moves the camera and drains the
					// keys and focus releases the worker receives (contracts/input.ts).
					if ( !Array.isArray( event.events ) ) throw new Error( "Invalid replay input" );
					for ( const raw of event.events ) cameraInput.accept( raw );
					if ( cameraInput.error() ) throw new Error( cameraInput.error() );
					const drained = cameraInput.drain();
					if ( drained ) simulationInput.receive( drained );
					break;
				}
				case "commit-input":
					simulationInput.commit( command => core.command( command, time ) );
					break;
				case "tick":
					if ( typeof event.advance !== "boolean" ) throw new Error( "Missing replay tick phase" );
					core.step( time, event.advance );
					break;
				case "take": {
					const batch = core.take();
					if ( batch ) pendingDelivery = batch;
					retain( observations, { atMs: time, batch: canonical( batch ) } );
					break;
				}
				case "present": {
					const batch = pendingDelivery;
					if ( !batch || batch.sequence !== event.sequence ) {
						throw new Error( "Presentation does not match outstanding world batch" );
					}
					const reset = presentation.apply( batch );
					presentedSequence = batch.sequence;
					retain( presented, { atMs: time, sequence: batch.sequence, reset } );
					break;
				}
				case "drain-native":
					retain( native, { atMs: time, events: canonical( presentation.takeNative() ) } );
					break;
				case "ack":
					core.ack( event.sequence );
					if ( pendingDelivery?.sequence === event.sequence ) pendingDelivery = null;
					break;
				case "reset-gameplay":
					core.resetGameplay();
					break;
				case "clear":
					core.clear();
					break;
				default:
					throw new Error( "Unknown replay event" );
			}
		} catch ( error ) {
			disposed = true;
			core.dispose();
			presentation.dispose();
			throw error;
		}
		events.push( event );
	}
	/*
	================
	witness
	================
	*/
	function witness() {
		if ( disposed ) throw new Error( "Replay disposed" );
		return canonical( {
			observations,
			sent,
			count: core.count(),
			synchronized: core.synchronized(),
			input: { accepted: simulationInput.lastAccepted(), camera: cameraInput.camera() },
			presentation: {
				sequence: presentedSequence,
				entities: presentation.entities(),
				gameplay: presentation.gameplay(),
				bootstrap: presentation.bootstrap(),
				presented,
				native
			}
		} );
	}
	/*
	================
	save
	================
	*/
	function save() {
		if ( disposed ) throw new Error( "Replay disposed" );
		const body = { version: 2, scope: "admitted-client-world", sourceHash, events, witness: witness() };
		return JSON.parse( encode( { ...body, sha256: digest( encode( body ) ) } ) );
	}
	try {
		if ( checkpoint ) {
			const { sha256, ...body } = checkpoint;
			if (
				checkpoint.version !== 2 || checkpoint.scope !== "admitted-client-world" ||
				checkpoint.sourceHash !== sourceHash || sha256 !== digest( encode( body ) )
			) throw new Error( "Checkpoint version/source/integrity mismatch" );
			if ( !Array.isArray( checkpoint.events ) || encode( checkpoint ).length * 2 > limit ) {
				throw new Error( "Invalid checkpoint envelope" );
			}
			for ( const event of checkpoint.events ) apply( event );
			if ( encode( witness() ) !== encode( checkpoint.witness ) ) {
				throw new Error( "Restored world witness mismatch" );
			}
		}
	} catch ( error ) {
		core.dispose();
		presentation.dispose();
		throw error;
	}
	return {
		apply,
		checkpoint: save,
		witness,
		dispose() {
			disposed = true;
			core.dispose();
			presentation.dispose();
		}
	};
}
