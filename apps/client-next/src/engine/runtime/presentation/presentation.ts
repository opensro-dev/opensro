/*
===========================================================================

presentation.ts - the main thread's projection of the simulation journal

Applies complete world batches from the worker (entities, gameplay
snapshots, feedback, effective HP) and owns no simulation decisions. It also
indexes the skill catalog once per catalog for per-frame UI lookups.

===========================================================================
*/
import { createEffectiveHp } from "./effective-hp";
import type { EntityState, WorldBatch, WorldEvent } from "@/engine/contracts/world";
// Main-thread projection owns no simulation decisions. Apply a complete batch
// before acknowledging it, and never use coalesced frame snapshots for lifecycle.
export function createPresentation() {
	const hp = createEffectiveHp();
	const finishingCasts = new Map<number, import("@/engine/contracts/gameplay").CastState>();
	let disposed = false, travel: import("@/engine/contracts/world").WorldTravel | null = null;
	let gameplay: import("@/engine/contracts/gameplay").GameplayState | null = null;
	// Consumers use the projection's identity as their change signal (retained HUD,
	// vitals index), so unchanged inputs must return the same object on every call.
	let projection: {
			readonly source: import("@/engine/contracts/gameplay").GameplayState;
			readonly casts: number;
			readonly hp: number;
			readonly value: import("@/engine/contracts/gameplay").GameplayState;
		} | null = null,
		castsRevision = 0;
	let sequence = 0, epoch = 0, bootstrap: unknown = null, unhandledBytes = 0;
	let orbs: import("@/engine/contracts/orb").VisualFeedback[] = [];
	let sounds: Extract<WorldEvent, { kind: "ui-sound" | "item-sound" | "buff-ended"; }>[] = [];
	let rows: readonly EntityState[] = [];
	// The skill catalog arrives once and is carried across snapshots; its id
	// index is rebuilt only when the catalog itself changes.
	let indexed: {
		readonly catalog: readonly import("@/engine/foundation/gameplay/skill-catalog").SkillMetadata[];
		readonly index: ReadonlyMap<number, import("@/engine/foundation/gameplay/skill-catalog").SkillMetadata>;
	} | null = null;
	const skillIndex = (
		catalog: readonly import("@/engine/foundation/gameplay/skill-catalog").SkillMetadata[] | undefined
	) => {
		if ( !catalog ) return undefined;
		if ( indexed?.catalog !== catalog ) {
			indexed = { catalog, index: new Map( catalog.map( row => [ row.id, row ] ) ) };
		}
		return indexed.index;
	};
	let entities = new Map<number, EntityState>(), unhandled: WorldEvent[] = [];
	return {
		apply( batch: WorldBatch ) {
			if ( disposed ) throw new Error( "Presentation disposed" );
			if ( batch.sequence !== sequence + 1 ) throw new Error( "Presentation journal gap" );
			const nextSounds = [ ...sounds ], nextOrbs = [ ...orbs ];
			let next = entities;
			const writable = () => {
				if ( next === entities ) next = new Map( entities );
				return next;
			};
			const pending: WorldEvent[] = [ ...unhandled ];
			let nextTravel = travel,
				nextEpoch = epoch,
				nextBootstrap = bootstrap,
				nextGameplay = gameplay,
				bytes = unhandledBytes;
			for ( const event of batch.events ) {
				if ( event.kind === "reset" ) {
					if ( event.epoch <= nextEpoch ) throw new Error( "Stale world reset" );
					nextSounds.length = 0;
					nextOrbs.length = 0;
					next = new Map();
					nextEpoch = event.epoch;
					nextBootstrap = null;
					nextGameplay = null;
					nextTravel = null;
					pending.length = 0;
					bytes = 0;
				} else if ( event.kind === "travel" ) nextTravel = { ...event.travel };
				else if (
					event.kind === "item-effect" || event.kind === "level-up" || event.kind === "pet-appear" ||
					event.kind === "orb-feedback" ||
					event.kind === "orb-clear" || event.kind === "orb-gauge"
				) nextOrbs.push( event );
				else if ( event.kind === "ui-sound" || event.kind === "item-sound" || event.kind === "buff-ended" ) {
					nextSounds.push( event );
				} else if ( event.kind === "gameplay" ) {
					const catalog = event.state.skillCatalog ?? nextGameplay?.skillCatalog;
					nextGameplay = {
						...event.state,
						skillCatalog: catalog,
						skillIndex: skillIndex( catalog ),
						social: event.state.social ?? nextGameplay?.social,
						shop: "shop" in event.state ? event.state.shop : nextGameplay?.shop
					};
				} else if ( event.kind === "cast-finalize" ) { /* Retained across coalesced gameplay snapshots. */ }
				else if (
					event.kind === "hp-seed" || event.kind === "hp-result" || event.kind === "hp-refresh" ||
					event.kind === "hp-revive"
				) { /* Applied after complete batch validation below. */ } else if ( event.kind === "bootstrap" ) {
					nextBootstrap = event.value;
				} else if ( event.kind === "spawn" || event.kind === "state" ) {
					if (
						event.kind === "spawn" && next.has( event.entity.gid ) ||
						event.kind === "state" && !next.has( event.entity.gid )
					) throw new Error( "Invalid entity lifecycle transition" );
					writable().set( event.entity.gid, Object.freeze( { ...event.entity } ) );
				} else if ( event.kind === "despawn" ) { if ( next.has( event.gid ) ) writable().delete( event.gid ); }
				else if ( event.kind === "native" ) {
					bytes += event.payload.byteLength;
					pending.push( event );
				} else if ( event.epoch !== nextEpoch ) throw new Error( "World synchronization epoch mismatch" );
			}
			if ( bytes > (8 << 20) || pending.length > 8192 ) {
				throw new Error( "Unimplemented native packet consumer backlog" );
			}
			if ( nextOrbs.length > 8192 ) throw new Error( "Orb presentation backlog" );
			if ( nextSounds.length > 8192 ) throw new Error( "Sound presentation backlog" );
			const reset = epoch !== nextEpoch;
			for ( const event of batch.events ) {
				if ( event.kind === "reset" ) {
					hp.clear();
					finishingCasts.clear();
					castsRevision++;
				} else if ( event.kind === "cast-finalize" ) {
					finishingCasts.set( event.cast.token, event.cast );
					castsRevision++;
				} else if ( event.kind === "despawn" ) hp.remove( event.gid );
				else if (
					event.kind === "hp-seed" || event.kind === "hp-result" || event.kind === "hp-refresh" ||
					event.kind === "hp-revive"
				) hp.receive( event );
			}
			if ( next !== entities ) rows = [ ...next.values() ];
			travel = nextTravel;
			orbs = nextOrbs;
			sounds = nextSounds;
			gameplay = nextGameplay;
			entities = next;
			epoch = nextEpoch;
			bootstrap = nextBootstrap;
			sequence = batch.sequence;
			unhandled = pending;
			unhandledBytes = bytes;
			return reset;
		},
		travel: () => travel,
		takeFeedback() {
			const result = orbs;
			orbs = [];
			return result;
		},
		takeSounds() {
			const result = sounds;
			sounds = [];
			return result;
		},
		impact: hp.impact,
		currentResult: hp.currentResult,
		finishedCasts() {
			if ( finishingCasts.size ) {
				finishingCasts.clear();
				castsRevision++;
			}
		},
		release: hp.release,
		dead: hp.dead,
		step: hp.step,
		dispose() {
			hp.clear();
			finishingCasts.clear();
			projection = null;
			travel = null;
			orbs = [];
			sounds = [];
			disposed = true;
			entities.clear();
			rows = [];
			unhandled = [];
			unhandledBytes = 0;
			bootstrap = null;
			gameplay = null;
		},
		entities: () => rows,
		read: ( gid: number ) => entities.get( gid ),
		count: () => entities.size,
		bootstrap: () => bootstrap,
		gameplay() {
			const source = gameplay, hpRevision = hp.revision();
			if ( !source ) return null;
			if ( projection?.source !== source || projection.casts !== castsRevision || projection.hp !== hpRevision ) {
				const casts = finishingCasts.size ?
					[
						...(source.casts ?? []).filter( c => !finishingCasts.has( c.token ) ),
						...finishingCasts.values()
					] :
					source.casts;
				const vitals = source.vitals?.map( row => {
					const value = hp.hp( row.gid ), deathState = hp.dead( row.gid );
					return (value === undefined || value === row.hp) && !deathState ?
						row :
						{
							...row,
							...(value === undefined ? {} : { hp: value }),
							...(deathState ? { deathState } : {})
						};
				} );
				projection = { source, casts: castsRevision, hp: hpRevision, value: { ...source, casts, vitals } };
			}
			return projection.value;
		},
		takeNative() {
			const result = unhandled;
			unhandled = [];
			unhandledBytes = 0;
			return result;
		}
	};
}
