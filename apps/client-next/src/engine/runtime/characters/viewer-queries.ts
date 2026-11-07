/*
===========================================================================

viewer-queries.ts - the 3D viewer's read-only window on the presentation

Port-only, not native (docs/guides/VIEWER.md). The viewer drives the character
presentation like the world does, but must also tell a model the catalogue
lacks from one still loading, type a look's worn items, and wait until a
dressed actor wears its parts. These answers read characters.ts's state;
the game never calls them.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { CharacterActor } from "@/engine/contracts/character";
import type { ItemPresentation, Resource } from "./internal/presentation-contract";

// An effect actor's model names its program in the effect bundle.
const EFFECT_PROGRAMS = "/effects/programs.json#";

/*
================
ViewerQuerySources

What the queries read, all owned by characters.ts.
================
*/
export interface ViewerQuerySources {
	readonly published: {
		readonly manifest: number;
		readonly manifests: readonly string[];
		readonly items: Readonly<Record<string, ItemPresentation>>;
		readonly ridesByRider: ReadonlyMap<string, unknown>;
	};
	readonly appearances: ReadonlyMap<
		number,
		{ readonly parts: readonly unknown[]; readonly model: string; readonly particles: readonly unknown[]; }
	>;
	// The frame's published results: read live, since a frame replaces its map.
	readonly output: { readonly displayed: ReadonlyMap<number, CharacterActor>; };
}

/*
================
createViewerQueries

resourceFor is characters.ts's appearance lookup (appearance-lookup.ts).
================
*/
export function createViewerQueries(
	sources: ViewerQuerySources,
	resourceFor: ( entity: EntityState ) => Resource | undefined
) {
	const { published, appearances, output } = sources;
	return {
		/*
		================
		knows

		Whether the catalogue has the entity's model; undefined until every
		manifest is admitted.
		================
		*/
		knows( entity: EntityState ): boolean | undefined {
			return published.manifest < published.manifests.length ?
				undefined :
				resourceFor( entity ) !== undefined;
		},
		/*
		================
		itemType

		An item's packed type word, once the catalogue has it.
		================
		*/
		itemType( refObjId: number ): number | undefined {
			return published.items[String( refObjId )]?.typeFlags;
		},
		/*
		================
		dressed

		Every worn part is resident and the actor wears them: until then the
		actor shows its fallback model (actor-appearance.ts).
		================
		*/
		dressed( gid: number ): boolean {
			const appearance = appearances.get( gid );
			return !appearance?.parts.length || output.displayed.get( gid )?.model === appearance.model;
		},
		/*
		================
		glows

		Whether the dressed actor carries effect particles: a rare (SoX) weapon
		or shield's glow (itemrare.txt, native 8EA960) and the model's own
		ambient set. The viewer keeps animating such a doll.
		================
		*/
		glows( gid: number ): boolean {
			return (appearances.get( gid )?.particles.length ?? 0) > 0;
		},
		/*
		================
		glowing

		Every one of the actor's particles is drawn: as many effect actors
		attached to it as its appearance carries (model-emission.ts emits one
		for each once its effect program is resident).
		================
		*/
		glowing( gid: number ): boolean {
			const wanted = appearances.get( gid )?.particles.length ?? 0;
			if ( !wanted ) return true;
			let drawn = 0;
			for ( const actor of output.displayed.values() ) {
				if ( actor.attachment?.gid === gid && actor.model.includes( EFFECT_PROGRAMS ) ) drawn++;
			}
			return drawn >= wanted;
		},
		/*
		================
		linkedRide

		The drawn actor of the entity's linked ride (the four riding uniques,
		presentation-auxiliary.ts): undefined when it has none, null while its
		ride is not drawn yet. The ride is the actor that picks as its rider.
		================
		*/
		linkedRide( entity: EntityState ): number | null | undefined {
			const resource = resourceFor( entity );
			if ( !resource || !published.ridesByRider.has( resource.codename ) ) return undefined;
			for ( const actor of output.displayed.values() ) {
				if ( actor.pickOwner === entity.gid && actor.gid !== entity.gid ) return actor.gid;
			}
			return null;
		}
	};
}
