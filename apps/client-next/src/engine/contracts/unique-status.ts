/*
===========================================================================

unique-status.ts - the Unique Status feed a player receives

Port-only, not native, off by default (server SRO_UNIQUE_STATUS, client
Experimental row). The worker decodes the port-only 0x3FC4/0x3FC5 frames
into this shape (runtime/.../unique-status/unique-status.ts); the
gameplay snapshot carries the latest feed and the HUD window draws it.
The rows are the same states the public API's uniques page publishes:
alive or dead, and the last recorded kill - never a respawn time, which
stays secret.

===========================================================================
*/

/*
================
UniqueStatusRow

One unique's state (port-only 0x3FC4/0x3FC5; unique-status.ts). A
diedAtMs of 0 (and an empty killer) means no kill is recorded yet.
================
*/
export interface UniqueStatusRow {
	readonly refObjId: number;
	readonly name: string;
	readonly level: number;
	readonly regionId: number;
	readonly alive: boolean;
	readonly diedAtMs: number;
	readonly killer: string;
}

/*
================
UniqueStatusFeed

The whole list as last composed by the server, with the server's clock
at composition (the window's times are server time) and a revision that
rises with every frame, so the HUD can tell a fresh feed from the one
it already drew - and notice which rows changed for its alerts.
================
*/
export interface UniqueStatusFeed {
	readonly serverNowMs: number;
	readonly rows: readonly UniqueStatusRow[];
	readonly revision: number;
}
