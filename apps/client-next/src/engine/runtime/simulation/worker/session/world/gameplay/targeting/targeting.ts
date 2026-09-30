/*
===========================================================================

targeting.ts - owns selection grants and the untagged release barrier.

Closing publishes local UI intent after a successful send. The pending release
remains owned until acknowledgement, so latency cannot redirect a late reply.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

/*
================
Pending
================
*/
type Pending = { kind: "select" | "release"; gid: number; deadline: number; family?: string; fortress?: boolean; };
/*
================
SelectionOptions

The conversation owner supplies whether its pane has closed while a service
still retains the target. Reopening requires a fresh authoritative grant.
================
*/
type SelectionOptions = { fortress?: boolean; reopen?: boolean; };
const REPLY_TIMEOUT_MS = 10_000;

/*
================
createTargeting
================
*/
export function createTargeting( send: ( frame: WireFrame ) => void ) {
	let capabilities = 0, taxRate = 0;
	let target = 0, pending: Pending | null = null, error: string | null = null;
	/*
================
request
================
	*/
	function request( opcode: number, gid: number ) {
		const payload = new Uint8Array( 4 );
		new DataView( payload.buffer ).setUint32( 0, gid, true );
		const frame = { opcode, payload };
		send( frame );
		return frame;
	}
	return {
		/*
================
select
================
		*/
		select( gid: number, now = 0, kind = "npc", options: SelectionOptions = {} ) {
			if ( !Number.isInteger( gid ) || gid <= 0 || gid > 0xffffffff ) {
				throw new Error( "Invalid target identity" );
			}
			// 692BC8..692BEC suppresses the same NPC only while its talk pane
			// is visible. Pending grants still coalesce without extending timeouts.
			const reopen = options.reopen && (kind === "npc" || kind === "teleport");
			if ( pending?.kind === "select" && pending.gid === gid || !pending && target === gid && !reopen ) {
				return null;
			}
			if ( pending ) throw new Error( "Target request pending" );
			const frame = request( 0x745a, gid );
			error = null;
			// NPCs, monsters and gates have distinct native grants. Other objects
			// have a local selection intent, not an invented server acknowledgement.
			if ( kind === "npc" || kind === "monster" || kind === "teleport" ) {
				pending = {
					kind: "select",
					gid,
					deadline: now + REPLY_TIMEOUT_MS,
					family: kind,
					fortress: options.fortress
				};
			} else {
				target = gid;
				capabilities = 0;
				taxRate = 0;
			}
			return frame;
		},
		/*
================
release
================
		*/
		release( now = 0 ) {
			if ( pending ) throw new Error( "Target request pending" );
			if ( !target ) return null;
			const frame = request( 0x74b3, target );
			pending = { kind: "release", gid: target, deadline: now + REPLY_TIMEOUT_MS };
			// Display intent is local; the untagged protocol transaction remains pending.
			target = 0;
			capabilities = 0;
			taxRate = 0;
			error = null;
			return frame;
		},
		/*
================
step
================
		*/
		step( now: number ) {
			if ( !pending || now < pending.deadline ) return false;
			// B4B3 has no identity. Reusing this channel after a missing release
			// could apply its late reply to a newer target: reconnect to resync.
			if ( pending.kind === "release" ) throw new Error( "Target release timed out; reconnect to resynchronize" );
			pending = null;
			target = 0;
			capabilities = 0;
			taxRate = 0;
			error = "Target selection was not confirmed; select again";
			return true;
		},
		/*
================
receive
================
		*/
		receive( op: number, p: Uint8Array ) {
			if ( op === 0xb4b3 ) {
				if ( !((p.length === 1 && p[0] === 1) || (p.length === 2 && p[0] === 2)) ) {
					throw new Error( "Invalid target release" );
				}
				target = 0;
				capabilities = 0;
				taxRate = 0;
				pending = null;
				error = null;
				return true;
			}
			if ( op !== 0xb45a ) return false;
			// Native 7651F1 consumes a refusal without granting a new target.
			// Do not let an untagged selection reply release a B4B3 barrier.
			if ( p[0] === 2 ) {
				if ( p.length !== 2 ) throw new Error( "Invalid target rejection" );
				if ( pending?.kind === "select" ) {
					pending = null;
					error = null;
				}
				return true;
			}
			if ( p[0] !== 1 ) throw new Error( "Unsupported target result" );
			const gate = pending?.family === "teleport";
			if ( gate && p.length !== (pending!.fortress ? 11 : 9) ) throw Error( "Invalid teleport grant" );
			if (
				!((gate && p.length === (pending!.fortress ? 11 : 9)) || (p.length === 11 && p[5] === 0) ||
					(p.length === 14 && p[5] === 1))
			) throw new Error( "Unsupported target grant" );
			const gid = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true );
			if ( pending?.kind !== "select" || gid !== pending.gid ) return true;
			target = gid;
			taxRate = gate && pending!.fortress ?
				new DataView( p.buffer, p.byteOffset, p.byteLength ).getInt16( 9, true ) :
				0;
			capabilities = gate ?
				new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 5, true ) :
				p.length === 11 ?
				new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 6, true ) :
				0;
			pending = null;
			error = null;
			return true;
		},
		/*
================
remove
================
		*/
		remove( gid: number ) {
			if ( target === gid ) {
				target = 0;
				capabilities = 0;
				taxRate = 0;
			}
			// Preserve a release barrier until its untagged acknowledgement arrives.
			if ( pending?.kind === "select" && pending.gid === gid ) pending = null;
		},
		/*
================
clear
================
		*/
		clear() {
			target = 0;
			capabilities = 0;
			taxRate = 0;
			pending = null;
			error = null;
		},
		error: () => error,
		/*
================
selectionIntent

The object the player last chose: a selection still awaiting its grant,
else the granted target, and nothing while a release is in flight. A skill
request names its own target (the server does not read the selection), so
a press right after a click aims at the clicked monster instead of waiting
for the B45A grant or reusing the previous target. Inferred: retail's
target window lags the grant too, yet an immediate press casts.
================
		*/
		selectionIntent: () => pending?.kind === "select" ? pending.gid : pending ? 0 : target,
		state: () => ({
			targetTaxRate: taxRate,
			target,
			targetPending: pending?.gid ?? 0,
			...(capabilities ? { targetCapabilities: capabilities } : {})
		})
	};
}
