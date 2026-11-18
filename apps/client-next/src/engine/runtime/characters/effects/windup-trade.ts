/*
===========================================================================

windup-trade.ts - a windup object held for the mover that trades it

A skill's windup stage can hold an object on the caster (Strong Bow's
READY stage holds cha_arrow_normal.bsr in the right hand, command slot 1)
that a later SHOT mover of the same cast takes over (its trade field names
that slot; 8DC3B0 transfers the first command payload to a single
distributed mover, keeping the resource and its visual clock). The port
releases a completed windup once the server's release time (shotAtMs)
passes, an inference so a retained buff token does not keep its windup
alive. The mover only launches on its SHOT callback, after the release:
the windup arrow was gone by then, the mover found nothing to trade and
spawned a new arrow, and the shot visibly cut and reappeared. A windup a
SHOT mover trades is therefore held until the mover takes it, or until
its cast ends or is cancelled, as the native object stays until it is
traded. An internal of effects.ts.

===========================================================================
*/
import type { EffectStage } from "@/engine/contracts/effects";

// The single-target and splash movers, the distributed movers 8DC3B0 trades into.
const TRADING_ACTIONS: ReadonlySet<string> = new Set( [ "AT_MOV_1TAR", "AT_MOV_SPLASH" ] );

/*
================
windupAwaitsTrade

True when a SHOT stage of the cast's record trades the windup object held
in command slot.
================
*/
export function windupAwaitsTrade( stages: readonly EffectStage[] | undefined, slot: number ): boolean {
	if ( !stages || slot === 0 ) return false;
	return stages.some( stage =>
		(stage.phase ?? "SHOT") === "SHOT" && stage.native?.trade === slot && stage.count === 1 &&
		TRADING_ACTIONS.has( stage.action )
	);
}
