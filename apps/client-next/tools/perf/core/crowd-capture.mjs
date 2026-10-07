/*
===========================================================================

crowd-capture.mjs - bounded, reusable character-frame inputs

Records renderer actors, not network commands or credentials. Capturing is
deliberately separate from timing: cloning and encoding perturb the frame.
Replay starts fresh renderer state; it cannot restore live particles or cloth.

===========================================================================
*/
export const CROWD_CAPTURE_VERSION = 1;
const MAX_DEPTH = 64;
const DEFAULT_FRAME_LIMIT = 1200;
const DEFAULT_BYTE_LIMIT = 64 * 1024 * 1024;
const TAG = "$crowd";

/*
================
encodeCrowdValue

Optional undefined fields and typed rotations must survive a JSON file. Reject
unsupported values instead of silently producing a different renderer input.
================
*/
export function encodeCrowdValue( value, depth = 0 ) {
	if ( depth > MAX_DEPTH ) throw Error( "Crowd capture nesting limit" );
	if ( value === undefined ) return { [TAG]: "undefined" };
	if ( typeof value === "number" ) {
		if ( !Number.isFinite( value ) ) throw Error( "Non-finite crowd input" );
		return Object.is( value, -0 ) ? { [TAG]: "negative-zero" } : value;
	}
	if ( value === null || typeof value === "boolean" || typeof value === "string" ) return value;
	if ( value instanceof Float32Array ) {
		return {
			[TAG]: "float32",
			values: Array.from( value, item => encodeCrowdValue( item, depth + 1 ) )
		};
	}
	if ( Array.isArray( value ) ) return value.map( item => encodeCrowdValue( item, depth + 1 ) );
	if ( typeof value !== "object" || ![ Object.prototype, null ].includes( Object.getPrototypeOf( value ) ) ) {
		throw Error( "Unsupported crowd input object" );
	}
	if ( Object.hasOwn( value, TAG ) ) throw Error( "Reserved crowd input key" );
	return Object.fromEntries(
		Object.entries( value ).map( ( [key, item] ) => [ key, encodeCrowdValue( item, depth + 1 ) ] )
	);
}

/*
================
decodeCrowdValue
================
*/
export function decodeCrowdValue( value, depth = 0 ) {
	if ( depth > MAX_DEPTH ) throw Error( "Crowd capture nesting limit" );
	if ( value === null || typeof value !== "object" ) {
		if ( typeof value === "number" && !Number.isFinite( value ) ) throw Error( "Non-finite crowd input" );
		return value;
	}
	if ( Array.isArray( value ) ) return value.map( item => decodeCrowdValue( item, depth + 1 ) );
	if ( Object.hasOwn( value, TAG ) ) {
		if ( value[TAG] === "undefined" ) return undefined;
		if ( value[TAG] === "negative-zero" ) return -0;
		if ( value[TAG] === "float32" && Array.isArray( value.values ) ) {
			const values = value.values.map( item => decodeCrowdValue( item, depth + 1 ) );
			if ( !values.every( item => typeof item === "number" && Number.isFinite( Math.fround( item ) ) ) ) {
				throw Error( "Invalid crowd float32 value" );
			}
			return Float32Array.from( values );
		}
		throw Error( "Unknown crowd input tag" );
	}
	return Object.fromEntries(
		Object.entries( value ).map( ( [key, item] ) => [ key, decodeCrowdValue( item, depth + 1 ) ] )
	);
}

/*
================
createCrowdCapture

Store encoded copies immediately: the runtime reuses actor and pose objects.
Limits stop before admitting a partial frame, retaining an explicit reason.
================
*/
export function createCrowdCapture(
	metadata,
	{ maxFrames = DEFAULT_FRAME_LIMIT, maxBytes = DEFAULT_BYTE_LIMIT } = {}
) {
	if ( !Number.isSafeInteger( maxFrames ) || maxFrames < 1 || maxFrames > DEFAULT_FRAME_LIMIT ) {
		throw Error( "Invalid crowd frame limit" );
	}
	if ( !Number.isSafeInteger( maxBytes ) || maxBytes < 1 || maxBytes > DEFAULT_BYTE_LIMIT ) {
		throw Error( "Invalid crowd byte limit" );
	}
	const header = encodeCrowdValue( metadata );
	const frames = [];
	let bytes = new TextEncoder().encode( JSON.stringify( header ) ).length;
	let reason = null, previousAtMs = -Infinity;
	if ( bytes > maxBytes ) throw Error( "Crowd metadata exceeds byte limit" );
	return {
		/*
		================
		append
		================
		*/
		append( frame ) {
			if ( reason ) return false;
			if ( !Number.isFinite( frame.atMs ) || frame.atMs <= previousAtMs || !Array.isArray( frame.actors ) ) {
				throw Error( "Invalid crowd frame clock or actors" );
			}
			const encoded = encodeCrowdValue( frame );
			const size = new TextEncoder().encode( JSON.stringify( encoded ) ).length;
			if ( bytes + size > maxBytes ) {
				reason = "byte-limit";
				return false;
			}
			frames.push( encoded );
			bytes += size;
			previousAtMs = frame.atMs;
			if ( frames.length === maxFrames ) reason = "frame-limit";
			return true;
		},
		/*
		================
		finish
		================
		*/
		finish() {
			reason ??= "stopped";
			return {
				version: CROWD_CAPTURE_VERSION,
				scope: "character-renderer",
				metadata: header,
				frames: frames.slice(),
				bytes,
				reason
			};
		},
		stopped: () => reason !== null
	};
}

/*
================
installCrowdCapture

Use after the existing benchmark instrument() and runtime boot. The movement
observer supplies the exact runtime render clock, not callback wall time.
================
*/
export function installCrowdCapture( metadata, options = {}, target = globalThis ) {
	const root = /** @type {any} */ (target);
	const probe = root.__worldProbeFrameProfiler;
	if ( !probe?.end || !probe?.begin || !probe?.movement || !root.__benchRuntime ) {
		throw Error( "Crowd capture requires a booted benchmark" );
	}
	if ( root.__crowdCapture ) throw Error( "Crowd capture already installed" );
	const recorder = createCrowdCapture( metadata, options );
	const original = { begin: probe.begin, movement: probe.movement, end: probe.end };
	let atMs, failure = null;
	/*
	================
	begin
	================
	*/
	function begin( ...args ) {
		atMs = undefined;
		return original.begin.apply( this, args );
	}
	/*
	================
	movement
	================
	*/
	function movement( sample ) {
		atMs = sample.atMs;
		return original.movement.call( this, sample );
	}
	/*
	================
	end
	================
	*/
	function end( ...args ) {
		const result = original.end.apply( this, args );
		if ( atMs === undefined || recorder.stopped() || failure ) return result;
		try {
			recorder.append( {
				atMs,
				actors: root.__benchRuntime.characterActors(),
				orbitCamera: root.__benchRuntime.camera()
			} );
		} catch ( error ) {
			failure = String( error );
		}
		return result;
	}
	probe.begin = begin;
	probe.movement = movement;
	probe.end = end;
	root.__crowdCapture = {
		/*
		================
		finish
		================
		*/
		finish() {
			if ( probe.begin === begin ) probe.begin = original.begin;
			if ( probe.movement === movement ) probe.movement = original.movement;
			if ( probe.end === end ) probe.end = original.end;
			delete root.__crowdCapture;
			return { ...recorder.finish(), failure };
		}
	};
	return root.__crowdCapture;
}
