/*
===========================================================================

crowd-activity.mjs - a held crowd that walks and chats, scripted from a seed

A standing crowd is the best case for any "write only what changed" work
(#3563): nothing changes. This owner gives a share of the peers human
input instead. Walkers click ground points near their grid spot at human
intervals, chatters send local (All) chat lines, the rest stand. Each peer
draws its whole script from its own generator seeded by (seed, index), so
the same seed gives every peer the same sequence of clicks and lines no
matter how the timers interleave.

Frames are what the replacement client sends: chat goes through the
client's own chat owner (chat.ts createChat, with its acknowledgement and
deadline), a ground click through the movement owner's predicted envelope
(movement.ts request: 0x0009 [u8 1][u32 id] then the native 0x7738 body).

Fighting is not here: it needs monsters beside the crowd and each peer
decoding their spawns for a target gid (see the lane report).

===========================================================================
*/
import "../../../tests/helpers/native-source-loader.mjs";

const { createChat } = await import(
	"../../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts"
);
const { REGION_SIZE } = await import( "../../../src/engine/foundation/world/world-space.ts" );

// The replacement client's predicted-movement envelope (movement.ts
// OP_PREDICTED_MOVE, transport.OpPredictedMove) and its player kind byte;
// the server answers each id with one 0x000A JSON receipt.
const OP_PREDICTED_MOVE = 0x0009, OP_PREDICTED_MOVE_RESULT = 0x000a, ENVELOPE_PLAYER = 1;
// The native 0x7738 ground click (moverequest.go DecodeClientMovementRequest):
// mode 1, region, then the int16 region-local x, height and z.
const MOVE_DESTINATION_MODE = 1, GROUND_CLICK_BYTES = 9;
// Chat: the All channel (nearby players) and its acknowledgement (chat.ts).
const CHAT_ALL_CHANNEL = 1, OP_CHAT_ACK = 0xb367, CHAT_ACK_OK = 1;
// A walker clicks a point within WALK_RADIUS of its grid spot (15 units
// apart, crowd.mjs crowdLayout, which keeps this reach inside the
// observer's interest area) every 1.5 to 6 s: a player clicking around
// while waiting, not a bot pinned to one rate.
export const WALK_RADIUS = 25;
const WALK_MIN_MS = 1500, WALK_MAX_MS = 6000;
// A chatter speaks every 8 to 30 s.
const CHAT_MIN_MS = 8000, CHAT_MAX_MS = 30000;
const CHAT_LINES = Object.freeze( [
	"anyone for a party?",
	"selling blades, pm me",
	"lfg tiger girl",
	"where is the stable?",
	"brb",
	"thanks!",
	"hello all",
	"how much for elixirs?"
] );
export const ACTIVITY_ROLES = Object.freeze( [ "walk", "chat" ] );
const STILL = "still";

/*
================
parseActivity

"walk=0.5,chat=0.2" (or "" / "still") into shares of the crowd. The shares
are disjoint roles; whatever they leave stands still. label is canonical,
so two spellings of the same activity key the same held crowd.
================
*/
export function parseActivity( text ) {
	const shares = { walk: 0, chat: 0 };
	const spec = String( text ?? "" ).trim();
	if ( spec && spec !== STILL ) {
		for ( const part of spec.split( "," ) ) {
			const [name, value, extra] = part.split( "=" ).map( item => item.trim() );
			const share = Number( value );
			if ( !ACTIVITY_ROLES.includes( name ) || extra !== undefined || !(share >= 0 && share <= 1) ) {
				throw Error( `bad --act part "${part}": expected walk=SHARE or chat=SHARE, SHARE in [0, 1]` );
			}
			shares[name] = share;
		}
	}
	if ( shares.walk + shares.chat > 1 + 1e-9 ) throw Error( `--act shares sum past 1: ${spec}` );
	const label =
		ACTIVITY_ROLES.filter( role => shares[role] > 0 ).map( role => `${role}=${shares[role]}` ).join( "," ) ||
		STILL;
	return { ...shares, label };
}

/*
================
apportionRoles

How many of count peers take each role: largest remainder over walk, chat
and still, so the counts always sum to count (rounding each share alone
can give 2 + 2 of 3). Ties go to the earlier role.
================
*/
export function apportionRoles( count, shares ) {
	const roles = [ ...ACTIVITY_ROLES, STILL ];
	const wanted = [ shares.walk * count, shares.chat * count, Math.max( 0, 1 - shares.walk - shares.chat ) * count ];
	const counts = wanted.map( Math.floor );
	let left = count - counts.reduce( ( a, b ) => a + b, 0 );
	const order = roles.map( ( _, i ) => i ).sort( ( a, b ) =>
		(wanted[b] - counts[b]) - (wanted[a] - counts[a]) || a - b
	);
	for ( let i = 0; left > 0; i = (i + 1) % order.length, left-- ) counts[order[i]]++;
	return Object.fromEntries( roles.map( ( role, i ) => [ role, counts[i] ] ) );
}

/*
================
createRandom

mulberry32: a small fixed generator, so a script never depends on
Math.random or on the order other peers drew in.
================
*/
function createRandom( seed ) {
	let state = seed >>> 0;
	return () => {
		state = state + 0x6d2b79f5 >>> 0;
		let t = Math.imul( state ^ state >>> 15, 1 | state );
		t = t + Math.imul( t ^ t >>> 7, 61 | t ) ^ t;
		return ((t ^ t >>> 14) >>> 0) / 0x100000000;
	};
}

/*
================
streamSeed

One generator per (seed, stream): stream 0 deals the roles, stream
index + 1 is peer index's script.
================
*/
function streamSeed( seed, stream ) {
	return Math.imul( (seed >>> 0) ^ 0x9e3779b9, 0x85ebca6b ) + Math.imul( stream, 0xc2b2ae35 ) >>> 0;
}

/*
================
assignRoles

Each peer's role: a seeded shuffle of the indices, cut by apportionRoles.
================
*/
export function assignRoles( count, shares, seed ) {
	const random = createRandom( streamSeed( seed, 0 ) );
	const order = Array.from( { length: count }, ( _, i ) => i );
	for ( let i = count - 1; i > 0; i-- ) {
		const j = Math.floor( random() * (i + 1) );
		[order[i], order[j]] = [ order[j], order[i] ];
	}
	const counts = apportionRoles( count, shares ), roles = new Array( count ).fill( STILL );
	let at = 0;
	for ( const role of ACTIVITY_ROLES ) {
		for ( let n = 0; n < counts[role]; n++ ) roles[order[at++]] = role;
	}
	return roles;
}

/*
================
peerScript

Peer index's endless script as a next() function: each call gives the
wait before the next action and the action. The first wait is drawn
within one interval, so the crowd does not act in step.
================
*/
export function peerScript( { role, index, seed, spot } ) {
	const random = createRandom( streamSeed( seed, index + 1 ) );
	const [min, max] = role === "walk" ? [ WALK_MIN_MS, WALK_MAX_MS ] : [ CHAT_MIN_MS, CHAT_MAX_MS ];
	let first = true;
	return () => {
		const delayMs = Math.round( first ? random() * max : min + random() * (max - min) );
		first = false;
		if ( role === "chat" ) {
			return { delayMs, kind: "chat", text: CHAT_LINES[Math.floor( random() * CHAT_LINES.length )] };
		}
		const angle = random() * Math.PI * 2, distance = Math.sqrt( random() ) * WALK_RADIUS;
		return {
			delayMs,
			kind: "walk",
			to: {
				...spot,
				x: regionLocal( spot.x + Math.cos( angle ) * distance ),
				z: regionLocal( spot.z + Math.sin( angle ) * distance )
			}
		};
	};
}

/*
================
regionLocal

A click keeps its spot's region: a point drawn past the region's edge
stays on it. The server refuses a local coordinate outside
[0, REGION_SIZE), which would count a script's own draw as a refusal.
================
*/
function regionLocal( value ) {
	return Math.min( REGION_SIZE - 1, Math.max( 0, value ) );
}

/*
================
groundClickBody

The native 0x7738 mode-1 body for spot, in the client's integer
region-local form.
================
*/
export function groundClickBody( spot ) {
	const payload = new Uint8Array( GROUND_CLICK_BYTES ), view = new DataView( payload.buffer );
	payload[0] = MOVE_DESTINATION_MODE;
	view.setUint16( 1, spot.regionId, true );
	view.setInt16( 3, Math.trunc( spot.x ), true );
	view.setInt16( 5, Math.trunc( spot.y ), true );
	view.setInt16( 7, Math.trunc( spot.z ), true );
	return payload;
}

/*
================
predictedMoveBody

The player envelope the movement owner sends for a ground click: kind,
the session's next command id (strictly increasing from 1, or the server
drops it as stale), then the native body.
================
*/
function predictedMoveBody( id, spot ) {
	const payload = new Uint8Array( 5 + GROUND_CLICK_BYTES );
	payload[0] = ENVELOPE_PLAYER;
	new DataView( payload.buffer ).setUint32( 1, id, true );
	payload.set( groundClickBody( spot ), 5 );
	return payload;
}

/*
================
startActivity

Runs the scripts on an admitted crowd ({ send, listen }, crowd.mjs).
spotFor( index ) is the peer's grid spot. Returns { evidence, stop }:
evidence is plain JSON (what ran, with its seed, and what the server
answered), updated in place while the activity runs.
================
*/
export function startActivity( { crowd, count, activity, seed, spotFor } ) {
	const roles = activity.label === STILL ? new Array( count ).fill( STILL ) : assignRoles( count, activity, seed );
	const evidence = {
		act: activity.label,
		seed: activity.label === STILL ? null : seed,
		peers: apportionRoles( count, activity.label === STILL ? { walk: 0, chat: 0 } : activity ),
		clicks: 0,
		moveAccepted: 0,
		moveRefused: 0,
		chats: 0,
		chatAcked: 0,
		chatRefused: 0,
		// A line due while the previous one still waits for its receipt.
		chatBusy: 0
	};
	const timers = new Set(), started = performance.now();
	let running = true;
	/*
	================
	schedule
	================
	*/
	const schedule = ( next, act ) => {
		const step = next();
		const timer = setTimeout( () => {
			timers.delete( timer );
			if ( !running ) return;
			act( step );
			schedule( next, act );
		}, step.delayMs );
		timer.unref?.();
		timers.add( timer );
	};
	for ( let index = 0; index < count; index++ ) {
		const role = roles[index];
		if ( role === STILL ) continue;
		const next = peerScript( { role, index, seed, spot: spotFor( index ) } );
		if ( role === "walk" ) {
			let id = 0;
			crowd.listen( index, frame => {
				if ( frame.opcode !== OP_PREDICTED_MOVE_RESULT ) return;
				const receipt = JSON.parse( new TextDecoder().decode( frame.payload ) );
				if ( receipt.accepted ) evidence.moveAccepted++;
				else evidence.moveRefused++;
			} );
			schedule( next, step => {
				crowd.send( index, OP_PREDICTED_MOVE, predictedMoveBody( ++id, step.to ) );
				evidence.clicks++;
			} );
			continue;
		}
		const chat = createChat( frame => crowd.send( index, frame.opcode, frame.payload ) );
		crowd.listen( index, frame => {
			if ( frame.opcode !== OP_CHAT_ACK ) return;
			if ( frame.payload[0] === CHAT_ACK_OK ) evidence.chatAcked++;
			else evidence.chatRefused++;
			chat.receive( frame, 0 );
		} );
		schedule( next, step => {
			const now = performance.now() - started;
			chat.step( now );
			if ( chat.state().pending ) {
				evidence.chatBusy++;
				return;
			}
			chat.request( CHAT_ALL_CHANNEL, step.text, "", now );
			evidence.chats++;
		} );
	}
	return {
		evidence,
		stop: () => {
			running = false;
			for ( const timer of timers ) clearTimeout( timer );
			timers.clear();
		}
	};
}
