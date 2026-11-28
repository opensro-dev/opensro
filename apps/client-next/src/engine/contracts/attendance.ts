/*
===========================================================================

attendance.ts - the attendance calendar feed's shapes (port-only, not native)

#3255: with SRO_ATTENDANCE on, the server keeps every entered session
told of its character's attendance month (wire 0x3FD2). The month is a
bit set of the days checked, the claim masks of the two reward columns
(Continuity and Accumulation) and each column's tiers with the day count
that earns them and the item codename they pay. The client's check-in
and claims ride 0x7FD2; the server decides every one, so the window
never counts a reward the server did not say.

===========================================================================
*/

export interface AttendanceTier {
	// The streak or checked-day count that earns the tier.
	readonly days: number;
	// The item codename the tier pays.
	readonly reward: string;
}

export interface AttendanceFeed {
	// YYYYMM of the attendance month.
	readonly monthKey: number;
	// Day of the month (1..31); the day turns at 04:00 UTC.
	readonly today: number;
	readonly daysInMonth: number;
	// Bit d-1 is set for each checked day d.
	readonly checked: number;
	// Bit i is set for each received tier i of the column.
	readonly continuityClaimed: number;
	readonly accumulationClaimed: number;
	readonly continuityTiers: readonly AttendanceTier[];
	readonly accumulationTiers: readonly AttendanceTier[];
	readonly nextResetMs: number;
	// The server clock at the build instant: the countdown's anchor.
	readonly serverNowMs: number;
	// Rises with every snapshot; never resets (see the worker lane).
	readonly revision: number;
}
