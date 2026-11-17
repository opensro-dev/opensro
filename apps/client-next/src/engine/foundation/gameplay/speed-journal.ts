/*
===========================================================================

speed-journal.ts - the speed frames, effects and mount the client applied

Port-only diagnostics (#3508), not native: nothing here changes what the
client does. A speed desync (a speed scroll whose icon is gone while the
character still runs fast, or the reverse) cannot be root-caused from a
report's state.json, because it shows the speeds the mover holds but not
which 0x376F set them, where that frame was routed, or which effects the
client held. The network journal keeps only the last two minutes; a
desync that started an hour earlier leaves no trace there. These rows
are kept for the whole session, the newest of each kind:

  - speed rows, every 0x376F about the local player or its mount:
    [atMs, gid, appliedGid, moverGid, walkSpeed, runSpeed]. appliedGid is
    the entity the frame's speeds were written to (the frame's gid, or
    the mount it resolved through mountedOn, CCharactor_GetActiveMoverEntity
    0x85E000); moverGid is the entity the local mover then took its
    speeds from (0 when there was none);
  - effect rows, the local player's attached effects as they arrived and
    retired: [atMs, opcode, gid, skill, token] (0xB419 add, 0xB6A0
    teardown);
  - mount rows, each change of the local player's mountedOn:
    [atMs, mountedOn] (0 on foot);
  - buff rows, the buff board at capture:
    [gid, skill, token, active (1, 0 departing), remainingMs, receivedAtMs],
    -1 for a timer the slot does not carry.

Numbers only, so the shared report copy carries every row and no name
(a buff's subject names a player; it is never recorded).

===========================================================================
*/
import type { BuffSlot } from "./buff-slots";

// How many rows each journal keeps, and the numbers in each row.
export const SPEED_JOURNAL_ROWS = 128;
export const SPEED_JOURNAL_FIELDS = 6;
export const EFFECT_JOURNAL_ROWS = 256;
export const EFFECT_JOURNAL_FIELDS = 5;
export const MOUNT_JOURNAL_ROWS = 32;
export const MOUNT_JOURNAL_FIELDS = 2;
export const BUFF_JOURNAL_FIELDS = 6;
// A buff row's timer column when the slot carries no timer.
export const BUFF_JOURNAL_NO_TIMER = -1;
// mountedOn before the first observation, so that the first one (on foot
// included) is recorded.
const MOUNT_UNKNOWN = -1;

/*
================
SpeedFrameRow

One 0x376F as the client routed it (see the file header).
================
*/
export type SpeedFrameRow = {
	readonly atMs: number;
	readonly gid: number;
	readonly appliedGid: number;
	readonly moverGid: number;
	readonly walkSpeed: number;
	readonly runSpeed: number;
};

/*
================
EffectRow
================
*/
export type EffectRow = {
	readonly atMs: number;
	readonly opcode: number;
	readonly gid: number;
	readonly skill: number;
	readonly token: number;
};

/*
================
SpeedJournalState

The four flat journals as GameplayState publishes them.
================
*/
export type SpeedJournalState = {
	readonly speedJournal: readonly number[];
	readonly effectJournal: readonly number[];
	readonly mountJournal: readonly number[];
	readonly buffJournal: readonly number[];
};

/*
================
appendRow

The rows with one more appended, the oldest dropped past limit rows.
A new array, so a published state never changes under its reader.
================
*/
function appendRow( rows: readonly number[], row: readonly number[], limit: number ): readonly number[] {
	const keep = Math.max( 0, rows.length - (limit - 1) * row.length );
	return [ ...rows.slice( keep ), ...row ];
}

/*
================
buffJournal

The buff board as flat rows (see the file header).
================
*/
export function buffJournal( slots: readonly BuffSlot[] ): readonly number[] {
	const rows: number[] = [];
	for ( const slot of slots ) {
		const effect = slot.effect;
		rows.push(
			effect.gid,
			effect.skill,
			effect.token,
			slot.state === "active" ? 1 : 0,
			effect.remainingMs ?? BUFF_JOURNAL_NO_TIMER,
			effect.receivedAtMs ?? BUFF_JOURNAL_NO_TIMER
		);
	}
	return rows;
}

/*
================
createSpeedJournal
================
*/
export function createSpeedJournal() {
	let speeds: readonly number[] = [], effects: readonly number[] = [], mounts: readonly number[] = [];
	let mountedOn = MOUNT_UNKNOWN;
	return {
		/*
		================
		speed
		================
		*/
		speed( row: SpeedFrameRow ) {
			speeds = appendRow(
				speeds,
				[ row.atMs, row.gid, row.appliedGid, row.moverGid, row.walkSpeed, row.runSpeed ],
				SPEED_JOURNAL_ROWS
			);
		},
		/*
		================
		effect
		================
		*/
		effect( row: EffectRow ) {
			effects = appendRow(
				effects,
				[ row.atMs, row.opcode, row.gid, row.skill, row.token ],
				EFFECT_JOURNAL_ROWS
			);
		},
		/*
		================
		mount

		Records a change of the local player's mountedOn (0 on foot) and
		says whether it was one.
		================
		*/
		mount( atMs: number, next: number ): boolean {
			if ( next === mountedOn ) return false;
			mountedOn = next;
			mounts = appendRow( mounts, [ atMs, next ], MOUNT_JOURNAL_ROWS );
			return true;
		},
		/*
		================
		state
		================
		*/
		state( slots: readonly BuffSlot[] ): SpeedJournalState {
			return {
				speedJournal: speeds,
				effectJournal: effects,
				mountJournal: mounts,
				buffJournal: buffJournal( slots )
			};
		}
	};
}

/*
================
speedFrameRoute

Where a 0x376F about gid lands, and whether it is the local player's
business: the frame's speeds are written to its gid's mount when that
gid rides one the client knows (0x85E000), else to the gid itself. The
local player's business is a frame about it, about its mount, or one
routed onto its mount.
================
*/
export function speedFrameRoute(
	gid: number,
	localGid: number,
	mountOf: ( gid: number ) => number | undefined
): { readonly appliedGid: number; readonly local: boolean; } {
	const mount = mountOf( gid ), appliedGid = mount || gid, localMount = mountOf( localGid );
	const local = !!localGid &&
		(gid === localGid || !!localMount && (gid === localMount || appliedGid === localMount));
	return { appliedGid, local };
}
