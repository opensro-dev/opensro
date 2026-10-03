/*
===========================================================================

preparation.ts - sound dependencies for the admitted world

Owns catalogue loading and selects sound bytes from the same authored tables
as character presentation. Audio owns decoding and residency. Readiness covers
the current character, known skills, and resident character sound profiles;
unrelated regions and unlearned skills do not delay world entry.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";
import { skillSoundRoots } from "@/engine/foundation/animation/sound-selectors";

/*
================
SoundScene
Only sound dependency identities cross into this owner.
================
*/
export type SoundScene = Pick<GameplayState, "localGid" | "skills" | "casts" | "attachedEffects">;
/*
================
SoundEntity
================
*/
export type SoundEntity = Pick<EntityState, "refObjId" | "groundItem" | "transformSkin">;

const CATALOG_LIMIT = 16 << 20;
// Effect records share the 32 MiB admission contract of character effects.
const EFFECT_CATALOG_LIMIT = 32 << 20;
const CATALOG_PATHS = [
	"/assets/audio/effectsound.json",
	"/assets/char/roster.json",
	"/assets/npc/manifest.json",
	"/assets/anim/manifest.json",
	"/assets/data/skillAudioData.json",
	"/assets/skill/effectRecords.json"
] as const;

/*
================
SoundRow
Only the selector and path are retained; presentation owns cue timing.
================
*/
interface SoundRow {
	object: string;
	skillId?: string;
	publicPath?: string;
}
/*
================
CharacterRow
================
*/
interface CharacterRow {
	refObjId: number;
	codename: string;
	soundProfileName?: string;
}
/*
================
EffectRow
================
*/
interface EffectRow {
	stages?: readonly { sound?: string | null; soundEnd?: string | null; }[];
}

/*
================
sameMembers
================
*/
function sameMembers<T>( a: ReadonlySet<T>, b: ReadonlySet<T> ): boolean {
	if ( a.size !== b.size ) return false;
	for ( const value of a ) if ( !b.has( value ) ) return false;
	return true;
}

/*
================
createSoundPreparation
================
*/
export function createSoundPreparation( assets: AssetOwner, origin: string ) {
	let job: number | null = null;
	let cursor = 0;
	let failure: string | null = null;
	let disposed = false;
	let rules: readonly SoundRow[] = [];
	const characters = new Map<number, CharacterRow>();
	let profiles: Record<string, { soundProfileName?: string; }> = {};
	let skills: ReturnType<typeof skillSoundRoots> | null = null;
	let effects: Record<string, EffectRow> = {};
	// The last selection's inputs. Runs every frame: equal inputs return
	// before any set is built, equal members before any path is chosen.
	const selection: {
		entities: readonly SoundEntity[] | null;
		skills: SoundScene["skills"] | undefined;
		casts: SoundScene["casts"] | undefined;
		attachedEffects: SoundScene["attachedEffects"] | undefined;
		ids: Set<number> | null;
		refs: Set<number> | null;
	} = { entities: null, skills: undefined, casts: undefined, attachedEffects: undefined, ids: null, refs: null };
	let paths: readonly string[] = [];
	let active = false;

	/*
	================
	admit
	Discard bulk geometry and animation metadata after retaining sound identity.
	================
	*/
	function admit( value: unknown ) {
		if ( !value || typeof value !== "object" ) throw Error( "Invalid sound preparation catalogue" );
		if ( cursor === 0 ) {
			const source = value as { rules: SoundRow[]; };
			if ( !Array.isArray( source.rules ) ) throw Error( "Missing effect sound rules" );
			rules = source.rules.map( row => {
				if (
					typeof row.object !== "string" || (row.publicPath !== undefined &&
						(typeof row.publicPath !== "string" || !row.publicPath.startsWith( "/assets/audio/" ) ||
							row.publicPath.includes( ".." )))
				) {
					throw Error( "Invalid effect sound path" );
				}
				return { object: row.object, skillId: row.skillId, publicPath: row.publicPath };
			} );
		} else if ( cursor === 1 || cursor === 2 ) {
			const source = value as { models: CharacterRow[] | Record<string, CharacterRow>; };
			if ( !source.models || typeof source.models !== "object" ) {
				throw Error( "Missing character sound profiles" );
			}
			for ( const row of Object.values( source.models ) ) {
				// Auxiliary BSR entries have no entity identity, as in character admission.
				if ( row.refObjId === undefined ) continue;
				if ( !Number.isSafeInteger( row.refObjId ) || typeof row.codename !== "string" ) {
					throw Error( "Invalid character sound identity" );
				}
				characters.set( row.refObjId, {
					refObjId: row.refObjId,
					codename: row.codename,
					soundProfileName: row.soundProfileName
				} );
			}
		} else if ( cursor === 3 ) {
			const source = value as { models: Record<string, { soundProfileName?: string; }>; };
			if ( !source.models ) throw Error( "Missing animation sound profiles" );
			profiles = Object.fromEntries(
				Object.entries( source.models ).map( (
					[name, row]
				) => [ name, { soundProfileName: row.soundProfileName } ] )
			);
		} else if ( cursor === 4 ) {
			// skillAudioData.json: the skill sound identity plane alone (buildSkillDataAsset.mjs).
			const source = value as { format?: unknown; skillAudioRows: string[]; };
			if ( source.format !== "sro-skill-audio" || !Array.isArray( source.skillAudioRows ) ) {
				throw Error( "Missing skill sound roots" );
			}
			skills = skillSoundRoots( source.skillAudioRows );
		} else {
			effects = Object.fromEntries(
				Object.entries( value ).map( ( [id, row] ) => {
					const source = row as EffectRow;
					return [ id, {
						stages: (source.stages ?? []).map( stage => ({
							sound: stage.sound,
							soundEnd: stage.soundEnd
						}) )
					} ];
				} )
			);
		}
	}

	/*
	================
	select
	Known skills include the server-provisioned racial basic attacks. Preserve
	both root spellings because player and non-player dispatch use different ones.
	================
	*/
	function select( gameplay: SoundScene, entities: readonly SoundEntity[] ) {
		if (
			selection.ids && selection.entities === entities && selection.skills === gameplay.skills &&
			selection.casts === gameplay.casts && selection.attachedEffects === gameplay.attachedEffects
		) return;
		selection.entities = entities;
		selection.skills = gameplay.skills;
		selection.casts = gameplay.casts;
		selection.attachedEffects = gameplay.attachedEffects;
		const ids = new Set( gameplay.skills ?? [] );
		for ( const cast of gameplay.casts ?? [] ) ids.add( cast.skill );
		for ( const effect of gameplay.attachedEffects ?? [] ) ids.add( effect.skill );
		const refs = new Set<number>();
		for ( const entity of entities ) {
			if ( !entity.groundItem ) refs.add( entity.transformSkin?.refObjId ?? entity.refObjId );
		}
		if ( selection.ids && sameMembers( selection.ids, ids ) && sameMembers( selection.refs!, refs ) ) return;
		selection.ids = ids;
		selection.refs = refs;
		const wantedProfiles = new Set<string>();
		for ( const id of refs ) {
			const row = characters.get( id );
			if ( row ) {
				wantedProfiles.add( row.soundProfileName ?? profiles[row.codename]?.soundProfileName ?? row.codename );
			}
		}
		const groups = new Set<string>( [ "-" ] );
		for ( const id of ids ) for ( const name of skills?.get( id ) ?? [] ) groups.add( name );
		const wanted = new Set<string>();
		for ( const row of rules ) {
			if ( !row.publicPath ) continue;
			if ( row.object === "PLAYER" ? groups.has( row.skillId ?? "-" ) : wantedProfiles.has( row.object ) ) {
				wanted.add( row.publicPath );
			}
		}
		// The player's level-up system skill uses the same authored stage table.
		for ( const id of [ ...ids, -2147483642 ] ) {
			for ( const stage of effects[String( id )]?.stages ?? [] ) {
				for ( const path of [ stage.sound, stage.soundEnd ] ) {
					if ( !path ) continue;
					if ( !path.startsWith( "/assets/audio/" ) || path.includes( ".." ) ) {
						throw Error( "Invalid skill sound path" );
					}
					wanted.add( path );
				}
			}
		}
		paths = [ ...wanted ].sort();
	}

	return {
		/*
		================
		step
		Always collect owned work, including when the world is left mid-load.
		================
		*/
		step( gameplay: SoundScene | null, entities: readonly SoundEntity[] ) {
			if ( disposed ) return;
			active = !!gameplay?.localGid;
			try {
				if ( job !== null ) {
					const result = assets.take( job );
					if ( result ) {
						job = null;
						if ( cursor === CATALOG_PATHS.length - 1 ) {
							if ( result.kind !== "effects" ) {
								throw Error(
									result.kind === "error" ? result.error : "Expected decoded effect catalogue"
								);
							}
							admit( result.catalog );
						} else {
							if ( result.kind !== "bytes" ) {
								throw Error(
									result.kind === "error" ? result.error : "Invalid sound catalogue response"
								);
							}
							admit( JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) ) );
						}
						cursor++;
					}
				}
				if ( active && !failure && cursor < CATALOG_PATHS.length && job === null && assets.available() > 0 ) {
					const path = CATALOG_PATHS[cursor]!;
					const limit = cursor === CATALOG_PATHS.length - 1 ? EFFECT_CATALOG_LIMIT : CATALOG_LIMIT;
					job = assets.request(
						new URL( path, origin ).href,
						limit,
						cursor === CATALOG_PATHS.length - 1 ? "effects" : undefined
					);
				}
				if ( active && cursor === CATALOG_PATHS.length ) select( gameplay!, entities );
			} catch ( error ) {
				failure = `Sound preparation ${CATALOG_PATHS[cursor] ?? "selection"}: ${String( error )}`;
			}
		},
		paths: () => active ? paths : [],
		ready: () => !active || (!failure && cursor === CATALOG_PATHS.length),
		error: () => failure,
		/*
		================
		dispose
		================
		*/
		dispose() {
			disposed = true;
			if ( job !== null ) assets.cancel( job );
			job = null;
			paths = [];
			characters.clear();
		}
	};
}
