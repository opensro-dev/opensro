/*
===========================================================================

device-recovery.ts - when the renderer may try for a new GPU device

A device the browser takes away is replaced, not the session ended. Right
after a GPU-process or driver reset, requestDevice itself can reject for a
while (#2337: "ID3D12Device::Create..."), so one immediate attempt is not
enough. This module owns the pacing and the judgement of which refusals a
new device can cure; renderer.ts owns the device. Frame
time drives it, with no timer of its own.

===========================================================================
*/

// The wait before each attempt, counted from the failure that led to it. The
// whole budget is under two minutes: a reset outlives the first attempts, and
// a machine that never recovers still reaches the visible failure.
const DEVICE_RECOVERY_DELAYS_MS = [ 0, 1000, 2000, 5000, 10000, 30000, 30000, 30000 ] as const;
// A device that stays up this long has recovered: a loss hours later in the
// same session starts a fresh budget instead of spending the old one.
const DEVICE_STABLE_MS = 60000;

/*
================
isOutOfMemoryRefusal

Whether an error is the browser refusing a GPU allocation. Dawn reports
that refusal in more than one shape: an uncaptured out-of-memory error,
or a rejected promise carrying a DOMException ("OperationError: Not
enough memory left.", #2866) whose class says nothing about memory. The
stringified value is therefore what is matched (the class name survives
stringification), the same rule the worker's heap refusal follows
(network.ts isOutOfMemoryFailure); the one instanceof check stays with
the device owner, which may name GPU types. A new device starts empty,
so every shape takes the recovery path; a validation or compile error
would only repeat on the new device and stays permanent.
================
*/
export function isOutOfMemoryRefusal( error: unknown ): boolean {
	return /not enough memory|out ?of ?memory/i.test( String( error ) );
}

/*
================
createDeviceRecovery
================
*/
export function createDeviceRecovery() {
	let attempts = 0, failedAt: number | null = null, runningSince: number | null = null;
	return {
		/*
		================
		pending

		Another attempt remains, so a failed device isn't the end yet.
		================
		*/
		pending: () => attempts < DEVICE_RECOVERY_DELAYS_MS.length,
		/*
		================
		due

		Called on frames where the device has failed recoverably.
		================
		*/
		due( nowMs: number ) {
			runningSince = null;
			if ( attempts >= DEVICE_RECOVERY_DELAYS_MS.length ) return false;
			failedAt ??= nowMs;
			return nowMs - failedAt >= DEVICE_RECOVERY_DELAYS_MS[attempts]!;
		},
		/*
		================
		began
		================
		*/
		began() {
			attempts++;
			failedAt = null;
		},
		/*
		================
		running

		Called on frames where the device is running.
		================
		*/
		running( nowMs: number ) {
			runningSince ??= nowMs;
			if ( attempts > 0 && nowMs - runningSince >= DEVICE_STABLE_MS ) attempts = 0;
		}
	};
}
