/*
===========================================================================

arrow-keys.ts - walking and turning with the arrow keys

The v1.150 client walks with Up and turns with Left and Right (#1574):

  - Up press (Game_OnKeyDown 0x680A30 case 1, MovementController_
    SetDirectionVector 0x877FB0 bit 1): a direction walk along the facing
    (CNavigationDeadreckon_SendAngleMoveCommand 0x877F30 with bFlag 0, so
    no resend while a direction walk is already under way).
  - Left / Right held: steering flags 0x10 / 0x20 turn the facing by pi
    radians a second (CNavigationController_UpdateSteeringTurn 0x86D480:
    Left adds, Right subtracts; Left wins when both are held). The heading
    reaches the server as 0x72CF [u16 heading] whenever it is a quarter
    of pi or more from the heading last sent (SendSteeringUpdate 0x877540,
    flag 0), and once more on release if it is a degree or more off
    (flag 1).
  - Up release (Game_OnKeyUp 0x688290, DispatchMovementKeyRelease
    0x877C60 bit 1): 0x72F5 [u16 heading] stops the walk
    (SendAngleUpdatePacket 0x8777B0).
  - Down is not a movement key (its press is the default case, its
    release a no-op mask); auto-repeat never reaches here.
  - A press during a cast (#1589): CGInterface_CancelSkillForKeyboardMove
    (0x693190) sends 0x72CD [u8 2], at most once per 2000 ms
    (g_dwLastKeyboardMoveCancelMs) and only with fewer than two commands
    queued (0x67D140); locomotion stays refused, so the key's latch stays
    0 and CGInterface_PollArrowKeyMovement (0x67C960) retries it every
    frame while it is held: the walk or turn starts the moment the cast
    lets go.

A rider sends the vehicle's twins, 0x769E [u32 gid] tag 4 (steer) and
tag 3 (stop). The server already answers all four (world/movement
steer.go, action COS commands).

Inference, stated because the port has no +0x40: SendSteeringUpdate
compares with the last heading it sent. The port's server learns the
heading with every move command, so a turn starts from the heading the
walk already carries.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
import { HEADING_SCALE } from "@/engine/foundation/gameplay/native-movement";
import { ARROW_TURN_WORDS_PER_MS } from "@/engine/foundation/gameplay/direction-movement";

// 0x72CF: SendSteeringUpdate's player form.
export const OP_STEER = 0x72cf;
// 0x72F5: SendAngleUpdatePacket's player form.
export const OP_DIRECTION_STOP = 0x72f5;
// 0x769E: the vehicle's movement family, with its steer and stop tags.
export const OP_COS_COMMAND = 0x769e;
export const COS_STOP_TAG = 3;
export const COS_STEER_TAG = 4;
// pi radians a second (0x86D480), shared with the main-thread mover.
const TURN_WORDS_PER_MS = ARROW_TURN_WORDS_PER_MS;
// SendSteeringUpdate's pi/4 (0x877540, flag 0): a turn under way reaches the
// server every eighth of a turn.
const STEER_SEND_WORDS = HEADING_SCALE / 8;
// SendSteeringUpdate's one degree (flag 1): the release sends only a heading a
// degree or more off.
// 0x6931D9: one keyboard cancel per 2000 ms (cmp 0x7D0 / jb).
const KEYBOARD_CANCEL_THROTTLE_MS = 2000;
const RELEASE_SEND_WORDS = HEADING_SCALE / 360;

export type ArrowKey = "up" | "left" | "right";

/*
================
ArrowGate

Whether a press may act now: open; held by a cast (CanPerformLocomotion
refuses, the press cancels and the key waits latched open); or closed (the
press guards of 0x680A30 return before anything happens).
================
*/
export type ArrowGate = "open" | "cast" | "closed";

/*
================
ArrowKeyMover

What the arrow keys move: the movement owner's pose and direction walk,
and the ridden vehicle that receives the commands instead, if any.
================
*/
export interface ArrowKeyMover {
	readonly pose: () => Pose | null;
	readonly walkingDirection: () => boolean;
	readonly direct: ( heading: number, now: number ) => WireFrame;
	readonly steer: ( heading: number, now: number ) => void;
	readonly stop: ( now: number ) => void;
	readonly vehicle: () => number | undefined;
	// 0x693190's request, when fewer than two commands are queued; returns
	// whether it went out.
	readonly cancelCast: () => boolean;
}

/*
================
headingFrame

A steer or stop of the player (0x72CF / 0x72F5 [u16 heading]) or of the
ridden vehicle (0x769E [u32 gid][u8 tag][u16 heading]).
================
*/
export function headingFrame( kind: "steer" | "stop", heading: number, vehicle?: number ): WireFrame {
	if ( vehicle === undefined ) {
		const payload = new Uint8Array( 2 );
		new DataView( payload.buffer ).setUint16( 0, heading, true );
		return { opcode: kind === "steer" ? OP_STEER : OP_DIRECTION_STOP, payload };
	}
	const payload = new Uint8Array( 7 ), view = new DataView( payload.buffer );
	view.setUint32( 0, vehicle, true );
	payload[4] = kind === "steer" ? COS_STEER_TAG : COS_STOP_TAG;
	view.setUint16( 5, heading, true );
	return { opcode: OP_COS_COMMAND, payload };
}

/*
================
headingGap

The unsigned distance between two headings, in words, the short way round.
================
*/
function headingGap( a: number, b: number ): number {
	const d = Math.abs( a - b ) % HEADING_SCALE;
	return Math.min( d, HEADING_SCALE - d );
}

/*
================
createArrowKeys
================
*/
export function createArrowKeys( send: ( frame: WireFrame ) => void ) {
	// The keys down, and those whose press has acted (0x7C8..0x7D0): a held
	// key that is not latched is retried every step (0x67C960).
	const held = { up: false, left: false, right: false };
	const latched = { up: false, left: false, right: false };
	// When the last keyboard cancel went out (g_dwLastKeyboardMoveCancelMs).
	let lastCancelMs = -Infinity;
	// The turn in progress: its unrounded heading, when it was last stepped,
	// and the heading last sent to the server.
	let turn: { heading: number; atMs: number; sent: number; } | null = null;
	/*
	================
	turnRate

	Words a millisecond: Left adds and wins over Right (0x86D480 tests 0x10
	first).
	================
	*/
	function turnRate(): number {
		if ( latched.left ) return TURN_WORDS_PER_MS;
		if ( latched.right ) return -TURN_WORDS_PER_MS;
		return 0;
	}
	/*
	================
	act

	A held key's press takes effect: Up walks the facing unless a direction
	walk is under way (0x877F30, bFlag 0); Left and Right start the turn.
	================
	*/
	function act( key: ArrowKey, now: number, mover: ArrowKeyMover ): WireFrame | null {
		const pose = mover.pose();
		if ( !pose ) return null;
		latched[key] = true;
		if ( key === "up" ) return mover.walkingDirection() ? null : mover.direct( pose.angle, now );
		turn ??= { heading: pose.angle, atMs: now, sent: pose.angle };
		return null;
	}
	/*
	================
	cancelCast

	0x693190: the cast in the way is asked to end, once per 2000 ms.
	================
	*/
	function cancelCast( now: number, mover: ArrowKeyMover ) {
		if ( now - lastCancelMs < KEYBOARD_CANCEL_THROTTLE_MS ) return;
		if ( mover.cancelCast() ) lastCancelMs = now;
	}
	/*
	================
	advance

	Turns the facing to now and sends a steer every eighth of a turn.
	================
	*/
	function advance( now: number, mover: ArrowKeyMover ) {
		if ( !turn ) return;
		const rate = turnRate();
		const elapsed = Math.max( 0, now - turn.atMs );
		turn.atMs = now;
		if ( !rate || !elapsed ) return;
		turn.heading = ((turn.heading + rate * elapsed) % HEADING_SCALE + HEADING_SCALE) % HEADING_SCALE;
		const heading = Math.round( turn.heading ) % HEADING_SCALE;
		mover.steer( heading, now );
		if ( headingGap( heading, turn.sent ) >= STEER_SEND_WORDS ) {
			send( headingFrame( "steer", heading, mover.vehicle() ) );
			turn.sent = heading;
		}
	}
	return {
		/*
		================
		key

		A press or release. gate is the walk gate: a closed press changes
		nothing (0x680A30's guards return before latching); a press held by a
		cast cancels it and waits; a release always runs.
		================
		*/
		key( key: ArrowKey, down: boolean, now: number, mover: ArrowKeyMover, gate: ArrowGate ): WireFrame | null {
			advance( now, mover );
			const pose = mover.pose();
			if ( down ) {
				if ( gate === "closed" || !pose || held[key] ) return null;
				held[key] = true;
				if ( gate === "cast" ) {
					cancelCast( now, mover );
					return null;
				}
				return act( key, now, mover );
			}
			if ( !held[key] ) return null;
			held[key] = false;
			if ( !latched[key] ) return null;
			latched[key] = false;
			if ( key === "up" ) {
				if ( !pose ) return null;
				const frame = headingFrame( "stop", pose.angle, mover.vehicle() );
				send( frame );
				mover.stop( now );
				return frame;
			}
			if ( turn && !latched.left && !latched.right ) {
				const heading = Math.round( turn.heading ) % HEADING_SCALE;
				if ( headingGap( heading, turn.sent ) >= RELEASE_SEND_WORDS ) {
					send( headingFrame( "steer", heading, mover.vehicle() ) );
				}
				turn = null;
			}
			return null;
		},
		/*
		================
		step

		Turns the facing, and retries every held key the gate held back
		(0x67C960): one the gate now admits acts at once.
		================
		*/
		step( now: number, mover: ArrowKeyMover, gateOpen: boolean ): WireFrame | null {
			advance( now, mover );
			if ( !gateOpen ) return null;
			let frame: WireFrame | null = null;
			for ( const key of [ "up", "left", "right" ] as const ) {
				if ( held[key] && !latched[key] ) frame = act( key, now, mover ) ?? frame;
			}
			return frame;
		},
		/*
		================
		rate

		The turn under way in words a millisecond (Left positive), published
		so the main-thread mover keeps turning past a publication (#1574).
		================
		*/
		rate(): number {
			return turn ? turnRate() : 0;
		},
		/*
		================
		clear

		Focus lost or the world left: nothing stays held.
		================
		*/
		clear() {
			held.up = held.left = held.right = false;
			latched.up = latched.left = latched.right = false;
			turn = null;
		}
	};
}
