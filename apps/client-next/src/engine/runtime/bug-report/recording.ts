/*
===========================================================================

recording.ts - the bug reporter's Record/Stop cycle

Owns when the recorder runs: from the player's Record to their Stop, the
server's cap, or a /bug typed while recording. It knows nothing of the
DOM or the network; the reporter (bug-report.ts) hands it the recorder,
the clock and what to do on each outcome, which is what lets a test drive
the cycle with fakes.

Every Record, Stop and dispose is a new attempt. A start or drain that
resolves after the player moved on belongs to an older attempt and is
ignored: Record, Stop, Record again must not let the first start's late
answer stop the second recording.

===========================================================================
*/
import type { Mp4Track } from "@/engine/foundation/media/mp4";
import type { RecorderSettings } from "./recorder";

/*
================
RecordingPhase

"finishing" drains the encoders after Stop, before the window opens.
================
*/
export type RecordingPhase = "idle" | "starting" | "recording" | "finishing";

/*
================
RecordingRecorder

The part of recorder.ts the cycle drives.
================
*/
export interface RecordingRecorder {
	start( settings: RecorderSettings ): Promise<boolean>;
	stop(): void;
	complete(): Promise<Mp4Track | null>;
	running(): boolean;
	watch(): void;
	lastError(): string | null;
}

/*
================
RecordingHost

What the reporter supplies. maxSeconds is the server's cap (replaySeconds).
show draws the control; failed reports a reason to the player; finished
opens the report window with the recording and the text typed with /bug.
================
*/
export interface RecordingHost {
	readonly recorder: RecordingRecorder;
	now(): number;
	maxSeconds(): number;
	show( phase: RecordingPhase, seconds: number, maxSeconds: number ): void;
	failed( reason: string ): void;
	finished( track: Mp4Track | null, text: string ): void;
}

/*
================
Recording
================
*/
export interface Recording {
	phase(): RecordingPhase;
	/** Record pressed: starts when idle, otherwise does nothing. */
	record(): void;
	/**
	 * Stop pressed, the cap reached, or /bug typed (`text`). Abandons a
	 * pending start; drains a running recording and then calls finished.
	 */
	stop( text: string ): void;
	/** Every frame: the timer, the cap, and a recorder that died. */
	frame(): void;
	/** The last finished recording, kept until the next Record. */
	recorded(): Mp4Track | null;
	/** A sent report used this recording: it is not offered again. */
	sent( track: Mp4Track | null ): void;
	dispose(): void;
}

/*
================
createRecording
================
*/
export function createRecording( host: RecordingHost ): Recording {
	const recorder = host.recorder;
	let phase: RecordingPhase = "idle";
	let recorded: Mp4Track | null = null;
	// When "recording" began: the timer and the cap are wall time, what the
	// player sees, not encoded video (which trails it).
	let sinceMs = 0;
	let attempt = 0;
	let finishText = "";

	/*
	================
	enter

	A phase change shows at once: the control must not read "Saving…" over
	the window that Stop just opened.
	================
	*/
	function enter( next: RecordingPhase ) {
		phase = next;
		host.show( next, 0, host.maxSeconds() );
	}

	/*
	================
	fail
	================
	*/
	function fail( reason: string ) {
		attempt++;
		enter( "idle" );
		recorder.stop();
		host.failed( reason );
	}

	/*
	================
	record

	A previous recording is dropped: a new one is what the next report
	should show.
	================
	*/
	function record() {
		const maxSeconds = host.maxSeconds();
		if ( phase !== "idle" || maxSeconds <= 0 ) return;
		const owner = ++attempt;
		recorded = null;
		enter( "starting" );
		recorder.start( { windowSeconds: maxSeconds } ).then( started => {
			if ( owner !== attempt ) return;
			if ( !started ) {
				fail( recorder.lastError() ?? "Recording did not start" );
				return;
			}
			sinceMs = host.now();
			enter( "recording" );
		}, failure => {
			if ( owner === attempt ) fail( "Recording did not start: " + String( failure ) );
		} );
	}

	/*
	================
	stop
	================
	*/
	function stop( text: string ) {
		if ( phase === "starting" ) {
			attempt++;
			enter( "idle" );
			recorder.stop();
			return;
		}
		if ( phase !== "recording" ) return;
		const owner = ++attempt;
		finishText = text;
		enter( "finishing" );
		recorder.complete().then( track => {
			if ( owner !== attempt ) return;
			recorded = track;
			enter( "idle" );
			host.finished( track, finishText );
		}, failure => {
			if ( owner === attempt ) fail( "Finishing the recording failed: " + String( failure ) );
		} );
	}

	/*
	================
	frame

	The control writes the DOM only when the whole second changes
	(dialog.ts), so showing every frame is cheap.
	================
	*/
	function frame() {
		if ( phase !== "recording" ) return;
		if ( !recorder.running() ) {
			fail( recorder.lastError() ?? "The recording stopped" );
			return;
		}
		recorder.watch();
		const seconds = (host.now() - sinceMs) / 1000, maxSeconds = host.maxSeconds();
		if ( seconds >= maxSeconds ) {
			stop( "" );
			return;
		}
		host.show( phase, seconds, maxSeconds );
	}

	return {
		phase: () => phase,
		record,
		stop,
		frame,
		recorded: () => recorded,
		/*
		================
		sent
		================
		*/
		sent( track ) {
			if ( track && track === recorded ) recorded = null;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			attempt++;
			phase = "idle";
			recorder.stop();
		}
	};
}
