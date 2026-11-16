/*
===========================================================================

targeting.ts - owns selection grants and the untagged release barrier.

Closing publishes local UI intent after a successful send. The pending release
remains owned until acknowledgement, so latency cannot redirect a late reply.

===========================================================================
*/
import type { GateRow } from "@/engine/contracts/gameplay";
import { TARGET_RELEASE_TIMEOUT, type WireFrame } from "@/engine/contracts/network";

/*
================
Pending
================
*/
type Pending = {
	kind: "select" | "release";
	gid: number;
	deadline: number;
	// The latest deadline an inbound hold may reach, fixed by the first hold.
	answerLimit?: number;
	family?: string;
	fortress?: boolean;
};
/*
================
SelectionOptions

The conversation owner supplies whether its pane has closed while a service
still retains the target. Reopening requires a fresh authoritative grant.
================
*/
type SelectionOptions = { fortress?: boolean; reopen?: boolean; };
const REPLY_TIMEOUT_MS = 10_000;
// While the inbound stream still delivers, an untagged answer is in
// transit behind it, not lost: holds reach at most this far past the
// first frame seen (#2148; the movement receipt rule of #1978).
const REPLY_LIMIT_MS = 90_000;
// GATE_ROWS_FLAG is the teleport-gate service (0x1C) bit whose rows follow
// the grant.
const GATE_ROWS_FLAG = 0x8000000;
// A teleport gate's grant: result byte, gid, capability word.
const TELEPORT_GRANT_BYTES = 9;
// The character arm's grants: an NPC's [u8 vitals=0][u32 word][u8 event]
// and a monster's or COS's [u8 vitals=1][u32 hp][u32 word].
const NPC_GRANT_BYTES = 11;
const VITALS_GRANT_BYTES = 14;
// CPSMission_OnNpcSelectResponse0xB45A gates a CICMonster or CICCos
// target's talk pane on its grant word: talk 0x2 set and 0x10 clear.
const MONSTER_TALK_BIT = 0x2;
const MONSTER_TALK_BLOCK_BIT = 0x10;

/*
================
talkPaneOpens

Whether a granted selection opens the talk pane, by the target's kind as
CPSMission_OnNpcSelectResponse0xB45A branches (#3280): a CICNPC always
shows it and builds its menu from the grant (5D9100), even an empty one;
a CITeleportGate always shows it with the gate caption (0x7650b8); a
CICMonster or CICCos only when its word has the talk bit and not 0x10.
A target of another kind, or one this client has not spawned, is judged
by the grant's shape, which the server writes by the same class: the
NPC arm (11 bytes, no vitals) opens, the vitals arm takes the monster
rule. Inferred, as no other kind has a recorded grant.
================
*/
export function talkPaneOpens( kind: string | undefined, gate: boolean, p: Uint8Array ): boolean {
	if ( gate || kind === "npc" ) return true;
	const view = new DataView( p.buffer, p.byteOffset, p.byteLength );
	const monsterRule = ( word: number ) => (word & MONSTER_TALK_BLOCK_BIT) === 0 && (word & MONSTER_TALK_BIT) !== 0;
	const vitalsArm = p.length === VITALS_GRANT_BYTES && p[5] === 1;
	if ( kind === "monster" || kind === "cos" ) return vitalsArm && monsterRule( view.getUint32( 10, true ) );
	if ( kind !== undefined && kind !== "structure" ) return false;
	if ( p.length === NPC_GRANT_BYTES && p[5] === 0 ) return true;
	return vitalsArm && monsterRule( view.getUint32( 10, true ) );
}

/*
================
decodeGateRows

A quest exit gate's rows: [u8 count], then per row [u8 kind] - 0 the
return (no payload), 1 an ascii text symbol ([u16 length][bytes]), 2 a
u16 point id. Null when the bytes after the base are not exactly that.
================
*/
function decodeGateRows( p: Uint8Array, at: number ): GateRow[] | null {
	const view = new DataView( p.buffer, p.byteOffset, p.byteLength );
	if ( at >= p.length ) return null;
	const count = p[at++]!;
	const rows: GateRow[] = [];
	for ( let i = 0; i < count; i++ ) {
		if ( at >= p.length ) return null;
		const kind = p[at++]!;
		if ( kind === 0 ) {
			rows.push( { kind } );
		} else if ( kind === 1 ) {
			if ( at + 2 > p.length ) return null;
			const length = view.getUint16( at, true );
			at += 2;
			if ( at + length > p.length ) return null;
			rows.push( { kind, symbol: String.fromCharCode( ...p.subarray( at, at + length ) ) } );
			at += length;
		} else if ( kind === 2 ) {
			if ( at + 2 > p.length ) return null;
			rows.push( { kind, point: view.getUint16( at, true ) } );
			at += 2;
		} else {
			return null;
		}
	}
	return at === p.length && rows.length > 0 ? rows : null;
}

/*
================
createTargeting
================
*/
export function createTargeting(
	send: ( frame: WireFrame ) => void,
	readEntity: ( gid: number ) => import("@/engine/contracts/world").EntityState | undefined = () => undefined
) {
	let capabilities = 0, taxRate = 0, flagWarMode = 0;
	// talkPane is the granted target's talk-pane decision (talkPaneOpens).
	let talkPane = false;
	let gateRows: GateRow[] = [];
	let localSelection = false;
	let target = 0, pending: Pending | null = null, error: string | null = null;
	/*
================
forgetGrant

Drops everything the last grant carried; the target and the pending
transaction are each caller's own to settle.
================
	*/
	function forgetGrant() {
		capabilities = 0;
		flagWarMode = 0;
		taxRate = 0;
		gateRows = [];
		talkPane = false;
	}
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
			// 6B3E90 calls 6813E0 directly for the portrait. A pending wire
			// transaction retains its reply ownership while display intent changes.
			if ( kind === "local-player" ) {
				target = gid;
				forgetGrant();
				error = null;
				localSelection = true;
				return null;
			}
			// 692BC8..692BEC suppresses the same NPC only while its talk pane
			// is visible. Pending grants still coalesce without extending timeouts.
			const reopen = options.reopen && (kind === "npc" || kind === "teleport");
			if ( pending?.kind === "select" && pending.gid === gid || !pending && target === gid && !reopen ) {
				return null;
			}
			if ( pending ) throw new Error( "Target request pending" );
			const frame = request( 0x745a, gid );
			localSelection = false;
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
				forgetGrant();
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
			localSelection = false;
			forgetGrant();
			error = null;
			return frame;
		},
		/*
================
step
================
		*/
		/*
================
waitingSince

When the pending select or release left (its deadline less the reply
window), or undefined: the world session's stall detector reads it with
the other lanes' (#1472).
================
		*/
		waitingSince(): number | undefined {
			return pending ? pending.deadline - REPLY_TIMEOUT_MS : undefined;
		},
		/*
================
noteInboundProgress

Every frame (gameplay's receive wrapper) proves the inbound stream is
delivering, so the pending answer waits a full reply window past it
instead of expiring behind a burst it is ordered behind. The first
frame fixes the hard limit the holds cannot pass.
================
		*/
		noteInboundProgress( now: number ) {
			if ( !pending ) return;
			const answerLimit = pending.answerLimit ?? now + REPLY_LIMIT_MS;
			const deadline = Math.max( pending.deadline, Math.min( now + REPLY_TIMEOUT_MS, answerLimit ) );
			pending = { ...pending, deadline, answerLimit };
		},
		step( now: number ) {
			if ( !pending || now < pending.deadline ) return false;
			// B4B3 has no identity. Reusing this channel after a missing release
			// could apply its late reply to a newer target: reconnect to resync.
			if ( pending.kind === "release" ) {
				throw new Error( "Target release timed out; reconnect to resynchronize", {
					cause: TARGET_RELEASE_TIMEOUT
				} );
			}
			pending = null;
			target = 0;
			localSelection = false;
			forgetGrant();
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
				localSelection = false;
				forgetGrant();
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
			// Native 764C60 picks the shape from the entity's class when the
			// reply arrives, never from the click. The port's click-time family
			// can disagree during the entry burst, and 9 bytes is only ever a
			// teleport gate, so the length settles that case (#2149); an
			// 11-byte answer is a fortress gate's tax tail or an NPC grant,
			// which only the replied entity's class settles (#2870).
			const gid = p.length >= 5 ?
				new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true ) :
				0;
			const replied = gid ? readEntity( gid ) : undefined;
			const gate = replied?.kind === "teleport" || pending?.family === "teleport" ||
				p.length === TELEPORT_GRANT_BYTES;
			const fortress = replied ? !!replied.teleport?.fortressId : pending?.fortress;
			// A teleport gate's word may carry 0x8000000, a quest exit gate's
			// rows after the base (CIFNPCTalk_AppendTeleportRows 5D32C0).
			const base = gate ? (fortress ? 11 : TELEPORT_GRANT_BYTES) : 0;
			const rows = gate && p.length > base && p.length >= 9 &&
					(new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 5, true ) & GATE_ROWS_FLAG) !== 0 ?
				decodeGateRows( p, base ) :
				null;
			if ( gate && p.length !== base && !rows ) throw Error( "Invalid teleport grant" );
			if (
				!((gate && (p.length === base || rows)) || (p.length === 11 && p[5] === 0) ||
					(p.length === 14 && p[5] === 1))
			) throw new Error( "Unsupported target grant" );
			if ( pending?.kind !== "select" || gid !== pending.gid ) return true;
			target = gid;
			localSelection = false;
			taxRate = gate && fortress ?
				new DataView( p.buffer, p.byteOffset, p.byteLength ).getInt16( 9, true ) :
				0;
			capabilities = gate ?
				new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 5, true ) :
				p.length === 11 ?
				new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 6, true ) :
				0;
			// The NPC grant's trailing byte is the flag-war row mode
			// (the reference server's event byte: 1 offers sign-up, 2 offers cancel, else no row).
			flagWarMode = !gate && p.length === 11 && p[5] === 0 ? p[10]! : 0;
			gateRows = rows ?? [];
			// The click-time family can be stale (above); an unspawned
			// target is judged by the grant's shape instead.
			talkPane = talkPaneOpens( replied?.kind, gate, p );
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
				localSelection = false;
				forgetGrant();
			}
			// Preserve a release barrier until its untagged acknowledgement arrives.
			if ( pending?.kind === "select" && pending.gid === gid ) pending = null;
		},
		/*
================
dismiss

The target window's close (CIFTargetWindow_OnClose 582020): the window
forgets its target and sends nothing, so the server's selection and any
NPC function opened through it (an open warehouse) stay as they are.
================
		*/
		dismiss() {
			if ( pending ) return;
			target = 0;
			localSelection = false;
			forgetGrant();
			error = null;
		},
		/*
================
clear
================
		*/
		clear() {
			target = 0;
			localSelection = false;
			forgetGrant();
			pending = null;
			error = null;
		},
		error: () => error,
		/*
================
opensTalkPane

Whether the current target's grant opens the talk pane (talkPaneOpens).
================
		*/
		opensTalkPane: () => !!target && !localSelection && talkPane,
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
		selectionIntent: () =>
			localSelection ? target : pending?.kind === "select" ? pending.gid : pending ? 0 : target,
		state: () => ({
			targetTaxRate: taxRate,
			target,
			targetPending: pending?.gid ?? 0,
			...(capabilities ? { targetCapabilities: capabilities } : {}),
			...(flagWarMode ? { targetFlagWarMode: flagWarMode } : {}),
			...(gateRows.length ? { targetGateRows: gateRows } : {})
		})
	};
}
