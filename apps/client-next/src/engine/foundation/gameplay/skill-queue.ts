/*
===========================================================================

skill-queue.ts - when a skill press is sent, held or denied

A press is decided on the client against the server's clock, never sent to
be refused (a deliberate deviation from retail, which sends every press and
answers a cooling-down one with 0x3005):

	send   the skill is ready by the time the press reaches the server
	queue  it becomes ready within QUEUE_WINDOW_MS: hold the press in one
	       slot (a newer press replaces it) and send it so it arrives just
	       after the server's cooldown ends
	deny   longer to wait: send nothing, the shortcut slot gives feedback

The press reaches the server one delivery after it is sent, so the client
aims to send at (cooldown end - one delivery + ARRIVAL_MARGIN_MS). The
server's grace window (cooldownGraceMs, 150 ms) absorbs what the round-trip
estimate gets wrong.

The round trip is measured from the commands themselves: every skill press
is answered at once by B245 or B2CD, and the time to that first answer is a
sample, smoothed as TCP smooths its RTT (gain 1/8).

===========================================================================
*/

// How far ahead of readiness a press is held instead of denied (the WoW-style
// spell queue window).
export const QUEUE_WINDOW_MS = 400;
// How late after the server's cooldown end a held press aims to arrive, so the
// server's 100 ms tick rarely has to hold it in its own grace queue.
export const ARRIVAL_MARGIN_MS = 20;
// A press answer later than this is not a round-trip sample (a stall).
const MAX_RTT_SAMPLE_MS = 3000;
const RTT_GAIN = 0.125;

/*
================
PressDecision
================
*/
export type PressDecision =
	| { readonly kind: "send"; }
	| { readonly kind: "queue"; readonly fireAtMs: number; }
	| { readonly kind: "deny"; readonly remainingMs: number; };

/*
================
decidePress

remainingMs is the local wait until the server's cooldown ends (0 or
undefined when ready), oneWayMs one delivery.
================
*/
export function decidePress( remainingMs: number | undefined, oneWayMs: number, now: number ): PressDecision {
	if ( !remainingMs || remainingMs <= 0 ) return { kind: "send" };
	const wait = remainingMs - oneWayMs + ARRIVAL_MARGIN_MS;
	if ( wait <= 0 ) return { kind: "send" };
	if ( remainingMs <= QUEUE_WINDOW_MS ) return { kind: "queue", fireAtMs: now + wait };
	return { kind: "deny", remainingMs };
}

/*
================
QueuedPress

A held press: its skill, the command to replay as pressed, and when to
send it.
================
*/
export interface QueuedPress<Command> {
	readonly skill: number;
	readonly command: Command;
	readonly fireAtMs: number;
}

/*
================
DeniedPress

The newest denied press, for the shortcut slot's feedback.
================
*/
export interface DeniedPress {
	readonly skill: number;
	readonly atMs: number;
	readonly remainingMs: number;
}

// A denial sounds at most this often, however fast the key repeats.
const DENIAL_SOUND_MS = 400;

/*
================
createSkillPressQueue

The one held press, the newest denial and the round-trip estimate.
================
*/
export function createSkillPressQueue<Command>() {
	let queued: QueuedPress<Command> | null = null, denied: DeniedPress | null = null;
	let lastDenialSound = -Infinity;
	let rtt = 0, answerPendingSince: number | null = null;
	return {
		/*
		================
		queue

		Hold a press; a newer press replaces the held one.
		================
		*/
		queue( press: QueuedPress<Command> ) {
			queued = press;
		},
		/*
		================
		deny

		Record a denial; true when it should sound.
		================
		*/
		deny( press: DeniedPress ): boolean {
			denied = press;
			if ( press.atMs - lastDenialSound < DENIAL_SOUND_MS ) return false;
			lastDenialSound = press.atMs;
			return true;
		},
		/*
		================
		due

		The held press once its time has come, removed from the slot.
		================
		*/
		due( now: number ): QueuedPress<Command> | null {
			if ( !queued || now < queued.fireAtMs ) return null;
			const press = queued;
			queued = null;
			return press;
		},
		/*
		================
		queued
		================
		*/
		queued: () => queued,
		/*
		================
		cancel

		Drop the held press (movement, death, a vanished target).
		================
		*/
		cancel(): boolean {
			const had = queued !== null;
			queued = null;
			return had;
		},
		/*
		================
		sent

		A press went out; its first answer times the round trip.
		================
		*/
		sent( now: number ) {
			answerPendingSince = now;
		},
		/*
		================
		answered

		B245 or B2CD arrived: the first answer to a sent press is a sample.
		================
		*/
		answered( now: number ) {
			if ( answerPendingSince === null ) return;
			const sample = now - answerPendingSince;
			answerPendingSince = null;
			if ( sample < 0 || sample > MAX_RTT_SAMPLE_MS ) return;
			rtt = rtt ? rtt + (sample - rtt) * RTT_GAIN : sample;
		},
		/*
		================
		oneWayMs

		One delivery: half the smoothed round trip (0 until measured, which
		only makes a held press arrive later).
		================
		*/
		oneWayMs(): number {
			return rtt / 2;
		},
		/*
		================
		state
		================
		*/
		state() {
			return {
				skillQueue: queued ? { skill: queued.skill, fireAtMs: queued.fireAtMs } : undefined,
				skillDenied: denied ?? undefined
			};
		},
		/*
		================
		clear
		================
		*/
		clear() {
			queued = null;
			denied = null;
			lastDenialSound = -Infinity;
			rtt = 0;
			answerPendingSince = null;
		}
	};
}
