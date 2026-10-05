/*
===========================================================================

effects.ts - the asset worker's decoder for published skill effect records

Validates the published native skilleffectset projection (effectRecords.json)
and projects each record into the EffectCatalog the presentation owner reads:
stage controls, decoded attachment bindings, scripts and resources. Unknown
or malformed fields throw instead of degrading; unsupported operations are
kept for explicit presentation diagnostics.

===========================================================================
*/
import { createEffectPrograms } from "./program/program";
import { skillMotionRole } from "@/engine/foundation/animation/skill-motion";
import { effectScript } from "@/engine/foundation/animation/effect-script";
import type { EffectCatalog, EffectRecord, EffectStage } from "@/engine/contracts/effects";
/*
================
createEffectDecoder

Decode the published native skilleffectset projection in the asset worker.
Preserve unsupported operations for explicit presentation diagnostics.
================
*/
export function createEffectDecoder() {
	const programs = createEffectPrograms();
	return {
		model: programs.decode,
		dispose: programs.clear,
		/*
		================
		decode

		One published catalog: every record validated before any is returned.
		================
		*/
		decode( bytes: Uint8Array ): EffectCatalog {
			const value = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) );
			if (
				!value || typeof value !== "object" || Array.isArray( value ) || Object.keys( value ).length > 10000
			) {
				throw new Error( "Invalid effect catalog" );
			}
			const result: Record<string, EffectRecord> = {};
			for ( const [id, record] of Object.entries( value ) ) {
				const row = record as {
					animSlotKey?: number;
					hitLight?: EffectRecord["hitLight"];
					attachedMotion?: { set: string; id: number; } | null;
					priority00?: number;
					defenseEffectPath?: string | null;
					byteBe?: number;
					overlap01?: boolean;
					authoredActionNames?: { ready?: string | null; wait?: string | null; shot?: string | null; };
					damageEffectPath?: string;
					arrowTrailEffectPath?: string | null;
					arrowForceEffectPath?: string | null;
					hideWeapon?: number;
					authoredShotAnimationNames?: string[];
					animTable0?: string[];
					animTable1?: string[];
					animTable2?: string[];
					animTable3?: string[];
					animTable4?: string[];
					animTable5?: string[];
					authoredStages?: Record<string, unknown>[];
				};
				if ( !Array.isArray( row.authoredStages ) || row.authoredStages.length > 512 ) {
					throw new Error( "Invalid effect stage list" );
				}
				const phaseClips = [
					row.animTable0 ?? [],
					row.animTable1 ?? [],
					row.animTable2 ?? row.authoredShotAnimationNames ?? [],
					row.animTable3 ?? [],
					row.animTable4 ?? [],
					row.animTable5 ?? []
				].map( table => {
					if (
						!Array.isArray( table ) || table.length > 255 ||
						table.some( name => typeof name !== "string" || !name.startsWith( "ANI_" ) )
					) throw new Error( "Invalid effect phase animation table" );
					return table.map( name =>
						row.animSlotKey === undefined ?
							name.replace( /^ANI_/, "" ).toLowerCase() :
							skillMotionRole( row.animSlotKey, name )
					);
				} );
				if ( row.overlap01 !== undefined && typeof row.overlap01 !== "boolean" ) {
					throw Error(
						"Invalid attached effect overlap flag"
					);
				}
				const priority = row.priority00 ?? 0, actions = row.authoredActionNames;
				if (
					!Number.isInteger( priority ) || priority < 0 || priority > 255 ||
					[ actions?.ready, actions?.wait ].some( value =>
						value !== undefined && value !== null && (typeof value !== "string" || value.length > 256)
					)
				) throw Error( "Invalid attached action metadata" );
				const arrowEffects = [ row.arrowTrailEffectPath ?? null, row.arrowForceEffectPath ?? null ] as const;
				if (
					arrowEffects.some( path =>
						path !== null && (typeof path !== "string" || !path.endsWith( ".efp" ) || path.includes( ".." ))
					)
				) throw Error( "Invalid arrow effect resource" );
				const hideWeapon = row.hideWeapon ?? 0;
				if ( !Number.isInteger( hideWeapon ) || hideWeapon < 0 || hideWeapon > 255 ) {
					throw Error(
						"Invalid weapon visibility flag"
					);
				}
				const damageEffect = row.damageEffectPath ?? null;
				if (
					damageEffect !== null &&
					(typeof damageEffect !== "string" || !damageEffect.endsWith( ".efp" ) ||
						damageEffect.includes( ".." ))
				) throw Error( "Invalid damage effect resource" );
				const secondary = row.byteBe ?? 0;
				if ( !Number.isInteger( secondary ) || secondary < 0 || secondary > 255 ) {
					throw Error(
						"Invalid secondary effect flag"
					);
				}
				const defense = row.defenseEffectPath ?? null;
				if ( defense !== null && (typeof defense !== "string" || defense.includes( ".." )) ) {
					throw Error(
						"Invalid defensive effect resource"
					);
				}
				const attachedMotion = row.attachedMotion ?? undefined;
				if (
					attachedMotion &&
					(!/^[a-z_]+$/.test( attachedMotion.set ) || !Number.isInteger( attachedMotion.id ) ||
						attachedMotion.id < 0 || attachedMotion.id > 32767)
				) throw Error( "Invalid attached motion" );
				const hitLight = row.hitLight ?? undefined;
				if (
					hitLight &&
					(!Array.isArray( hitLight.color ) || hitLight.color.length !== 3 || hitLight.color.some( v =>
						!Number.isFinite( v ) || v < 0 || v > 1
					) || ![ hitLight.duration, hitLight.range, hitLight.attenuation ].every( Number.isFinite ))
				) throw Error( "Invalid hit light" );
				result[id] = {
					hitLight,
					attachedMotion,
					damageEffect,
					secondaryEffect: secondary !== 0,
					arrowEffects,
					hideWeapon,
					overlap: row.overlap01 ?? false,
					attachedAction: { priority, defense, attack: damageEffect },
					phaseClips,
					clips: phaseClips[2]!,
					stages: row.authoredStages.map( stage => {
						const offset = stage.startOffset,
							move = stage.move as {
								kind?: string;
								delay?: number;
								startSpeed?: number;
								endSpeed?: number;
							} | undefined,
							options = stage.actionOptions as {
								lifeMs?: number;
							} | undefined;
						if (
							!Array.isArray( offset ) || offset.length !== 3 || offset.some( n =>
								typeof n !== "number" || !Number.isFinite( n )
							) || typeof stage.startEvent !== "number" || typeof stage.actionType !== "string"
						) {
							throw new Error( "Invalid native effect stage" );
						}
						const targetOffset = stage.targetOffset ?? [ 0, 0, 0 ];
						if (
							!Array.isArray( targetOffset ) || targetOffset.length !== 3 ||
							targetOffset.some( n => typeof n !== "number" || !Number.isFinite( n ) )
						) throw new Error( "Invalid effect target offset" );
						const movement = {
							delayMs: move?.delay ?? 0,
							startSpeed: move?.startSpeed ?? 0,
							endSpeed: move?.endSpeed ?? 0
						};
						if (
							Object.values( movement ).some( n => !Number.isInteger( n ) || n < 0 || n > 65535 )
						) throw new Error( "Invalid effect movement" );
						const parameters = stage.param ?? [ 0, 0, 0 ];
						const uint = ( key: string, min = 0 ) => {
							const n = stage[key] ?? 0;
							if (
								typeof n !== "number" || !Number.isInteger( n ) || n < min || n > 0xffffffff
							) throw Error( "Invalid native effect " + key );
							return n;
						};
						const scale = stage.scale ?? null;
						if ( scale !== null && scale !== "MOB_BASE" && scale !== "CHAR_BASE" ) {
							throw Error( "Invalid native effect scale" );
						}
						const rotation = stage.rotate ?? 0;
						if ( typeof rotation !== "number" || !Number.isFinite( rotation ) ) {
							throw Error( "Invalid native effect rotation" );
						}
						const damageTypes = stage.damageTypes ?? [];
						if (
							!Array.isArray( damageTypes ) ||
							damageTypes.some( t => ![ "NOR", "CRI", "HWAN" ].includes( String( t ) ) )
						) throw Error( "Invalid native damage filter" );
						const ao = (stage.actionOptions ?? {}) as Record<string, unknown>;
						const option = ( key: string ) => {
							const n = ao[key] ?? 0;
							if ( typeof n !== "number" || !Number.isFinite( n ) ) {
								throw Error( "Invalid effect option " + key );
							}
							return n;
						};
						for (
							const key of [ "enabled", "simultaneousRelease" ]
						) {
							if ( ao[key] !== undefined && typeof ao[key] !== "boolean" ) {
								throw Error( "Invalid effect option " + key );
							}
						}
						for (
							const key of [
								"startKeepRotation",
								"startAddHeight",
								"targetKeepRotation",
								"targetAddHeight"
							]
						) if ( typeof stage[key] !== "boolean" ) throw Error( "Invalid effect binding " + key );
						const native: NonNullable<EffectStage["native"]> = {
							slot: uint( "id" ),
							attach: uint( "attach" ),
							trade: uint( "trade" ),
							kill: uint( "kill" ),
							scale,
							rotation,
							fadeInMs: uint( "fadeInMs" ),
							fadeOutMs: uint( "fadeOutMs", -0x80000000 ),
							damageTypes: damageTypes.map( String ),
							actionOptions: {
								enabled: ao.enabled === true,
								direction: option( "direction" ),
								distance: option( "distance" ),
								residualDistance: option( "residualDistance" ),
								simultaneousRelease: ao.simultaneousRelease === true
							}
						};
						if (
							!Array.isArray( parameters ) || parameters.length !== 3 ||
							parameters.some( n => !Number.isInteger( n ) || n < -0x80000000 || n > 0x7fffffff )
						) throw new Error( "Invalid effect parameters" );
						return {
							native,
							script: effectScript( Array.isArray( stage.scripts ) ? stage.scripts.map( String ) : [] ),
							parameters: parameters as [number, number, number],
							movement,
							targetOffset: targetOffset as [number, number, number],
							targetBone: typeof stage.targetBone === "string" ? stage.targetBone : null,
							targetKeepRotation: stage.targetKeepRotation as boolean,
							targetAddHeight: stage.targetAddHeight as boolean,
							arrivalResource: typeof stage.secondaryObjectPath === "string" ?
								stage.secondaryObjectPath :
								null,
							soundEnd: typeof stage.soundEndPublicPath === "string" ? stage.soundEndPublicPath : null,
							phase: typeof stage.animationPhase === "string" ? stage.animationPhase : "",
							resource: typeof stage.objectResourcePath === "string" ? stage.objectResourcePath : null,
							damageEvent: stage.damageEvent === true,
							startEvent: stage.startEvent,
							action: stage.actionType,
							move: move?.kind ?? "",
							bone: typeof stage.startBone === "string" ? stage.startBone : null,
							keepRotation: stage.startKeepRotation as boolean,
							addHeight: stage.startAddHeight as boolean,
							offset: offset as [
								number,
								number,
								number
							],
							life: (options?.lifeMs ?? 0) / 1000,
							sound: typeof stage.soundBeginPublicPath === "string" ? stage.soundBeginPublicPath : null,
							count: typeof stage.createCount === "number" ? stage.createCount : 1,
							scripts: Array.isArray( stage.scripts ) ? stage.scripts.map( String ) : []
						};
					} )
				};
			}
			return result;
		}
	};
}
