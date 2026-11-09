/*
===========================================================================

combat-feedback.ts - committed cosmetic damage feedback without HP mutation

===========================================================================
*/
export interface EnvironmentalDamage {
	readonly linked?: boolean;
	readonly sequence: number;
	readonly gid: number;
	readonly damage: number;
	readonly atMs: number;
}
