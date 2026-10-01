/*
===========================================================================

animation-metadata.ts - published BAN metadata and the weapon animation set

Validates the per-clip metadata the asset build publishes (duration, track
events, time warp, sound cues) and names the animation set a character plays
for its equipped weapon. The native client plays every motion through that
set and falls back to "default" (CCObjCharacter_PlayAnimationWithFallback
8E7150), so the name decides stand, walk, run and attack clips alike.

===========================================================================
*/
export interface AnimationTrackEvent {
	readonly cursorMs: number;
	readonly eventCode: number;
	readonly param0: number;
	readonly param1: number;
}
export interface AnimationWarpCurve {
	readonly scale: number;
	readonly records: readonly { readonly phase: number; readonly value: number; }[];
}
export interface AnimationMetadata {
	readonly loop?: boolean;
	readonly stateId?: number;
	readonly trackEvents: readonly AnimationTrackEvent[];
	readonly timeWarpCurve: AnimationWarpCurve;
	readonly durationMs: number;
	readonly soundEvents: readonly {
		readonly cursorMs: number;
		readonly cue: string;
	}[];
}
/*
================
animationMetadata

Checks one manifest metadata table and returns it typed. Throws on any
malformed row: a bad row is an asset build defect, not runtime data.
================
*/
export function animationMetadata( value: unknown ): Record<string, AnimationMetadata> {
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) {
		throw new Error( "Invalid animation metadata" );
	}
	const result: Record<string, AnimationMetadata> = {};
	for ( const [name, entry] of Object.entries( value ) ) {
		if ( !entry || typeof entry !== "object" ) {
			throw new Error( "Invalid animation metadata entry" );
		}
		const row = entry as {
			loop?: boolean;
			stateId?: unknown;
			durationMs?: unknown;
			soundEvents?: unknown;
			trackEvents?: unknown;
			timeWarpCurve?: unknown;
		};
		const stateId = typeof row.stateId === "number" ? row.stateId : undefined;
		if ( stateId !== undefined && (!Number.isInteger( stateId ) || stateId < 0 || stateId > 65535) ) {
			throw new Error( "Invalid animation state id" );
		}
		if ( typeof row.durationMs !== "number" || !Number.isFinite( row.durationMs ) || row.durationMs <= 0 ) {
			throw new Error( "Invalid animation duration" );
		}
		// The published BAN manifest omits soundEvents when the native cue list is empty.
		const events = row.soundEvents === undefined ? [] : row.soundEvents;
		if (
			!Array.isArray( events ) ||
			events.some( event =>
				!event || typeof event.cursorMs !== "number" || !Number.isFinite( event.cursorMs ) ||
				event.cursorMs < 0 || typeof event.cue !== "string"
			)
		) {
			throw new Error( "Invalid animation sound cue" );
		}
		const trackEvents = row.trackEvents ?? [];
		if (
			!Array.isArray( trackEvents ) ||
			trackEvents.some( event =>
				!event || !Number.isInteger( event.cursorMs ) || event.cursorMs < 0 ||
				!Number.isInteger( event.eventCode ) || !Number.isInteger( event.param0 ) ||
				!Number.isInteger( event.param1 )
			)
		) {
			throw new Error( "Invalid animation event map" );
		}
		const curve = (row.timeWarpCurve ?? { scale: 0, records: [] }) as AnimationWarpCurve;
		if (
			!curve || !Number.isFinite( curve.scale ) || !Array.isArray( curve.records ) ||
			curve.records.some( ( point, index ) =>
				!point || !Number.isFinite( point.phase ) || !Number.isFinite( point.value ) || point.phase < 0 ||
				point.phase > 1 || (index > 0 && point.phase <= curve.records[index - 1]!.phase)
			)
		) {
			throw new Error( "Invalid animation time-warp curve" );
		}
		if ( row.loop !== undefined && typeof row.loop !== "boolean" ) throw Error( "Invalid animation loop flag" );
		result[name] = {
			...(row.loop === undefined ? {} : { loop: row.loop }),
			...(stateId === undefined ? {} : { stateId }),
			trackEvents: trackEvents.filter( event => event.eventCode !== 0 ).map( event => ({ ...event }) ).sort( (
				a,
				b
			) => a.cursorMs - b.cursorMs ),
			timeWarpCurve: { scale: curve.scale, records: curve.records.map( point => ({ ...point }) ) },
			durationMs: row.durationMs,
			soundEvents: events.map( event => ({ cursorMs: event.cursorMs, cue: event.cue }) )
		};
	}
	return result;
}

/*
================
weaponAnimationSet

CCObjCharacter_GetWeaponAnimationPrefixString 8E6FF0: the item type word
(bits 11..15) to the set name, spelled with the published clip suffix's
hyphens. Type 10 is "onehand_staff": its global is labelled DarkStaff, but
CRT_InitWeaponAnimationPrefixStrings BBDB70 assigns "onehand_staff". Native
also maps type 0x10 to sword or onehand_sword by the owner's race byte; no
v1.150 item carries that type, so it is left out.
================
*/
export function weaponAnimationSet( typeFlags: number ): string | undefined {
	switch ( (typeFlags >>> 11) & 31 ) {
		case 2:
		case 3:
			return "sword";
		case 4:
		case 5:
			return "spear";
		case 6:
		case 12:
			return "bow";
		case 7:
			return "onehand-sword";
		case 8:
			return "twohand-sword";
		case 9:
			return "dual-axe";
		case 11:
			return "twohand-staff";
		case 13:
			return "dagger";
		case 14:
			return "harf";
		case 10:
		case 15:
			return "onehand-staff";
	}
}
