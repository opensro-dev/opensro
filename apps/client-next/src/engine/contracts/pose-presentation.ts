/*
===========================================================================

pose-presentation.ts - timed movement samples shared by presentation owners

The sample builder publishes these inputs; the pose owner consumes them.
This contract carries no interpolation state or executable behavior.

===========================================================================
*/
import type { Pose } from "./gameplay";

/*
================
SampleInput

What characters publishes each frame for a character with timed samples:
the simulation time of its latest pose, the movement revision it belongs to,
whether it is walking, and the end of the leg being walked.

The movement owner bumps the revision whenever it re-anchors a walk (a new
click, a receipt, a correction, a native move). Two samples define a
velocity only within one revision: across a re-anchor their difference is
a jump, not motion, and extrapolating it turned a 19-unit receipt
correction 8 ms after the previous sample into -2,275 units/s.
================
*/
export interface SampleInput {
	readonly atMs: number;
	readonly revision: number;
	readonly moving: boolean;
	readonly from?: Pose;
	readonly to?: Pose;
	readonly durationMs?: number;
	readonly startedAtMs?: number;
	readonly displacement?: boolean;
	readonly transition?: import("@/engine/contracts/gameplay").MovementTransition;
}
