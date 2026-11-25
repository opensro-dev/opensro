/*
===========================================================================

styria.ts - the Styria Clash status feed's shapes (port-only)

#3648 slice 5: with SRO_STYRIA_CLASH on, the server keeps an
applicant, a registrant or an entered participant told of the arena
(wire 0x3FC8) for the registration, score and result windows. Phases
follow the arena: registration (the day's queue and schedule), match
(the entered pair of sides under the clock) and result (the clock's
verdict with the viewer's own reward figures). Sides are 0 Hunters
and 1 Thieves; ownSide -1 is a viewer with no side. The clocks are
absolute server instants; serverNowMs anchors the countdowns on the
server's clock, not the tab's.

===========================================================================
*/

export type StyriaPhase = "registration" | "match" | "result";

export interface StyriaFeed {
	readonly phase: StyriaPhase;
	// The match this snapshot belongs to (0 before any match).
	readonly matchSeq: number;
	// Registration: the next match start and when its applications
	// open. 0 in the other phases.
	readonly nextStartMs: number;
	readonly windowOpensMs: number;
	// Match: the clock's end. Result: the world's close. Else 0.
	readonly phaseEndsMs: number;
	readonly serverNowMs: number;
	// 0 Hunters, 1 Thieves, -1 none.
	readonly ownSide: number;
	// 0 watching, 1 applied, 2 entered.
	readonly ownState: number;
	// Registration: applications per side. Match/result: entered.
	readonly hunters: number;
	readonly thieves: number;
	readonly scoreHunters: number;
	readonly scoreThieves: number;
	// 0 open, 1 Hunters, 2 Thieves, 3 draw.
	readonly outcome: number;
	// Result only: the viewer's own reward figures (job EXP and
	// the level-banded skill exp the reward drain pays).
	readonly jobExp: number;
	readonly skillExp: number;
	// Rises with every snapshot; never resets (see the worker lane).
	readonly revision: number;
}
